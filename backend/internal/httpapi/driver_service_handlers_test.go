package httpapi

import (
	"context"
	"mserp/internal/repository"
	"net/http/httptest"
	"testing"
)

func TestDriverServiceIntentPermissions(t *testing.T) {
	for _, tc := range []struct {
		service     string
		permissions []string
		allowed     bool
	}{
		{"relay", []string{"fleet.write", "fuel.read"}, false},
		{"weighmytruck", []string{"fleet.write", "weighmytruck.read"}, false},
		{"relay", []string{"fuel.drivers.create"}, true},
		{"weighmytruck", []string{"weighmytruck.create"}, true},
		{"relay", []string{"weighmytruck.create"}, false},
	} {
		for _, action := range []string{"add", "defer"} {
			req := httptest.NewRequest("POST", "/drivers", nil)
			req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: tc.permissions}}))
			w := httptest.NewRecorder()
			_, ok := serviceIntentContext(w, req, []repository.DriverServiceIntent{{Service: tc.service, Action: action, FollowUpOn: "2026-10-13"}})
			if ok != tc.allowed {
				t.Fatalf("%s %s allowed=%v want=%v", tc.service, action, ok, tc.allowed)
			}
			if !ok && w.Code != 403 {
				t.Fatalf("status=%d", w.Code)
			}
		}
	}
	if routePermission("GET", "/driver-services/id") != "drivers.read" || routePermission("POST", "/driver-services/id") != "drivers.read" {
		t.Fatal("driver read boundary missing")
	}
}
