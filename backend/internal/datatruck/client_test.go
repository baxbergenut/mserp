package datatruck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoRequestRetriesRateLimit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":0,"results":[]}`))
	}))
	defer server.Close()

	client := &Client{httpClient: server.Client(), apiKey: "test"}
	response, err := client.doRequest(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if response.Count != 0 || requests.Load() != 2 {
		t.Fatalf("response count = %d, requests = %d", response.Count, requests.Load())
	}
}

func TestDoRequestDoesNotRetryBadRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := &Client{httpClient: server.Client(), apiKey: "test"}
	if _, err := client.doRequest(context.Background(), server.URL); err == nil {
		t.Fatal("expected an error")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestRetryDelay(t *testing.T) {
	if delay := retryDelay("12", 0); delay != 12*time.Second {
		t.Fatalf("Retry-After delay = %s", delay)
	}
	if delay := retryDelay("", 2); delay != 4*time.Second {
		t.Fatalf("fallback delay = %s", delay)
	}
}
