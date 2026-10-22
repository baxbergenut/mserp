package repository

import (
	"encoding/json"
	"testing"
)

func TestDataTruckUpdateAllowlist(t *testing.T) {
	source := map[string]any{"first_name": "Local", "last_name": "Driver", "email": "local@example.test", "contact_number": "+15551234567", "brith_date": "1990-01-01", "driver_license": "NEW"}
	baseline := map[string]any{}
	for k, v := range source {
		baseline[k] = v
	}
	baseline["driver_license"] = "OLD"
	remote := map[string]any{"account": map[string]any{"first_name": "PROVIDER", "last_name": "NAME", "email": "provider@example.test"}, "contact_number": "+15551234567", "driver_type": "company_driver", "employee_status": "active", "status": "in_transit", "assigned_truck": map[string]any{"id": 8}, "assigned_dispatcher": map[string]any{"id": 9}}
	p, err := dataTruckUpdatePayload(source, baseline, remote, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	if p["assigned_truck"] != nil || p["assigned_dispatcher"] != nil || p["status"] != "in_transit" || p["driver_license"] != "NEW" || p["driver_license_state"] != nil {
		t.Fatal("wrong update", string(raw))
	}
	if p["account"].(map[string]any)["first_name"] != "PROVIDER" {
		t.Fatal("unchanged local name overwrote provider name")
	}
	delete(source, "brith_date")
	if _, err = dataTruckUpdatePayload(source, baseline, remote, 1); err == nil {
		t.Fatal("missing DOB accepted")
	}
	source["brith_date"] = "1990-01-01"
	remote["team_driver"] = map[string]any{"id": 123}
	if _, err = dataTruckUpdatePayload(source, baseline, remote, 1); err == nil {
		t.Fatal("unknown provider main-driver role guessed")
	}
}
