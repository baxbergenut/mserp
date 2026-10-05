package fiveeld

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientReadsPositionsDirectoryAndAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" || r.Header.Get("provider-token") != "provider" {
			t.Fatal("missing authentication headers")
		}
		switch r.URL.Path {
		case "/api/v2/units-by-usdot/123456":
			fmt.Fprint(w, `{"units":[{"truck_number":"17","vin":"1M8GDM9AXKP042788","coordinates":{"lat":41.881,"lng":-87.623},"timestamp":"2026-10-05T14:30:00Z"}]}`)
		case "/api/externalservice/current-units/123456":
			if r.URL.Query().Get("is_active") != "true" {
				t.Fatal("active filter missing")
			}
			fmt.Fprint(w, `{"data":[{"id":"vehicle-17","truck_number":"17","vin":"1M8GDM9AXKP042788"}],"meta":{"page":1,"totalPages":1}}`)
		case "/api/externalservice/trackings/123456/vehicle-17/":
			fmt.Fprint(w, `[{"address":"Chicago, IL","coordinates":{"lat":41.881,"lng":-87.623},"date":"2026-10-05T14:30:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClientWithHTTPClient(server.URL, "key", "provider", server.Client())
	ctx := context.Background()
	positions, err := client.CurrentPositions(ctx, "123456")
	if err != nil || len(positions) != 1 || positions[0].VIN != "1M8GDM9AXKP042788" {
		t.Fatalf("positions: %+v %v", positions, err)
	}
	units, err := client.ActiveUnits(ctx, "123456")
	if err != nil || len(units) != 1 || units[0].ID != "vehicle-17" {
		t.Fatalf("units: %+v %v", units, err)
	}
	point, ok, err := client.LatestTracking(ctx, "123456", units[0].ID, time.Now().Add(-time.Hour), time.Now())
	if err != nil || !ok || point.Address != "Chicago, IL" {
		t.Fatalf("tracking: %+v %v %v", point, ok, err)
	}
}

func TestClientDoesNotLeakResponseBodyInErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "secret response")
	}))
	defer server.Close()
	_, err := NewClientWithHTTPClient(server.URL, "key", "provider", server.Client()).CurrentPositions(context.Background(), "1")
	if err == nil || err.Error() != "Five ELD returned 401 Unauthorized" {
		t.Fatalf("unexpected error: %v", err)
	}
}
