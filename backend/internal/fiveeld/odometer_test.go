package fiveeld

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCurrentOdometer(t *testing.T) {
	now := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, units, points string
		want                int
		missing             bool
	}{
		{"latest valid reading", `[{"id":"provider-id","vin":"1ABCDEFGH12345678"}]`, `[{"odometer":100001,"date":"2026-10-15T14:30:00Z"},{"odometer":100002.9,"date":"2026-10-15T14:59:00Z"},{"odometer":999999,"date":"2026-10-15T15:01:00Z"}]`, 100002, false},
		{"stale", `[{"id":"provider-id","vin":"1ABCDEFGH12345678"}]`, `[{"odometer":100001,"date":"2026-10-15T12:00:00Z"}]`, 0, true},
		{"missing value", `[{"id":"provider-id","vin":"1ABCDEFGH12345678"}]`, `[{"date":"2026-10-15T14:59:00Z"}]`, 0, true},
		{"different VIN", `[{"id":"provider-id","vin":"1ABCDEFGH12345679"}]`, `[]`, 0, true},
		{"ambiguous identity", `[{"id":"first","vin":"1ABCDEFGH12345678"},{"id":"second","vin":"1ABCDEFGH12345678"}]`, `[]`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-api-key") != "key" || r.Header.Get("provider-token") != "token" {
					t.Error("missing auth")
				}
				if r.URL.Query().Get("to") != "2026-10-15T15:00:00.000Z" {
					t.Error("vendor requires millisecond UTC date")
				}
				if r.URL.Path == "/api/externalservice/active-units/123/" {
					fmt.Fprint(w, tc.units)
				} else if r.URL.Path == "/api/externalservice/trackings/123/provider-id/" {
					fmt.Fprint(w, tc.points)
				} else {
					t.Error("unexpected path", r.URL.Path)
				}
			}))
			defer server.Close()
			c := NewClient(server.URL, "key", "token")
			v, err := c.CurrentOdometer(context.Background(), "123", "1ABCDEFGH12345678", now)
			if tc.missing {
				if !errors.Is(err, ErrOdometerUnavailable) {
					t.Fatal(v, err)
				}
				return
			}
			if err != nil || v.Mileage != tc.want {
				t.Fatal(v, err)
			}
		})
	}
}
