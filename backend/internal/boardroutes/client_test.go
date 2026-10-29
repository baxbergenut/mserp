package boardroutes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
				if body["travelMode"] != "DRIVE" || body["routingPreference"] != "TRAFFIC_AWARE_OPTIMAL" {
					t.Error("expected traffic-aware car route")
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
