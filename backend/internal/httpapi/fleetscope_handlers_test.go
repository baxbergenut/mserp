package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/fleetscope"
	"mserp/internal/repository"
)

type fakeFleetScopeStore struct {
	called bool
	err    error
}

func (s *fakeFleetScopeStore) AcceptFleetScopeTermination(_ context.Context, _ fleetscope.Event, _ string) (repository.IntakeResult, error) {
	s.called = true
	return repository.IntakeResult{Status: "accepted", TerminationID: "00000000-0000-0000-0000-000000000004"}, s.err
}

func (s *fakeFleetScopeStore) AcceptFleetScopeHire(_ context.Context, _ fleetscope.Event, _ string) (repository.IntakeResult, error) {
	s.called = true
	return repository.IntakeResult{Status: "accepted", IntakeID: "00000000-0000-0000-0000-000000000004"}, s.err
}

func TestFleetScopeWebhookBoundary(t *testing.T) {
	options := fleetscope.Options{CompanyID: "00000000-0000-0000-0000-000000000001", Secret: strings.Repeat("s", 32)}
	valid := `{"version":1,"eventId":"00000000-0000-0000-0000-000000000002","companyId":"00000000-0000-0000-0000-000000000001","type":"driver.hired","occurredAt":"2026-09-28T12:00:00Z","driver":{"id":"00000000-0000-0000-0000-000000000003","fullName":"Test Driver","driverType":"company","hireDate":"2026-09-28"}}`
	for _, tc := range []struct {
		name, body                string
		unsigned, disabled, stale bool
		err                       error
		status                    int
		called                    bool
	}{
		{name: "valid without browser session", body: valid, status: 200, called: true},
		{name: "wrong company", body: strings.Replace(valid, options.CompanyID, "00000000-0000-0000-0000-000000000099", 1), status: 403},
		{name: "unsigned", body: valid, unsigned: true, status: 401},
		{name: "disabled", body: valid, disabled: true, status: 404},
		{name: "stale", body: valid, stale: true, status: 401},
		{name: "oversized", body: strings.Repeat("x", fleetscope.MaxBodyBytes+1), status: 413},
		{name: "unknown field", body: strings.Replace(valid, `"fullName"`, `"ssn"`, 1), status: 400},
		{name: "trailing JSON", body: valid + `{}`, status: 400},
		{name: "event collision", body: valid, err: repository.ErrFleetScopeEventConflict, status: 409, called: true},
		{name: "database unavailable", body: valid, err: errors.New("private DB details"), status: 503, called: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeFleetScopeStore{err: tc.err}
			settings := options
			if tc.disabled {
				settings = fleetscope.Options{}
			}
			req := httptest.NewRequest(http.MethodPost, "/integrations/fleetscope/driver-hired", strings.NewReader(tc.body))
			now := time.Now()
			if tc.stale {
				now = now.Add(-10 * time.Minute)
			}
			stamp := strconv.FormatInt(now.Unix(), 10)
			if !tc.unsigned {
				req.Header.Set("X-FleetScope-Timestamp", stamp)
				req.Header.Set("X-FleetScope-Signature", fleetscope.Sign(options.Secret, stamp, []byte(tc.body)))
			}
			w := httptest.NewRecorder()
			fleetScopeWebhook(slog.New(slog.NewTextHandler(io.Discard, nil)), store, settings)(w, req)
			if w.Code != tc.status || store.called != tc.called {
				t.Fatalf("status=%d called=%v body=%s", w.Code, store.called, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private DB details") {
				t.Fatal("database details exposed")
			}
		})
	}
}

