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

func TestCarRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		fail       bool
	}{
		{"valid", `{"routes":[{"duration":"7200s","distanceMeters":160934,"travelAdvisory":{"routeRestrictionsPartiallyIgnored":false}}]}`, 200, false},
		{"negative distance", `{"routes":[{"duration":"7200s","distanceMeters":-1}]}`, 200, true},
		{"missing distance", `{"routes":[{"duration":"7200s"}]}`, 200, true},
		{"no route", `{"routes":[]}`, 200, true},
		{"invalid", `{"routes":[{"duration":"oops"}]}`, 200, true},
		{"rate limit", `{}`, 429, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Goog-Api-Key") != "test" || r.Method != "POST" {
					t.Error("request authentication/method")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["travelMode"] != "DRIVE" || body["routingPreference"] != "TRAFFIC_UNAWARE" {
					t.Error("expected traffic-unaware car route")
				}
				if _, exists := body["routeModifiers"]; exists {
					t.Error("car estimates must not request truck restrictions or toll calculations")
				}
				if r.Header.Get("X-Goog-FieldMask") != "routes.duration,routes.distanceMeters" {
					t.Error("unexpected paid routing features")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := NewClient(Options{APIKey: "test", RequestsPerMinute: 30})
			c.URL = server.URL
			c.Budget = budgetFunc(func(context.Context, Options) (string, error) { return "google", nil })
			got, err := c.Route(context.Background(), 40, -74, 41, -73)
			if (err != nil) != tc.fail {
				t.Fatal(got, err)
			}
			if !tc.fail && (got.Duration != 2*time.Hour || got.Meters != 160934) {
				t.Fatal(got)
			}
		})
	}
}

func TestRoutingGates(t *testing.T) {
	t.Setenv("BOARD_ROUTES_ENABLED", "false")
	if o, e := LoadOptions(); e != nil || o.Enabled {
		t.Fatal(o, e)
	}
	t.Setenv("BOARD_ROUTES_ENABLED", "true")
	t.Setenv("BOARD_ROUTES_ACCESS_VERIFIED", "")
	if _, e := LoadOptions(); e == nil {
		t.Fatal("unverified routing enabled")
	}
}

func TestRoutingAPIKeyAlias(t *testing.T) {
	for _, name := range []string{"BOARD_ROUTES_ENABLED", "BOARD_ROUTES_ACCESS_VERIFIED", "BOARD_ROUTES_OPERATIONS_VERIFIED", "FIVE_ELD_SPEED_VERIFIED"} {
		t.Setenv(name, "true")
	}
	t.Setenv("FIVE_ELD_SPEED_UNIT", "mph")
	t.Setenv("BOARD_ROUTES_PROFILES_FILE", "unused-legacy-profile.json")
	t.Setenv("BOARD_ROUTES_STANDARD_SEMI", "false")
	t.Setenv("BOARD_ROUTES_REQUESTS_PER_MINUTE", "30")
	t.Setenv("BOARD_ROUTES_API_KEY", "")
	t.Setenv("GOOGLE_ROUTING_API_TOKEN", " alias-test-key ")
	o, err := LoadOptions()
	if err != nil || o.APIKey != "alias-test-key" {
		t.Fatal("routing token alias not loaded")
	}
	t.Setenv("BOARD_ROUTES_API_KEY", "primary-test-key")
	o, err = LoadOptions()
	if err != nil || o.APIKey != "primary-test-key" {
		t.Fatal("primary routing key did not take precedence")
	}
	t.Setenv("BOARD_ROUTES_PROFILES_FILE", "")
	if _, err = LoadOptions(); err != nil {
		t.Fatal("car routes must not require truck profiles", err)
	}
	t.Setenv("BOARD_ROUTES_STANDARD_SEMI", "true")
	o, err = LoadOptions()
	if err != nil {
		t.Fatal("legacy standard semi setting must not affect car routes", err)
	}
}

func TestTomTomRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		fail       bool
	}{
		{"valid", `{"routes":[{"summary":{"travelTimeInSeconds":7200,"lengthInMeters":160934}}]}`, 200, false},
		{"missing duration", `{"routes":[{"summary":{"lengthInMeters":1}}]}`, 200, true},
		{"negative distance", `{"routes":[{"summary":{"travelTimeInSeconds":1,"lengthInMeters":-1}}]}`, 200, true},
		{"overflow duration", `{"routes":[{"summary":{"travelTimeInSeconds":9223372036854775807,"lengthInMeters":1}}]}`, 200, true},
		{"no route", `{"routes":[]}`, 200, true},
		{"unauthorized", `{"secret":"do not log provider bodies"}`, 401, true},
		{"rate limited", `{}`, 429, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reserved, calls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				q := r.URL.Query()
				if reserved != 1 || r.Method != "GET" || !r.Close || q.Get("key") != "secret" || q.Get("traffic") != "true" || q.Get("departAt") != "now" || q.Get("travelMode") != "car" || q.Get("routeRepresentation") != "summaryOnly" || q.Get("maxAlternatives") != "0" {
					t.Error("unexpected/unmetered TomTom request")
				}
				if r.URL.Path != "/40.0000000,-74.0000000:41.0000000,-73.0000000/json" {
					t.Error("coordinates", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := NewClient(Options{TomTomAPIKey: "secret", RequestsPerMinute: 120})
			c.TomTomURL = server.URL
			c.Budget = budgetFunc(func(context.Context, Options) (string, error) { reserved++; return "tomtom", nil })
			got, err := c.Route(context.Background(), 40, -74, 41, -73)
			if (err != nil) != tc.fail || calls != 1 || reserved != 1 {
				t.Fatal(got, err, calls, reserved)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("credential/provider body leaked")
			}
			if !tc.fail && (got.Duration != 2*time.Hour || got.Meters != 160934 || !strings.Contains(got.Assumptions, "TomTom traffic-aware")) {
				t.Fatal(got)
			}
		})
	}
}

func TestRoutingAccountingFailureAndCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	for _, budget := range []Budget{nil, budgetFunc(func(context.Context, Options) (string, error) { return "", ErrBudgetExhausted }), budgetFunc(func(context.Context, Options) (string, error) { return "", errors.New("database unavailable") })} {
		c := NewClient(Options{APIKey: "test"})
		c.URL = server.URL
		c.Budget = budget
		if _, err := c.Route(context.Background(), 40, -74, 41, -73); err == nil {
			t.Fatal("failed open")
		}
	}
	reserved := 0
	c := NewClient(Options{APIKey: "test"})
	c.URL = server.URL
	c.Budget = budgetFunc(func(context.Context, Options) (string, error) { reserved++; return "google", nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Route(ctx, 40, -74, 41, -73); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 0 || reserved != 0 {
		t.Fatal("failure/cancellation sent or reserved", calls, reserved)
	}
}

func TestRoutingDoesNotFollowRedirects(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, "/again", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	c := NewClient(Options{APIKey: "test"})
	c.URL = server.URL
	c.Budget = budgetFunc(func(context.Context, Options) (string, error) { return "google", nil })
	if _, err := c.Route(context.Background(), 40, -74, 41, -73); err == nil || calls != 1 {
		t.Fatal("redirect replay", calls, err)
	}
}

func TestRoutingBudgetConfig(t *testing.T) {
	for _, name := range []string{"BOARD_ROUTES_ENABLED", "BOARD_ROUTES_ACCESS_VERIFIED", "BOARD_ROUTES_OPERATIONS_VERIFIED", "FIVE_ELD_SPEED_VERIFIED"} {
		t.Setenv(name, "true")
	}
	t.Setenv("BOARD_ROUTES_API_KEY", "google")
	t.Setenv("TOMTOM_ROUTING_API_KEY", "tomtom")
	t.Setenv("FIVE_ELD_SPEED_UNIT", "mph")
	t.Setenv("BOARD_ROUTES_GOOGLE_MONTHLY_FREE", "")
	t.Setenv("BOARD_ROUTES_TOMTOM_MONTHLY_FREE", "")
	t.Setenv("BOARD_ROUTES_PAID_PROVIDER", "")
	o, err := LoadOptions()
	if err != nil || o.GoogleMonthlyFree != 10000 || o.TomTomMonthlyFree != 20000 || o.PaidProvider != "off" {
		t.Fatal("defaults", err)
	}
	for _, tc := range []struct{ name, value string }{{"BOARD_ROUTES_GOOGLE_MONTHLY_FREE", "10001"}, {"BOARD_ROUTES_TOMTOM_MONTHLY_FREE", "20001"}, {"BOARD_ROUTES_GOOGLE_MONTHLY_FREE", "-1"}, {"BOARD_ROUTES_GOOGLE_MONTHLY_FREE", "oops"}, {"BOARD_ROUTES_PAID_PROVIDER", "other"}, {"BOARD_ROUTES_TOMTOM_BILLING_TIMEZONE", "invalid"}} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			if _, err := LoadOptions(); err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
	t.Setenv("BOARD_ROUTES_PAID_PROVIDER", "tomtom")
	t.Setenv("TOMTOM_ROUTING_API_KEY", "")
	if _, err := LoadOptions(); err == nil {
		t.Fatal("paid provider enabled without key")
	}
}
