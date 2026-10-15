package httpapi

import (
	"context"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"mserp/internal/repository"
	"net/http/httptest"
	"testing"
)

func TestFleetScopeSyncPermissions(t *testing.T) {
	for _, c := range []struct{ method, path, want string }{
		{"GET", "/fleetscope-sync", "fleetscope.read"},
		{"POST", "/fleetscope-sync/snapshot", "fleetscope.reconcile"},
		{"POST", "/fleetscope-sync/review", "fleetscope.reconcile"},
		{"POST", "/fleetscope-sync/activate", "fleetscope.reconcile"},
		{"POST", "/fleetscope-sync/resume", "fleetscope.reconcile"},
		{"POST", "/fleetscope-sync/pause", "fleetscope.manage"},
		{"POST", "/fleetscope-sync/operations/id/retry", "fleetscope.manage"},
		{"POST", "/fleetscope-sync/operations/id/supersede", "fleetscope.reconcile"},
	} {
		if got := routePermission(c.method, c.path); got != c.want {
			t.Fatalf("%s got %s", c.path, got)
		}
	}
}

func TestFleetScopeSyncRequiresAdministrator(t *testing.T) {
	router := chi.NewRouter()
	registerFleetSyncRoutes(router, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	for _, path := range []string{"snapshot", "review", "assignments", "activate", "resume", "operations/11111111-1111-4111-8111-111111111111/supersede"} {
		request := httptest.NewRequest("POST", "/fleetscope-sync/"+path, nil)
		// Even a user granted the reconciliation permission needs built-in admin status.
		session := repository.AuthSession{}
		session.User.Permissions = []string{"fleet.read", "fleetscope.reconcile"}
		request = request.WithContext(context.WithValue(request.Context(), authContextKey{}, session))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatalf("%s returned %d", path, response.Code)
		}
	}
}
