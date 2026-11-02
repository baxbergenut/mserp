package fiveeld

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientReadsCurrentPositions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" || r.Header.Get("provider-token") != "provider" {
			t.Fatal("missing authentication headers")
		}
		switch r.URL.Path {
		case "/api/v2/units-by-usdot/123456":
			fmt.Fprint(w, `{"units":[{"truck_number":"17","vin":"1M8GDM9AXKP042788","coordinates":{"lat":41.881,"lng":-87.623},"rotation":90,"timestamp":"2026-10-05T14:30:00Z"}]}`)
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
	if positions[0].Heading == nil || *positions[0].Heading != 90 {
		t.Fatalf("missing heading: %+v", positions[0])
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

func TestClientKeepsInvalidTimestampUnitsWithoutAbortingPositions(t *testing.T) {
	for _, timestamp := range []string{`null`, `""`, `"invalid"`, `"2026-11-02T09:00:00"`} {
		t.Run(timestamp, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"units":[{"truck_number":"3065","vin":"1M8GDM9AXKP042788","timestamp":%s},{"truck_number":"17","vin":"1M8GDM9AXKP042789","timestamp":"2026-11-02T14:30:00Z"}]}`, timestamp)
			}))
			defer server.Close()
			positions, err := NewClientWithHTTPClient(server.URL, "key", "provider", server.Client()).CurrentPositions(context.Background(), "1")
			if err != nil || len(positions) != 2 {
				t.Fatalf("positions=%+v err=%v", positions, err)
			}
			if positions[0].TruckNumber != "3065" || !positions[0].ReportedAt.IsZero() {
				t.Fatalf("invalid observation gained a timestamp: %+v", positions[0])
			}
			if got := positions[1].ReportedAt.Format("2006-01-02T15:04:05Z07:00"); got != "2026-11-02T14:30:00Z" {
				t.Fatalf("valid observation timestamp=%s", got)
			}
		})
	}
}
