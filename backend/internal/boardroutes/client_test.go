package boardroutes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTomTomItineraryUsesOneRequestAndKeepsActiveStopEstimate(t *testing.T) {
	calls, reservations := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assertTruckProfile(t, r)
		if !strings.HasSuffix(r.URL.Path, "/40.0000000,-74.0000000:41.0000000,-73.0000000:42.0000000,-72.0000000:43.0000000,-71.0000000/json") {
			t.Errorf("unexpected waypoint order: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"routes":[{"summary":{"travelTimeInSeconds":3000,"lengthInMeters":20000},"legs":[
			{"summary":{"travelTimeInSeconds":600,"lengthInMeters":3000},"encodedPolyline":"first","encodedPolylinePrecision":5},
			{"summary":{"travelTimeInSeconds":900,"lengthInMeters":5000},"encodedPolyline":"second","encodedPolylinePrecision":5},
			{"summary":{"travelTimeInSeconds":1500,"lengthInMeters":12000},"encodedPolyline":"third","encodedPolylinePrecision":7}
		]}]}`))
	}))
	defer server.Close()
	client := NewClient(Options{TomTomAPIKey: "fixture", RequestsPerMinute: 120})
	client.TomTomURL = server.URL
	client.Budget = budgetFunc(func(context.Context, Options) error { reservations++; return nil })
	estimate, err := client.Route(context.Background(), 40, -74, 41, -73, Waypoint{42, -72}, Waypoint{43, -71})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || reservations != 1 || estimate.Duration != 10*time.Minute || estimate.Meters != 3000 || estimate.Geometry == nil || estimate.Geometry.Polyline != "first" {
		t.Fatalf("calls=%d reservations=%d estimate=%+v", calls, reservations, estimate)
	}
	if len(estimate.Following) != 2 || estimate.Following[0].Meters != 5000 || estimate.Following[1].Meters != 12000 || estimate.Following[1].Geometry == nil || estimate.Following[1].Geometry.Precision != 7 {
		t.Fatalf("unexpected following legs: %+v", estimate.Following)
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

func TestTomTomRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		fail       bool
		geometry   bool
	}{
		{"valid with geometry", `{"routes":[{"summary":{"travelTimeInSeconds":7200,"lengthInMeters":160934},"legs":[{"encodedPolyline":"_p~iF~ps|U_ulLnnqC","encodedPolylinePrecision":5}]}]}`, 200, false, true},
		{"missing geometry still yields ETA", `{"routes":[{"summary":{"travelTimeInSeconds":7200,"lengthInMeters":160934}}]}`, 200, false, false},
		{"missing duration", `{"routes":[{"summary":{"lengthInMeters":1}}]}`, 200, true, false},
		{"negative distance", `{"routes":[{"summary":{"travelTimeInSeconds":1,"lengthInMeters":-1}}]}`, 200, true, false},
		{"overflow duration", `{"routes":[{"summary":{"travelTimeInSeconds":9223372036854775807,"lengthInMeters":1}}]}`, 200, true, false},
		{"no route", `{"routes":[]}`, 200, true, false},
		{"unauthorized", `{"secret":"do not log provider bodies"}`, 401, true, false},
		{"rate limited", `{}`, 429, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reserved, calls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				q := r.URL.Query()
				if reserved != 1 || r.Method != "GET" || !r.Close || q.Get("key") != "secret" || q.Get("traffic") != "true" || q.Get("departAt") != "now" || q.Get("routeRepresentation") != "encodedPolyline" || q.Get("maxAlternatives") != "0" {
					t.Error("unexpected/unmetered TomTom request")
				}
				assertTruckProfile(t, r)
				if r.URL.Path != "/40.0000000,-74.0000000:41.0000000,-73.0000000/json" {
					t.Error("coordinates", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := NewClient(Options{TomTomAPIKey: "secret", RequestsPerMinute: 120})
			c.TomTomURL = server.URL
			c.Budget = budgetFunc(func(context.Context, Options) error { reserved++; return nil })
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
			if (got.Geometry != nil) != tc.geometry || (tc.geometry && (got.Geometry.Polyline != "_p~iF~ps|U_ulLnnqC" || got.Geometry.Precision != 5)) {
				t.Fatalf("unexpected route geometry: %+v", got.Geometry)
			}
		})
	}
}

func TestRoutingAccountingFailureAndCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	for _, budget := range []Budget{nil, budgetFunc(func(context.Context, Options) error { return ErrBudgetExhausted }), budgetFunc(func(context.Context, Options) error { return errors.New("database unavailable") })} {
		c := NewClient(Options{TomTomAPIKey: "test"})
		c.TomTomURL = server.URL
		c.Budget = budget
		if _, err := c.Route(context.Background(), 40, -74, 41, -73); err == nil {
			t.Fatal("failed open")
		}
	}
	reserved := 0
	c := NewClient(Options{TomTomAPIKey: "test"})
	c.TomTomURL = server.URL
	c.Budget = budgetFunc(func(context.Context, Options) error { reserved++; return nil })
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
	c := NewClient(Options{TomTomAPIKey: "test"})
	c.TomTomURL = server.URL
	c.Budget = budgetFunc(func(context.Context, Options) error { return nil })
	if _, err := c.Route(context.Background(), 40, -74, 41, -73); err == nil || calls != 1 {
		t.Fatal("redirect replay", calls, err)
	}
}

func TestRoutingBudgetConfig(t *testing.T) {
	for _, name := range []string{"BOARD_ROUTES_ENABLED", "BOARD_ROUTES_ACCESS_VERIFIED", "BOARD_ROUTES_OPERATIONS_VERIFIED", "FIVE_ELD_SPEED_VERIFIED"} {
		t.Setenv(name, "true")
	}
	t.Setenv("TOMTOM_ROUTING_API_KEY", "tomtom")
	t.Setenv("FIVE_ELD_SPEED_UNIT", "mph")
	t.Setenv("BOARD_ADDRESS_GEOCODE_MONTHLY_LIMIT", "")
	t.Setenv("BOARD_ROUTES_TOMTOM_MONTHLY_FREE", "")
	t.Setenv("BOARD_ROUTES_PAID_PROVIDER", "")
	o, err := LoadOptions()
	if err != nil || o.AddressGeocodeMonthlyLimit != 1000 || o.TomTomMonthlyFree != 20000 || o.PaidProvider != "off" {
		t.Fatal("defaults", err)
	}
	for _, tc := range []struct{ name, value string }{{"BOARD_ADDRESS_GEOCODE_MONTHLY_LIMIT", "1001"}, {"BOARD_ROUTES_TOMTOM_MONTHLY_FREE", "20001"}, {"BOARD_ADDRESS_GEOCODE_MONTHLY_LIMIT", "-1"}, {"BOARD_ADDRESS_GEOCODE_MONTHLY_LIMIT", "oops"}, {"BOARD_ROUTES_PAID_PROVIDER", "google"}, {"BOARD_ROUTES_PAID_PROVIDER", "other"}, {"BOARD_ROUTES_TOMTOM_BILLING_TIMEZONE", "invalid"}} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			if _, err := LoadOptions(); err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
	t.Setenv("BOARD_ROUTES_API_KEY", "legacy-google-key")
	t.Setenv("TOMTOM_ROUTING_API_KEY", "")
	if _, err := LoadOptions(); err == nil {
		t.Fatal("routing enabled with only a legacy Google key")
	}
}

func assertTruckProfile(t *testing.T, r *http.Request) {
	t.Helper()
	for key, want := range map[string]string{
		"travelMode": "truck", "vehicleCommercial": "true", "vehicleNumberOfAxles": "5",
		"vehicleLength": "22.4028", "vehicleWidth": "2.5908", "vehicleHeight": "4.1148",
		"vehicleWeight": "36287", "sectionType": "travelMode",
	} {
		if got := r.URL.Query().Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}