func TestIntakeRoutesRequireSessionAndCSRF(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := &fakeAuthStore{sessions: map[string]repository.AuthSession{}}
	auth := newAuthHandler(logger, store, AuthOptions{SessionTTL: time.Hour})
	router := chi.NewRouter()
	router.Use(auth.requireSession)
	router.Use(auth.requireCSRF)
	registerDriverIntakeRoutes(router, logger, nil)
	for _, path := range []string{"/driver-intake", "/driver-intake/00000000-0000-0000-0000-000000000001/complete"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "complete") {
			method = http.MethodPost
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated route returned %d", w.Code)
		}
	}
	store.sessions[hashToken("test-session")] = repository.AuthSession{
		User: repository.AuthUser{ID: "00000000-0000-0000-0000-000000000001"}, CSRFToken: "test-csrf", ExpiresAt: time.Now().Add(time.Hour),
	}
	for _, tc := range []struct {
		name, body, csrf string
		status           int
	}{
		{"missing csrf", `{}`, "", 403},
		{"wrong csrf", `{}`, "wrong", 403},
		{"missing choice", `{}`, "test-csrf", 400},
		{"invalid link", `{"linkDriverId":"bad"}`, "test-csrf", 400},
		{"zero pay", `{"driver":{"fullName":"Test","payType":"cpm","payRate":0}}`, "test-csrf", 400},
		{"both choices", `{"driver":{},"linkDriverId":"00000000-0000-0000-0000-000000000001"}`, "test-csrf", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/driver-intake/00000000-0000-0000-0000-000000000001/complete", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
			req.Header.Set("X-CSRF-Token", tc.csrf)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestTerminationWebhookBoundary(t *testing.T) {
	options := fleetscope.Options{CompanyID: "00000000-0000-0000-0000-000000000001", Secret: strings.Repeat("s", 32)}
	valid := `{"version":1,"eventId":"00000000-0000-0000-0000-000000000002","companyId":"00000000-0000-0000-0000-000000000001","type":"driver.terminated","occurredAt":"2026-01-01T12:00:00Z","terminationDate":"2026-01-01","driver":{"id":"00000000-0000-0000-0000-000000000003","fullName":"Test Driver"}}`
	for _, tc := range []struct {
		name, body, allowedType   string
		unsigned, stale, disabled bool
		failure                   error
		status                    int
	}{
		{name: "accepted", body: valid, status: 200},
		{name: "unsigned", body: valid, unsigned: true, status: 401},
		{name: "stale", body: valid, stale: true, status: 401},
		{name: "disabled", body: valid, disabled: true, status: 404},
		{name: "wrong company", body: strings.Replace(valid, options.CompanyID, "00000000-0000-0000-0000-000000000099", 1), status: 403},
		{name: "wrong endpoint", body: valid, allowedType: "driver.hired", status: 400},
		{name: "unknown field", body: strings.Replace(valid, `"fullName"`, `"reason"`, 1), status: 400},
		{name: "missing date", body: strings.Replace(valid, `"terminationDate":"2026-01-01",`, "", 1), status: 400},
		{name: "collision", body: valid, failure: repository.ErrFleetScopeEventConflict, status: 409},
		{name: "unavailable", body: valid, failure: errors.New("private details"), status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeFleetScopeStore{err: tc.failure}
			req := httptest.NewRequest(http.MethodPost, "/integrations/fleetscope/driver-terminated", strings.NewReader(tc.body))
			now := time.Now()
			if tc.stale {
				now = now.Add(-10 * time.Minute)
			}
			stamp := strconv.FormatInt(now.Unix(), 10)
			if !tc.unsigned {
				req.Header.Set("X-FleetScope-Timestamp", stamp)
				req.Header.Set("X-FleetScope-Signature", fleetscope.Sign(options.Secret, stamp, []byte(tc.body)))
			}
			settings := options
			if tc.disabled {
				settings = fleetscope.Options{}
			}
			allowed := tc.allowedType
			if allowed == "" {
				allowed = "driver.terminated"
			}
			w := httptest.NewRecorder()
			fleetScopeWebhook(slog.New(slog.NewTextHandler(io.Discard, nil)), store, settings, allowed)(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.status == 200 && !strings.Contains(w.Body.String(), `"terminationId"`) {
				t.Fatal("wrong receipt shape")
			}
			if tc.status < 500 && tc.status != 200 && tc.status != 409 && store.called {
				t.Fatal("invalid request reached repository")
			}
		})
	}
}
