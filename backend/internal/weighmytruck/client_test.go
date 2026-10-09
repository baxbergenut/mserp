package weighmytruck

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientWireAndFailures(t *testing.T) {
	tokens, calls, status := 0, 0, 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokens++
			r.ParseForm()
			if r.Form.Get("client_secret") != "secret~+&" || r.Form.Get("scope") != "scope" || r.Form.Get("grant_type") != "client_credentials" {
				t.Error("bad OAuth form")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"access_token":"private-token","expires_in":3600}`))
			return
		}
		calls++
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing bearer")
		}
		if strings.HasSuffix(r.URL.Path, "RemoveDriver") {
			var email string
			if json.NewDecoder(r.Body).Decode(&email) != nil || email != "driver@example.com" {
				t.Error("removal must send JSON string")
			}
		} else {
			var d Driver
			json.NewDecoder(r.Body).Decode(&d)
			if d.CompanyName != "Company" || d.Phone != "1234567890" {
				t.Error("add payload")
			}
		}
		w.WriteHeader(status)
		w.Write([]byte("do not expose private-token or contact@example.com"))
	}))
	defer srv.Close()
	c := NewClient(Options{ClientID: "id", ClientSecret: "secret~+&", TokenURL: srv.URL + "/token", Scope: "scope", APIURL: srv.URL, CompanyName: "Company"})
	d := Driver{FirstName: "Test", LastName: "Driver", Email: "driver@example.com", Phone: "1234567890"}
	for _, add := range []bool{true, false} {
		if err := c.Change(context.Background(), add, d); err != nil {
			t.Fatal(err)
		}
	}
	if tokens != 1 || calls != 2 {
		t.Fatal("token should be reused")
	}
	for _, s := range []int{400, 500, 401, 429, 302} {
		status = s
		before := calls
		err := c.Change(context.Background(), false, d)
		var e *Error
		if !errors.As(err, &e) || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "contact@example.com") || calls != before+1 {
			t.Fatalf("unsafe failure handling: %v", err)
		}
		if e.Uncertain != (s == 400 || s == 500 || s == 302) {
			t.Fatalf("wrong certainty for %d", s)
		}
	}
}

func TestOptions(t *testing.T) {
	if (Options{}).Validate() != nil {
		t.Fatal("disabled must be allowed")
	}
	if (Options{ClientID: "partial"}).Validate() == nil {
		t.Fatal("partial credentials allowed")
	}
}
