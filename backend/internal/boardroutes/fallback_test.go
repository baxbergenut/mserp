package boardroutes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTomTomNoRouteFallback(t *testing.T) {
	for _, tc := range []struct {
		name, paid string
		googleKey  bool
		used       int64
		success    bool
		wantCalls  int
	}{
		{"free Google", "tomtom", true, 0, true, 2},
		{"Google exhausted paid TomTom", "tomtom", true, 1, false, 1},
		{"Google exhausted paid off", "off", true, 1, false, 1},
		{"explicit paid Google", "google", true, 1, true, 2},
		{"Google absent", "tomtom", false, 0, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := map[string]int64{"google": tc.used}
			paidGoogle, calls, reservations := 0, 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method == http.MethodGet {
					if reservations != 1 || usage["tomtom"] != 1 {
						t.Error("TomTom sent without reservation")
					}
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"detailedError":{"code":"NO_ROUTE_FOUND","message":"secret-token never expose"}}`))
					return
				}
				if reservations != 2 || usage["google"] != tc.used+1 {
					t.Error("Google sent without separate reservation")
				}
				if r.Header.Get("X-Goog-FieldMask") != "routes.duration,routes.distanceMeters" {
					t.Error("unexpected paid features")
				}
				var body struct {
					Origin struct {
						Location struct {
							LatLng struct{ Latitude, Longitude float64 } `json:"latLng"`
						}
					}
					Destination struct {
						Location struct {
							LatLng struct{ Latitude, Longitude float64 } `json:"latLng"`
						}
						Address string
					}
					TravelMode, RoutingPreference string
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Origin.Location.LatLng.Latitude != 40 || body.Origin.Location.LatLng.Longitude != -74 || body.Destination.Location.LatLng.Latitude != 41 || body.Destination.Location.LatLng.Longitude != -73 || body.Destination.Address != "" || body.TravelMode != "DRIVE" || body.RoutingPreference != "TRAFFIC_UNAWARE" {
					t.Error("fallback changed coordinate payload", body)
				}
				_, _ = w.Write([]byte(`{"routes":[{"duration":"60s","distanceMeters":1000}]}`))
			}))
			defer server.Close()
			o := Options{TomTomAPIKey: "secret-token", TomTomMonthlyFree: 10, GoogleMonthlyFree: 1, PaidProvider: tc.paid, RequestsPerMinute: 120}
			if tc.googleKey {
				o.APIKey = "secret-google"
			}
			c := NewClient(o)
			c.URL, c.TomTomURL = server.URL, server.URL
			c.Budget = budgetFunc(func(_ context.Context, o Options) (string, error) {
				reservations++
				if reservations == 2 && (o.TomTomAPIKey != "" || o.PaidProvider != tc.paid || o.GoogleMonthlyFree != 1) {
					t.Error("fallback expanded provider/billing permission")
				}
				p, paid := chooseProvider(o, usage)
				if p == "" {
					return "", ErrBudgetExhausted
				}
				usage[p]++
				if p == "google" && paid {
					paidGoogle++
				}
				return p, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			started := time.Now()
			estimate, err := c.Route(ctx, 40, -74, 41, -73)
			if (err == nil) != tc.success || calls != tc.wantCalls {
				t.Fatal(estimate, err, calls)
			}
			if tc.success && (!strings.HasPrefix(estimate.Assumptions, "Google") || time.Since(started) < 400*time.Millisecond) {
				t.Fatal("fallback skipped normal pacing/provider assumptions")
			}
			if !c.blockedUntil.IsZero() {
				t.Fatal("expected no-route blocked fallback with generic cooldown")
			}
			if err != nil && (!strings.Contains(err.Error(), "NO_ROUTE_FOUND") || strings.Contains(err.Error(), "secret")) {
				t.Fatal("missing useful diagnostic or provider data leaked", err)
			}
			if paidGoogle > 0 && tc.paid != "google" {
				t.Fatal("unauthorized Google overage")
			}
		})
	}
}

func TestTomTomFallbackIsNarrowAndNonRecursive(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		fallback bool
	}{
		{"arbitrary400", 400, `{"detailedError":{"code":"SECRET","message":"secret-token"}}`, false},
		{"invalidJSON", 400, `secret-token`, false},
		{"auth", 401, `{"detailedError":{"code":"NO_ROUTE_FOUND"}}`, false},
		{"quota", 429, `{"detailedError":{"code":"NO_ROUTE_FOUND"}}`, false},
		{"server", 500, `{"detailedError":{"code":"NO_ROUTE_FOUND"}}`, false},
		{"onlyonce", 400, `{"detailedError":{"code":"NO_ROUTE_FOUND","message":"secret-token"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, reservations := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := NewClient(Options{TomTomAPIKey: "secret-token", APIKey: "secret-google", RequestsPerMinute: 120})
			c.URL, c.TomTomURL = server.URL, server.URL
			c.Budget = budgetFunc(func(_ context.Context, o Options) (string, error) {
				reservations++
				if o.TomTomAPIKey != "" {
					return "tomtom", nil
				}
				return "google", nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := c.Route(ctx, 40, -74, 41, -73)
			want := 1
			if tc.fallback {
				want = 2
			}
			if err == nil || calls != want || reservations != want || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "SECRET") {
				t.Fatal("unexpected fallback/replay or leaked error", err, calls, reservations)
			}
			if tc.fallback && !errors.Is(err, ErrTomTomNoRoute) {
				t.Fatal("lost original no-route cause", err)
			}
		})
	}
}
