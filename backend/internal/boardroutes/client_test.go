package boardroutes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTruckRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		fail       bool
	}{
		{"valid", `{"routes":[{"duration":"7200s","distanceMeters":160934,"travelAdvisory":{"routeRestrictionsPartiallyIgnored":false}}]}`, 200, false},
		{"restrictions", `{"routes":[{"duration":"7200s","distanceMeters":160934,"travelAdvisory":{"routeRestrictionsPartiallyIgnored":true}}]}`, 200, true},
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
				if body["travelMode"] != "TRUCK" || body["routingPreference"] != "TRAFFIC_AWARE_OPTIMAL" {
					t.Error("car routing or traffic unaware fallback")
				}
				if r.Header.Get("X-Goog-FieldMask") == "" {
					t.Error("missing field mask")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := NewClient(Options{APIKey: "test", RequestsPerMinute: 30})
			c.URL = server.URL
			got, err := c.Route(context.Background(), 40, -74, 41, -73, VehicleInfo{})
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
	p := Profile{LoadID: 1, Trailer: "A", ValidUntil: time.Now().Add(time.Hour), Vehicle: VehicleInfo{Height: 4000, Length: 22000, Width: 2600, Weight: 30000, TrailerInfo: []TrailerInfo{{Length: 16000}}, HazardousGoods: []string{}}}
	if !p.Valid(1, "A", time.Now()) || p.Valid(2, "A", time.Now()) || p.Valid(1, "B", time.Now()) || p.Valid(1, "A", time.Now().Add(2*time.Hour)) {
		t.Fatal("routing profile is not context-bound")
	}
}

func TestRoutingAPIKeyAlias(t *testing.T) {
	profiles := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(profiles, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"BOARD_ROUTES_ENABLED", "BOARD_ROUTES_ACCESS_VERIFIED", "BOARD_ROUTES_OPERATIONS_VERIFIED", "FIVE_ELD_SPEED_VERIFIED"} {
		t.Setenv(name, "true")
	}
	t.Setenv("FIVE_ELD_SPEED_UNIT", "mph")
	t.Setenv("BOARD_ROUTES_PROFILES_FILE", profiles)
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
	if _, err = LoadOptions(); err == nil {
		t.Fatal("missing profiles accepted without opt-in")
	}
	t.Setenv("BOARD_ROUTES_STANDARD_SEMI", "true")
	o, err = LoadOptions()
	if err != nil || !o.StandardSemi {
		t.Fatal("standard semi opt-in failed", err)
	}
}

func TestStandardSemiProfile(t *testing.T) {
	now := time.Now()
	o := Options{}
	if _, _, ok := o.VehicleFor("truck", 1, "trailer", now); ok {
		t.Fatal("standard profile enabled implicitly")
	}
	o.StandardSemi = true
	v, assumptions, ok := o.VehicleFor("truck", 1, "trailer", now)
	if !ok || v.Weight != 36288 || v.Height != 4115 || len(v.TrailerInfo) != 1 || assumptions != StandardSemiAssumptions {
		t.Fatal("missing approved profile or disclosure")
	}
	if _, _, ok := o.VehicleFor("truck", 0, "trailer", now); ok {
		t.Fatal("unresolved load used standard profile")
	}
	o.Profiles = map[string]Profile{"truck": {LoadID: 2}}
	if _, _, ok := o.VehicleFor("truck", 1, "trailer", now); ok {
		t.Fatal("invalid explicit profile fell back to standard")
	}
	p := Profile{LoadID: 1, Trailer: "trailer", ValidUntil: now.Add(time.Hour), Vehicle: v}
	o.Profiles["truck"] = p
	got, assumptions, ok := o.VehicleFor("truck", 1, "trailer", now)
	if !ok || got.Weight != v.Weight || assumptions != "" {
		t.Fatal("valid explicit profile did not take precedence")
	}
}
