package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"mserp/internal/repository"
)

func TestTruckStatusAccessWithoutEditPermission(t *testing.T) {
	// Exercise the production middleware chain with a user who has no grants.
	store := &fakeAuthStore{sessions: map[string]repository.AuthSession{
		hashToken("session"): {User: repository.AuthUser{ID: "viewer"}, CSRFToken: "csrf"},
	}}
	auth := newAuthHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), store, AuthOptions{})
	router := chi.NewRouter()
	router.Use(auth.requireSession, auth.requireCSRF, requirePermission)
	reached := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	router.Patch("/trucks/{id}/status", reached)
	router.Patch("/trucks/{id}/condition", reached)
	router.Put("/trucks/{id}", reached)
	router.Delete("/trucks/{id}", reached)
	router.Patch("/drivers/{id}/availability", reached)
	for _, tc := range []struct {
		method, path   string
		signedIn, csrf bool
		want           int
	}{
		{"PATCH", "/trucks/id/status", true, true, 204},
		{"PATCH", "/trucks/id/condition", true, true, 204},
		{"PATCH", "/trucks/id/status", false, true, 401},
		{"PATCH", "/trucks/id/condition", false, true, 401},
		{"PATCH", "/trucks/id/status", true, false, 403},
		{"PATCH", "/trucks/id/condition", true, false, 403},
		{"PUT", "/trucks/id", true, true, 403},
		{"DELETE", "/trucks/id", true, true, 403},
		{"PATCH", "/drivers/id/availability", true, true, 403},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.signedIn {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
		}
		if tc.csrf {
			req.Header.Set("X-CSRF-Token", "csrf")
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != tc.want {
			t.Errorf("%s %s signedIn=%v csrf=%v: got %d, want %d: %s", tc.method, tc.path, tc.signedIn, tc.csrf, res.Code, tc.want, res.Body.String())
		}
	}
}
