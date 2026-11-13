package boardroutes

import (
	"context"
	"errors"
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
		{"coordinate no route", `{"detailedError":{"code":"NO_ROUTE_FOUND"}}`, 400, false, false},
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
			} else if err == nil || calls != 1 || (tc.name == "coordinate no route" && !errors.Is(err, ErrTomTomNoRoute)) {
				t.Fatal(estimate, err, calls)
			}
		})
	}
}
