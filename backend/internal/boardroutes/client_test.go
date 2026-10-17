package boardroutes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
