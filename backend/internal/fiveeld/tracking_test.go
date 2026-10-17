package fiveeld

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRecentTracking(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" || r.Header.Get("provider-token") != "provider" {
			t.Error("missing tracking authentication")
		}
		switch r.URL.Path {
		case "/api/externalservice/active-units/123/":
			fmt.Fprint(w, `[{"id":"vehicle","vin":"1M8GDM9AXKP042789"}]`)
		case "/api/externalservice/trackings/123/vehicle/":
			if r.URL.Query().Get("from") != "2026-10-17T11:50:00.000Z" {
				t.Error("unbounded tracking request")
			}
			fmt.Fprint(w, `[{"date":"2026-10-17T12:00:00Z","coordinates":{"lat":40,"lng":-74},"speed":45},{"date":"2026-10-17T11:56:00Z","coordinates":{"lat":40,"lng":-74},"speed":0},{"date":"2026-10-17T11:00:00Z","coordinates":{"lat":40,"lng":-74},"speed":10},{"date":"2026-10-17T12:01:00Z","coordinates":{"lat":40,"lng":-74},"speed":10},{"date":"2026-10-17T12:00:00Z","speed":0}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClientWithHTTPClient(server.URL, "key", "provider", server.Client())
	points, err := client.RecentTracking(context.Background(), "123", "1M8GDM9AXKP042789", now)
	if err != nil || len(points) != 2 || !points[0].Date.Before(points[1].Date) || *points[1].Speed != 45 {
		t.Fatal(points, err)
	}
}
