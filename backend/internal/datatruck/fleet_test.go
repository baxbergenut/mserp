package datatruck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFleetCreateNeverRetries(t *testing.T) {
	for _, status := range []int{201, 400, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/drivers/create/" || r.Method != "POST" || r.Header.Get("Authorization") != "Token secret" {
					t.Error("wrong request")
				}
				w.WriteHeader(status)
				if status == 201 {
					w.Write([]byte(`{"detail":"success"}`))
				} else {
					w.Write([]byte(`{"email":"private@example.test"}`))
				}
			}))
			defer s.Close()
			c := NewClient("secret", "test")
			c.baseURL = s.URL
			c.gate = nil
			_, err := c.CreateFleetRecord(context.Background(), "drivers", json.RawMessage(`{"first_name":"Test"}`))
			if status == 201 && err != nil || status != 201 && err == nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("POST retried %d times", calls)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("vendor response leaked")
			}
		})
	}
}
func TestFleetListRejectsForeignPagination(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"results":[],"next":"https://example.invalid/steal"}`))
	}))
	defer s.Close()
	c := NewClient("secret", "test")
	c.baseURL = s.URL
	c.gate = nil
	if _, err := c.FleetList(context.Background(), "drivers", nil); err == nil {
		t.Fatal("foreign pagination accepted")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestDriverUpdateNeverRetries(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "PUT" || r.URL.Path != "/drivers/update/3098/" {
			t.Error("wrong endpoint")
		}
		w.WriteHeader(503)
		w.Write([]byte(`{"detail":"private"}`))
	}))
	defer s.Close()
	c := NewClient("secret", "test")
	c.baseURL = s.URL
	c.gate = nil
	_, err := c.UpdateDriver(context.Background(), 3098, json.RawMessage(`{"brith_date":"1990-01-01"}`))
	e, ok := err.(*FleetError)
	if !ok || !e.Uncertain || calls != 1 {
		t.Fatal("uncertain PUT replayed or not held", calls, err)
	}
}
