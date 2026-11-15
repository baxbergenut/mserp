package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSensitiveInvestorProjection(t *testing.T) {
	payload := map[string]any{
		"truck":        map[string]any{"ownerName": "Private Investor", "ownerId": "owner-id", "driverName": "Operating Driver"},
		"owners":       []any{map[string]any{"id": "owner-id", "fullName": "Private Investor", "driverId": "driver-id", "email": "secret@example.test"}},
		"investorName": "Private Investor",
		"escrow":       map[string]any{"ownerId": "owner-id", "heldAmount": "100.00", "driverName": "Private Investor - Truck 123"},
		"installment":  map[string]any{"ownerId": "owner-id", "installmentCount": 3, "driverName": "Private Investor - Truck 123"},
		"ownership":    map[string]any{"mserpOwnerId": "owner-id", "displayName": "Private Investor"},
		"payRate":      "0.75", "netPay": "123456789.12",
	}
	payload["task"] = map[string]any{"systemTaskKind": "driver_offboarding", "notes": "Owner Private Investor is terminated. Truck 123 was left unchanged.\nOwner: /drivers/detail?id=owner-driver"}
	payload["note"] = map[string]any{"body": "Truck terminated after owner Private Investor was terminated. Owner link retained."}
	payload["review"] = map[string]any{"activeDrivers": []string{"Operating Driver"}, "terminationId": "termination", "driverName": "Private Investor"}

	for _, path := range []string{"/trucks", "/lookups/trucks", "/expenses/form-choices", "/expenses/id", "/maintenance", "/truck-charges", "/driver-charges", "/escrows", "/weighmytruck/transactions", "/fleetscope-sync", "/driver-pay", "/driver-pay/statements/id", "/drivers/id/pay-history"} {
		for _, allowed := range []bool{false, true} {
			grants := []string{"trucks.read"}
			if allowed {
				grants = append(grants, "investors.read")
			}
			w := &permissionResponseWriter{ResponseWriter: httptest.NewRecorder(), request: capabilityRequest("GET", path, "", grants...)}
			raw, err := json.Marshal(w.ProjectJSON(payload))
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			if strings.Contains(text, "Private Investor") != allowed {
				t.Errorf("%s allowed=%v: %s", path, allowed, text)
			}
			if !allowed && strings.Contains(text, "secret@example.test") {
				t.Errorf("contact leaked: %s", text)
			}
			for _, retained := range []string{"Operating Driver", "owner-id", "123456789.12"} {
				if !strings.Contains(text, retained) {
					t.Errorf("%s lost %s", path, retained)
				}
			}
		}
	}
	original, _ := json.Marshal(payload)
	if !strings.Contains(string(original), "Private Investor") {
		t.Fatal("modified original cached response")
	}
}

func TestInvestorLookupProjectionAndCompany(t *testing.T) {
	w := &permissionResponseWriter{ResponseWriter: httptest.NewRecorder(), request: capabilityRequest("GET", "/lookups/investors", "", "trucks.read")}
	result := w.ProjectJSON([]any{map[string]any{"id": "owner", "fullName": "Private Investor", "isCompany": false}, map[string]any{"id": "company", "fullName": "Company", "isCompany": true}}).([]any)
	if result[0].(map[string]any)["fullName"] != "Investor" || result[1].(map[string]any)["fullName"] != "Company" {
		t.Fatalf("wrong labels: %v", result)
	}
}

func TestInvestorRoutesRequireSensitiveAccess(t *testing.T) {
	for _, policy := range routeAccessPolicies {
		if !strings.HasPrefix(policy.path, "/investor-pay") && !strings.HasPrefix(policy.path, "/investors") && policy.path != "/truck-charges/terms" {
			continue
		}
		grants := []string{"investor_pay.read", "investor_pay.edit", "investor_pay.finalize", "investor_pay.reopen", "investor_terms.read", "investor_terms.edit", "investors.create", "investors.edit"}
		w := httptest.NewRecorder()
		if requireCapabilities(w, capabilityRequest(policy.method, policy.path, "", grants...), policy.permission) || w.Code != 403 {
			t.Errorf("%s %s bypassed investor access", policy.method, policy.path)
		}
	}
}
