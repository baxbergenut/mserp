package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func TestServiceProgramManagementPermissions(t *testing.T) {
	router := chi.NewRouter()
	router.Use(requirePermission)
	h := maintenanceHandler{}
	router.Post("/maintenance/programs", h.programSave)
	router.Put("/maintenance/programs/{id}", h.programSave)
	router.Get("/maintenance/program-assignees", h.programAssignees)
	for _, tc := range []struct{ method, path string }{{"POST", "/maintenance/programs"}, {"PUT", "/maintenance/programs/00000000-0000-4000-8000-000000000020"}, {"GET", "/maintenance/program-assignees"}} {
		if got := routePermission(tc.method, tc.path); got != "service_programs.manage" {
			t.Fatalf("%s %s uses %s", tc.method, tc.path, got)
		}
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		r = r.WithContext(context.WithValue(r.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: []string{"trucks.edit"}}}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("request without management permission reached storage: %d", w.Code)
		}
	}
	if !repository.ValidPermissions([]string{"service_programs.manage"}) {
		t.Fatal("permission cannot be configured in Settings")
	}
}
