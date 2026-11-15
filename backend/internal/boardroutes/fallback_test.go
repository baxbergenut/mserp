package boardroutes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTruckRouteRetry(t *testing.T) {
	for _, tc := range []struct {
		name, response   string
		status           int
		following, retry bool
	}{
		{"itinerary no route", `{"detailedError":{"code":"NO_ROUTE_FOUND"}}`, 400, true, true},
		{"truck mode unavailable in a segment", `{"routes":[{"sections":[{"sectionType":"TRAVEL_MODE","travelMode":"other"}]}]}`, 200, true, true},
		{"auth", `{"detailedError":{"code":"NO_ROUTE_FOUND"}}`, 401, true, false},
		{"quota", `{}`, 429, true, false},
		{"unknown bad request", `{}`, 400, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, reserved := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assertTruckProfile(t, r)
				if reserved != calls {
					t.Error("route attempt was not reserved")
				}
				wantFollowing := calls == 1 && tc.following
				if strings.Contains(r.URL.Path, ":42.0000000,-72.0000000") != wantFollowing {
					t.Error("incorrect waypoint list", r.URL.Path)
				}
				if calls == 1 {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.response))
					return
				}
				_, _ = w.Write([]byte(`{"routes":[{"summary":{"travelTimeInSeconds":60,"lengthInMeters":1000}}]}`))
			}))
			defer server.Close()
			client := NewClient(Options{TomTomAPIKey: "test", RequestsPerMinute: 120})
			client.TomTomURL = server.URL
			client.Budget = budgetFunc(func(context.Context, Options) error { reserved++; return nil })
			var following []Waypoint
			if tc.following {
				following = []Waypoint{{42, -72}}
			}
			estimate, err := client.Route(context.Background(), 40, -74, 41, -73, following...)
			if tc.retry {
				if err != nil || calls != 2 || !estimate.FollowingRejected || estimate.Meters != 1000 || len(estimate.Following) != 0 {
					t.Fatal(estimate, err, calls)
				}
			} else if err == nil || calls != 1 {
				t.Fatal(estimate, err, calls)
			}
		})
	}
}

func TestCarFallback(t *testing.T) {
	for _, tc := range []struct {
		name                                      string
		following, section, carFails, budgetFails bool
	}{
		{name: "active stop"},
		{name: "truck segment unavailable", section: true},
		{name: "itinerary", following: true},
		{name: "car also unavailable", carFails: true},
		{name: "fallback budget exhausted", budgetFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, reserved := 0, 0
			truckCalls := 1
			if tc.following {
				truckCalls = 2
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if reserved != calls {
					t.Error("unmetered attempt")
				}
				if calls <= truckCalls {
					assertTruckProfile(t, r)
					if tc.section {
						_, _ = w.Write([]byte(`{"routes":[{"sections":[{"sectionType":"TRAVEL_MODE","travelMode":"other"}]}]}`))
					} else {
						w.WriteHeader(400)
						_, _ = w.Write([]byte(`{"detailedError":{"code":"NO_ROUTE_FOUND"}}`))
					}
					return
				}
				q := r.URL.Query()
				if q.Get("travelMode") != "car" || q.Get("traffic") != "true" || q.Get("departAt") != "now" {
					t.Error("incorrect car request")
				}
				for key := range q {
					if strings.HasPrefix(key, "vehicle") {
						t.Errorf("truck restriction in car request: %s", key)
					}
				}
				if strings.Contains(r.URL.Path, ":42.0000000,-72.0000000") {
					t.Error("fallback must estimate active stop only")
				}
				if tc.carFails {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"detailedError":{"code":"NO_ROUTE_FOUND"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"routes":[{"sections":[{"sectionType":"TRAVEL_MODE","travelMode":"car"}],"summary":{"travelTimeInSeconds":600,"lengthInMeters":10000},"legs":[{"encodedPolyline":"car-path","encodedPolylinePrecision":5}]}]}`))
			}))
			defer server.Close()
			client := NewClient(Options{TomTomAPIKey: "test", RequestsPerMinute: 120})
			client.TomTomURL = server.URL
			client.Budget = budgetFunc(func(context.Context, Options) error {
				reserved++
				if tc.budgetFails && reserved > truckCalls {
					return ErrBudgetExhausted
				}
				return nil
			})
			var following []Waypoint
			if tc.following {
				following = []Waypoint{{42, -72}}
			}
			estimate, err := client.Route(context.Background(), 40, -74, 41, -73, following...)
			wantCalls := truckCalls + 1
			if tc.budgetFails {
				wantCalls--
			}
			if calls != wantCalls || reserved != truckCalls+1 || (err != nil) != (tc.carFails || tc.budgetFails) {
				t.Fatalf("calls=%d reserved=%d estimate=%+v err=%v", calls, reserved, estimate, err)
			}
			if err == nil && (estimate.Meters != 10000 || estimate.Duration.Seconds() != 600 || estimate.Assumptions != CarFallbackAssumptions || estimate.Geometry == nil || estimate.Geometry.Polyline != "car-path" || estimate.FollowingRejected != tc.following || len(estimate.Following) != 0) {
				t.Fatalf("bad fallback: %+v", estimate)
			}
		})
	}
}
