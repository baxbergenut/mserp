package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDriverPagination(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "fixture-key" || r.URL.Query().Get("limit") != "50" {
			t.Error("missing auth or page size")
		}
		if r.URL.Query().Get("offset") != fmt.Sprint(calls*50) {
			t.Error("incorrect offset")
		}
		calls++
		page := []Driver{}
		if calls == 1 {
			for i := 0; i < 50; i++ {
				page = append(page, Driver{ID: fmt.Sprintf("dr_%d", i)})
			}
		} else {
			page = append(page, Driver{ID: "dr_last"})
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer server.Close()
	result, err := NewClient(server.URL, "fixture-key").Drivers(context.Background())
	if err != nil || len(result) != 51 || calls != 2 {
		t.Fatalf("pagination: %d %d %v", len(result), calls, err)
	}
}
func TestDriverRepeatedPageFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := []Driver{}
		for i := 0; i < 50; i++ {
			page = append(page, Driver{ID: fmt.Sprintf("dr_%d", i)})
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer server.Close()
	if _, err := NewClient(server.URL, "").Drivers(context.Background()); err == nil {
		t.Fatal("accepted repeated page")
	}
}
func TestDriverPolicyWireContract(t *testing.T) {
	var method, path, query string
	var body map[string]any
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		query = r.URL.Query().Get("driver_id")
		body = nil
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		w.WriteHeader(status)
		if r.Method == "GET" && status == 200 {
			fmt.Fprint(w, `{"id":"pa_one","policy_id":"plc_one","enabled":true}`)
		}
	}))
	defer server.Close()
	c := NewClient(server.URL, "")
	ctx := context.Background()
	if err := c.EnablePolicy(ctx, "dr_one", false); err != nil {
		t.Fatal(err)
	}
	if method != "PUT" || path != "/integrations/fuel/policies/policy-assignments/dr_one" || query != "" || body["enabled"] != false || len(body) != 1 {
		t.Fatalf("enable wire %s %s %s %v", method, path, query, body)
	}
	if err := c.AssignPolicy(ctx, "dr_one", "plc_one"); err != nil {
		t.Fatal(err)
	}
	if method != "POST" || path != "/integrations/fuel/policies/policy-assignments/" || body["driver_id"] != "dr_one" || body["policy_id"] != "plc_one" {
		t.Fatal("assignment wire")
	}
	if err := c.UnassignPolicy(ctx, "dr_one"); err != nil {
		t.Fatal(err)
	}
	if method != "DELETE" || path != "/integrations/fuel/policies/policy-assignments/dr_one" || query != "" {
		t.Fatal("unassignment wire")
	}
	status = 404
	a, err := c.Assignment(ctx, "dr_one")
	if err != nil || a != nil {
		t.Fatal("no policy is not an error")
	}
}
func TestDriverErrorsAreRedactedAndDoNotRetry(t *testing.T) {
	for _, status := range []int{400, 408, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				fmt.Fprint(w, "secret-pin-and-api-key")
			}))
			defer server.Close()
			err := NewClient(server.URL, "secret").RemoveDriver(context.Background(), "dr_one")
			var api *APIError
			if !errors.As(err, &api) || api.Uncertain != (status >= 500 || status == 408) || calls != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe error: %v calls=%d", err, calls)
			}
		})
	}
}
func TestDriverMutationRedirectIsNotFollowed(t *testing.T) {
	received := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	err := NewClient(server.URL, "fixture-secret").RemoveDriver(context.Background(), "dr_one")
	if err == nil || received {
		t.Fatal("followed mutation redirect")
	}
}
