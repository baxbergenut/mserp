package fiveeld

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

func TestTrackingHistoryWindowAndCache(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	var trackCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/externalservice/active-units/123/":
			fmt.Fprint(w, `[{"id":"vehicle","vin":"1M8GDM9AXKP042789"}]`)
		case "/api/externalservice/trackings/123/vehicle/":
			trackCalls.Add(1)
			if r.URL.Query().Get("from") != "2026-10-16T12:00:00.000Z" || r.URL.Query().Get("to") != "2026-10-17T12:00:00.000Z" {
				t.Error("expected 24-hour tracking window")
			}
			fmt.Fprint(w, `[
				{"date":"2026-10-16T11:59:00Z","coordinates":{"lat":40,"lng":-74}},
				{"date":"2026-10-16T13:00:00Z","coordinates":{"lat":40,"lng":-74}},
				{"date":"2026-10-16T13:01:00Z","coordinates":{"lat":40.01,"lng":-74}},
				{"date":"2026-10-16T13:04:00Z","coordinates":{"lat":40.04,"lng":-74}},
				{"date":"2026-10-16T13:13:00Z","coordinates":{"lat":40.13,"lng":-74}},
				{"date":"2026-10-16T13:16:00Z","coordinates":{"lat":40.16,"lng":-74}},
				{"date":"2026-10-16T13:30:00Z","coordinates":{"lat":40.30,"lng":-74}},
				{"date":"2026-10-16T13:31:00Z","coordinates":{"lat":40.31,"lng":-74}}
			]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClientWithHTTPClient(server.URL, "key", "provider", server.Client())
	for _, at := range []time.Time{now, now.Add(time.Minute)} {
		points, err := client.TrackingHistory(context.Background(), "123", "1M8GDM9AXKP042789", at)
		minutes := []int{0, 4, 13, 16, 30, 31}
		if err != nil || len(points) != len(minutes) {
			t.Fatal(points, err)
		}
		for i, minute := range minutes {
			want := time.Date(2026, 10, 16, 13, minute, 0, 0, time.UTC)
			if !points[i].Date.Equal(want) {
				t.Fatalf("sample %d at %s, want %s", i, points[i].Date, want)
			}
		}
	}
	if calls := trackCalls.Load(); calls != 1 {
		t.Fatalf("requested tracking %d times", calls)
	}
}
