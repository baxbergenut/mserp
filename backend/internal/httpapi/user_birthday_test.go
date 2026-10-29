package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type birthdayAuthStore struct {
	fakeAuthStore
	day    time.Time
	userID string
	calls  int
}

func (s *birthdayAuthStore) ClaimBirthday(_ context.Context, id string, day time.Time) (repository.BirthdayGreeting, error) {
	s.day, s.userID = day, id
	s.calls++
	return repository.BirthdayGreeting{Celebrate: true, Name: "Alex"}, nil
}

func TestBirthdayTashkentTimezoneAndAuthorization(t *testing.T) {
	store := &birthdayAuthStore{}
	h := newAuthHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), store, AuthOptions{})
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{ID: "self"}, CSRFToken: "csrf"})))
		})
	})
	router.Use(h.requireCSRF, requirePermission)
	router.Post("/auth/birthday", h.birthday)
	for _, tc := range []struct {
		instant, body, csrf string
		status              int
		day                 string
	}{
		{"2026-10-28T18:59:59Z", "", "csrf", 200, "2026-10-28"},
		{"2026-10-28T19:00:00Z", "", "csrf", 200, "2026-10-29"},
		{"2026-10-29T01:00:00Z", `{"timeZone":"America/New_York","userId":"other"}`, "csrf", 200, "2026-10-29"},
		{"2026-03-08T19:00:00Z", "", "csrf", 200, "2026-03-09"},
		{"2026-10-28T19:00:00Z", "", "", 403, ""},
	} {
		instant, _ := time.Parse(time.RFC3339, tc.instant)
		h.now = func() time.Time { return instant }
		before := store.calls
		req := httptest.NewRequest("POST", "/auth/birthday", strings.NewReader(tc.body))
		req.Header.Set("X-CSRF-Token", tc.csrf)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.body, res.Code, res.Body.String())
		}
		if tc.status == 200 {
			if store.userID != "self" || store.day.Format("2006-01-02") != tc.day {
				t.Fatalf("wrong user/date: %s %s", store.userID, store.day)
			}
			if strings.Contains(res.Body.String(), "dateOfBirth") || strings.Contains(res.Body.String(), "phone") {
				t.Fatal("private profile leaked")
			}
		} else if store.calls != before {
			t.Fatal("invalid request claimed greeting")
		}
	}
}

func TestNormalizeUserProfile(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	ptr := func(s string) *string { return &s }
	for _, tc := range []struct {
		input repository.ManagedUser
		valid bool
	}{
		{repository.ManagedUser{FirstName: " Alex ", LastName: " Smith ", DateOfBirth: ptr("2000-02-29"), Phone: ptr(" +1 (555) 123-4567 ")}, true},
		{repository.ManagedUser{ID: "legacy", Username: "Legacy User"}, true},
		{repository.ManagedUser{}, false},
		{repository.ManagedUser{FirstName: "Alex"}, false},
		{repository.ManagedUser{FirstName: strings.Repeat("é", 51), LastName: "Smith"}, false},
		{repository.ManagedUser{ID: "legacy", DateOfBirth: ptr("2001-02-29")}, false},
		{repository.ManagedUser{ID: "legacy", DateOfBirth: ptr("1899-01-01")}, false},
		{repository.ManagedUser{ID: "legacy", DateOfBirth: ptr("2027-01-01")}, false},
		{repository.ManagedUser{ID: "legacy", DateOfBirth: ptr("")}, true},
		{repository.ManagedUser{ID: "legacy", Phone: ptr("555\n123")}, false},
	} {
		err := normalizeUserProfile(&tc.input, now)
		if (err == nil) != tc.valid {
			t.Fatalf("valid=%v err=%v input=%+v", tc.valid, err, tc.input)
		}
		if tc.input.FirstName == "Alex" && tc.input.LastName == "Smith" && tc.input.Username != "Alex Smith" {
			t.Fatal("display name not composed")
		}
	}
	// Unset private fields are omitted from general-purpose response/audit DTOs.
	raw, _ := json.Marshal(repository.ManagedUser{FirstName: "Alex"})
	if strings.Contains(string(raw), "dateOfBirth") || strings.Contains(string(raw), "phone") {
		t.Fatal("private keys not omitted")
	}
}
