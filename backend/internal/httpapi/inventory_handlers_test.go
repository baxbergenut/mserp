package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func TestInventoryRoutePermissions(t *testing.T) {
	tests := []struct{ method, path, permission string }{
		{"GET", "/inventory", "inventory.read"}, {"GET", "/inventory/history", "inventory.read"},
		{"PUT", "/inventory/assets/019a0000-0000-4000-8000-000000000001", "inventory.manage"},
		{"POST", "/inventory/movements", "inventory.manage"},
		{"GET", "/inventory/imports", "inventory.import"}, {"POST", "/inventory/imports/preview", "inventory.import"},
		{"PUT", "/inventory/imports/019a0000-0000-4000-8000-000000000001", "inventory.import"},
		{"POST", "/inventory/imports/019a0000-0000-4000-8000-000000000001/commit", "inventory.import"},
		{"GET", "/trailers", "trailers.read"}, {"PUT", "/trailers/019a0000-0000-4000-8000-000000000001", "trailers.manage"},
		{"GET", "/trailers/019a0000-0000-4000-8000-000000000001", "trailers.read"},
		{"GET", "/trailers/019a0000-0000-4000-8000-000000000001/history", "trailers.read"},
	}
	for _, test := range tests {
		if got := routePermission(test.method, test.path); got != test.permission {
			t.Fatalf("%s %s got %s", test.method, test.path, got)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := chi.NewRouter()
	router.Use(requirePermission)
	registerInventoryRoutes(router, logger, nil)
	// A nil repository intentionally makes any accidental authorization bypass
	// fail loudly: denied requests must not reach storage or vendor code.
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{}`))
		request = request.WithContext(context.WithValue(request.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: []string{"trucks.read"}}}))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s leaked access: %d", test.method, test.path, response.Code)
		}
	}
	for _, path := range []string{"/inventory/imports", "/inventory/imports/preview"} {
		method := "GET"
		if strings.HasSuffix(path, "preview") {
			method = "POST"
		}
		request := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		request = request.WithContext(context.WithValue(request.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: repository.PermissionKeys()}}))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatal("nonadministrator import allowed", response.Code)
		}
	}
	request := httptest.NewRequest("PUT", "/inventory/trailers/019a0000-0000-4000-8000-000000000001", strings.NewReader(`{}`))
	request = request.WithContext(context.WithValue(request.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: []string{"inventory.read", "inventory.manage"}}}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("inventory permission bypassed trailer authorization")
	}
}
func TestInventoryImportBodyLimit(t *testing.T) {
	request := httptest.NewRequest("POST", "/inventory/imports/preview", strings.NewReader(`{"spreadsheetId":"source","capturedAt":"2026-10-19T12:00:00Z","sheets":[{"name":"Archive","rows":[["`+strings.Repeat("x", 1100000)+`"]]}]}`))
	var value repository.InventorySnapshot
	if err := decodeInventoryImport(httptest.NewRecorder(), request, &value); err != nil {
		t.Fatal("valid source above generic 1 MB request limit was rejected", err)
	}
	request = httptest.NewRequest("POST", "/inventory/imports/preview", strings.NewReader(`{"unexpected":true}`))
	if decodeInventoryImport(httptest.NewRecorder(), request, &value) == nil {
		t.Fatal("unknown snapshot fields accepted")
	}
}
