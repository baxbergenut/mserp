package datatruck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestFetchLoadsByDateSinceUsesRequestedColumn(t *testing.T) {
	var receivedFilter []map[string]string
	var receivedOrdering string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedOrdering = r.URL.Query().Get("ordering")
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filter")), &receivedFilter); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":0,"results":[]}`))
	}))
	defer server.Close()

	client := &Client{httpClient: server.Client(), apiKey: "test", baseURL: server.URL}
	since := time.Date(2026, time.July, 1, 12, 30, 0, 0, time.FixedZone("test", -4*60*60))
	if _, err := client.FetchLoadsByDateSince(context.Background(), "pickup_time", since); err != nil {
		t.Fatal(err)
	}

	if receivedOrdering != "-pickup_time" {
		t.Fatalf("ordering = %q", receivedOrdering)
	}
	if len(receivedFilter) != 1 {
		t.Fatalf("filter count = %d", len(receivedFilter))
	}
	if receivedFilter[0]["column"] != "pickup_time" {
		t.Fatalf("filter column = %q", receivedFilter[0]["column"])
	}
	if receivedFilter[0]["value"] != "2026-07-01T16:30:00Z" {
		t.Fatalf("filter value = %q", receivedFilter[0]["value"])
	}
	if receivedFilter[0]["contains"] != "after" {
		t.Fatalf("filter operation = %q", receivedFilter[0]["contains"])
	}
}

func TestFetchLoadsAfterIDPaginatesAndUsesNumericWatermark(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"count":2,"results":[{"id":102,"load_id":"B"}]}`))
			return
		}

		var filters []map[string]string
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filter")), &filters); err != nil {
			t.Fatal(err)
		}
		if len(filters) != 1 || filters[0]["column"] != "id" || filters[0]["value"] != "101" || filters[0]["contains"] != "greater_than" {
			t.Fatalf("filters = %#v", filters)
		}
		if r.URL.Query().Get("ordering") != "id" {
			t.Fatalf("ordering = %q", r.URL.Query().Get("ordering"))
		}
		if r.URL.Query().Get("page_size") != "25" {
			t.Fatalf("page_size = %q", r.URL.Query().Get("page_size"))
		}
		next := serverURLWithQuery(r, "page", "2")
		_, _ = w.Write([]byte(`{"count":2,"next":` + mustJSON(t, next) + `,"results":[{"id":101,"load_id":"A"}]}`))
	}))
	defer server.Close()

	client := &Client{httpClient: server.Client(), apiKey: "test", baseURL: server.URL}
	loads, err := client.FetchLoadsAfterID(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(loads) != 2 || loads[0].ID != 101 || loads[1].ID != 102 {
		t.Fatalf("loads = %#v", loads)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}
}

func TestFetchLoadsAfterIDStopsIfNumericFilterIsIgnored(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"count":35000,"next":"?page=2","results":[{"id":1}]}`))
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), baseURL: server.URL}
	if _, err := client.FetchLoadsAfterID(context.Background(), 100); err == nil {
		t.Fatal("expected an error for ignored numeric filter")
	}
	if requests.Load() != 1 {
		t.Fatalf("requested %d pages after detecting an ignored filter", requests.Load())
	}
}

func TestDateReconciliationExcludesAlreadyFetchedNewIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var filters []map[string]string
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filter")), &filters); err != nil {
			t.Fatal(err)
		}
		if len(filters) != 2 || filters[0]["column"] != "delivery_time" || filters[0]["contains"] != "after" ||
			filters[1]["column"] != "id" || filters[1]["contains"] != "less_than" || filters[1]["value"] != "100" {
			t.Fatalf("filters = %#v", filters)
		}
		_, _ = w.Write([]byte(`{"count":1,"results":[{"id":100}]}`))
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), baseURL: server.URL}
	loads, err := client.FetchLoadsByDateSinceThroughID(context.Background(), "delivery_time", time.Now(), 100)
	if err != nil || len(loads) != 1 || loads[0].ID != 100 {
		t.Fatalf("loads = %+v, error = %v", loads, err)
	}
}

func TestResolveNextURLHandlesAbsolutePaths(t *testing.T) {
	got := resolveNextURL(
		"https://example.com/api/v1/openapi",
		"/api/v1/openapi/orders/?page=2",
	)
	want := "https://example.com/api/v1/openapi/orders/?page=2"
	if got != want {
		t.Fatalf("resolveNextURL() = %q, want %q", got, want)
	}
}

func serverURLWithQuery(r *http.Request, key, value string) string {
	next := url.URL{
		Scheme:   "http",
		Host:     r.Host,
		Path:     r.URL.Path,
		RawQuery: url.Values{key: []string{value}}.Encode(),
	}
	return next.String()
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
