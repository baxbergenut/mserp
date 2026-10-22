package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"mserp/internal/repository"
)

func capabilityRequest(method, path, body string, permissions ...string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	return r.WithContext(context.WithValue(r.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: permissions}}))
}

func TestPermissionProjection(t *testing.T) {
	for _, path := range []string{"/drivers", "/driver-directory", "/drivers/id", "/drivers/id/status"} {
		t.Run(path, func(t *testing.T) {
			w := &permissionResponseWriter{ResponseWriter: httptest.NewRecorder(), request: capabilityRequest("GET", path, "", "drivers.read")}
			payload := map[string]any{"items": []any{map[string]any{"id": "driver", "phone": "555", "payType": "cpm", "payRate": 0.75, "address": "private", "emergencyContact": "private", "licenseNumber": "secret", "cdlFileId": "secret", "cdlFileName": "secret", "dateOfBirth": "1990-01-01"}}}
			raw, _ := json.Marshal(w.ProjectJSON(payload))
			for _, forbidden := range []string{"payType", "payRate", "address", "emergencyContact", "licenseNumber", "cdlFileId", "cdlFileName", "dateOfBirth"} {
				if strings.Contains(string(raw), forbidden) {
					t.Errorf("leaked %s: %s", forbidden, raw)
				}
			}
			if !strings.Contains(string(raw), "555") {
				t.Fatal("lost operational field")
			}
		})
	}
	w := &permissionResponseWriter{ResponseWriter: httptest.NewRecorder(), request: capabilityRequest("GET", "/gross-board", "", "board.read")}
	raw, _ := json.Marshal(w.ProjectJSON(map[string]any{"originalRate": "100.25", "driverRate": "90.00"}))
	if !strings.Contains(string(raw), "100.25") || !strings.Contains(string(raw), "90.00") {
		t.Fatal("operational board amounts were removed")
	}
}

func TestProtectedProfilePresenceAndPreservation(t *testing.T) {
	for _, body := range []string{`{"payRate":0}`, `{"PayRate":0}`, `{"PAYRATE":0}`, `{"payType":"cpm"}`, `{"address":null}`, `{"cdlFileId":null}`, `{"dateOfBirth":"1990-01-01"}`, `{"DATEOFBIRTH":null}`} {
		var dst driverRequest
		w := httptest.NewRecorder()
		if decodeProtectedProfile(w, capabilityRequest("PUT", "/drivers/id", body, "drivers.edit"), &dst, repository.Driver{}, "drivers") || w.Code != 403 {
			t.Fatalf("protected payload accepted: %s", body)
		}
	}
	var dst driverRequest
	saved := repository.Driver{FullName: "A", PayType: "cpm", PayRate: 0.75}
	if !decodeProtectedProfile(httptest.NewRecorder(), capabilityRequest("PUT", "/drivers/id", `{"phone":"555"}`, "drivers.edit"), &dst, saved, "drivers") {
		t.Fatal("ordinary edit rejected")
	}
	if dst.PayRate != 0.75 || !slices.Contains(dst.preserveFields, "payRate") || !slices.Contains(dst.preserveFields, "cdlFileId") {
		t.Fatalf("missing preservation: %+v", dst)
	}
}

func TestActionBoundaries(t *testing.T) {
	for _, area := range []string{"drivers", "trucks", "dispatchers", "updaters"} {
		for _, tc := range []struct{ method, path, action string }{{"GET", "", "read"}, {"POST", "", "create"}, {"PUT", "/id", "edit"}, {"DELETE", "/id", "delete"}} {
			if got := routePermission(tc.method, "/"+area+tc.path); got != area+"."+tc.action {
				t.Errorf("%s %s = %s", area, tc.action, got)
			}
		}
	}
	if routePermission("GET", "/drivers/setup-defaults") != "escrow.read" {
		t.Fatal("wildcard overrode specific policy")
	}
	if routePermission("POST", "/unknown-resource") != "" {
		t.Fatal("unknown routes must deny")
	}
	for _, area := range []string{"fuel.drivers", "tolls.fleet", "weighmytruck"} {
		for _, action := range []string{"add", "edit", "remove"} {
			for _, grant := range []string{"create", "edit", "delete"} {
				want := map[string]string{"add": "create", "edit": "edit", "remove": "delete"}[action] == grant
				got := authorizeVendorAction(httptest.NewRecorder(), capabilityRequest("POST", "/change", "", area+"."+grant), area, action)
				if got != want {
					t.Errorf("%s action %s grant %s: %v", area, action, grant, got)
				}
			}
		}
	}
}

// Exercise middleware for every catalog-owned policy, not just directory examples.
func TestEveryRouteRejectsMissingAction(t *testing.T) {
	for _, policy := range routeAccessPolicies {
		if policy.permission == "authenticated" {
			continue
		}
		for _, allowed := range []bool{false, true} {
			grants := []string{}
			if allowed {
				grants = append(grants, policy.permission)
			}
			called := false
			handler := requirePermission(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
			request := capabilityRequest(policy.method, policy.path, "", grants...)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if called != allowed {
				t.Errorf("%s %s allowed=%v: %d", policy.method, policy.path, allowed, recorder.Code)
			}
		}
	}
}

func TestEscrowActionsCannotSubstituteForEachOther(t *testing.T) {
	for _, tc := range []struct{ method, grant, body string }{
		{"POST", "escrow.edit", `{"amount":"1","cancelled":false}`},
		{"PUT", "escrow.create", `{"amount":"1","cancelled":false}`},
		{"PUT", "escrow.edit", `{"amount":"1","cancelled":true}`},
		{"PUT", "escrow.delete", `{"amount":"1","cancelled":false}`},
	} {
		w := httptest.NewRecorder()
		escrowHandler{}.saveRelease(w, capabilityRequest(tc.method, "/escrows/id/releases/id", tc.body, "escrow.read", tc.grant))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d", tc.method, tc.grant, w.Code)
		}
	}
}
