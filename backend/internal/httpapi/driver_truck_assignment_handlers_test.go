package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func TestDriverTruckAssignmentAccess(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions []string
		method      string
		csrf        bool
		want        int
	}{
		{"board edit cannot assign", []string{"driver_board.read", "driver_board.write", "trucks.read"}, "PUT", true, 403},
		{"board read cannot fetch choices", []string{"driver_board.read", "trucks.read"}, "GET", false, 403},
		{"truck read required for choices", []string{"drivers.read", "drivers.edit"}, "GET", false, 403},
		{"truck read required for save", []string{"drivers.read", "drivers.edit"}, "PUT", true, 403},
		{"csrf required", []string{"drivers.read", "drivers.edit", "trucks.read"}, "PUT", false, 403},
		{"authorized invalid input", []string{"drivers.read", "drivers.edit", "trucks.read"}, "PUT", true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeAuthStore{sessions: map[string]repository.AuthSession{hashToken("session"): {User: repository.AuthUser{ID: "editor", Permissions: tc.permissions}, CSRFToken: "csrf"}}}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			auth := newAuthHandler(logger, store, AuthOptions{})
			router := chi.NewRouter()
			router.Use(auth.requireSession, auth.requireCSRF, requirePermission)
			handler := fleetHandler{logger: logger}
			router.Get("/drivers/{id}/truck-assignment", handler.driverTruckAssignment)
			router.Put("/drivers/{id}/truck-assignment", handler.driverTruckAssignment)
			req := httptest.NewRequest(tc.method, "/drivers/e4540000-0000-0000-0000-000000000001/truck-assignment", strings.NewReader(`{}`))
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
			if tc.csrf {
				req.Header.Set("X-CSRF-Token", "csrf")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
			if tc.method == "GET" {
				router.Get("/drivers/truck-assignment-choices", handler.truckAssignmentChoices)
				req = httptest.NewRequest("GET", "/drivers/truck-assignment-choices", nil)
				req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
				response = httptest.NewRecorder()
				router.ServeHTTP(response, req)
				if response.Code != tc.want {
					t.Fatalf("choices got %d, want %d: %s", response.Code, tc.want, response.Body.String())
				}
			}
		})
	}
}
