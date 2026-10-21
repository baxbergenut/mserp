package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
	"testing"
)

type dataTruckFake struct {
	calls                int
	uncertain, duplicate bool
	payload              map[string]any
	kind                 string
}

func (f *dataTruckFake) Configured() bool    { return true }
func (f *dataTruckFake) Environment() string { return "test-tenant" }
func (f *dataTruckFake) FleetList(context.Context, string, []map[string]string) ([]map[string]any, error) {
	if f.duplicate {
		return []map[string]any{{"id": 123}}, nil
	}
	return []map[string]any{}, nil
}
func (f *dataTruckFake) CreateFleetRecord(_ context.Context, kind string, payload json.RawMessage) (json.RawMessage, error) {
	f.calls++
	f.kind = kind
	_ = json.Unmarshal(payload, &f.payload)
	if f.uncertain {
		return nil, &datatruck.FleetError{Uncertain: true}
	}
	return json.RawMessage(`{"id":123,"detail":"success"}`), nil
}
func testDataTruckEnrollment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	fake := &dataTruckFake{}
	dt := NewDataTruckRepository(pool, fake, 17)
	fleet := NewFleetRepository(pool)
	first, last, phone := "DataTruck", "Enrollment", "5559870011"
	driver, err := fleet.CreateDriver(WithDriverServiceIntents(ctx, []DriverServiceIntent{{Service: "datatruck", Action: "add"}}, actor), DriverInput{FirstName: &first, LastName: &last, FullName: first + " " + last, Phone: &phone, PayType: "cpm", Active: true, Status: "active", EscrowAmount: "0", NewTruck: &TruckInput{UnitNumber: "DT-ATOMIC", Status: "available", Active: true, AddToDataTruck: true}})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 {
		t.Fatal("provider called before local commit")
	}
	var queued int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM datatruck_enrollments WHERE entity_id=$1 AND state='queued'`, driver.TruckID).Scan(&queued); err != nil || queued != 1 {
		t.Fatal("atomic truck request missing", err)
	}
	if err = dt.Enroll(ctx, "drivers", driver.ID); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || fake.payload["mc_number"] != float64(17) || fake.payload["first_name"] != first {
		t.Fatal("wrong driver payload", fake.payload)
	}
	if err = dt.Enroll(ctx, "drivers", driver.ID); err == nil || fake.calls != 1 {
		t.Fatal("completed create replayed")
	}
	vin, make, model, plate, state := "1FUJGLDR9CLBP1234", "Freightliner", "Cascadia", "DT123", "KY"
	year := 2022
	truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "DT-NEW", VIN: &vin, Make: &make, Model: &model, Year: &year, LicensePlate: &plate, LicenseState: &state, Status: "available", Active: true, AddToDataTruck: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = dt.Enroll(ctx, "trucks", truck.ID); err != nil {
		t.Fatal(err)
	}
	if fake.payload["ownership"] != "company" || fake.payload["company_mc"] != float64(17) || fake.payload["vin"] != vin {
		t.Fatal("wrong truck payload", fake.payload)
	}
	vin = "1FUJGLDR9CLBP1235"
	fake.uncertain = true
	truck, err = fleet.CreateTruck(ctx, TruckInput{UnitNumber: "DT-UNCERTAIN", VIN: &vin, Make: &make, Model: &model, Year: &year, LicensePlate: &plate, LicenseState: &state, Status: "available", Active: true, AddToDataTruck: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = dt.Enroll(ctx, "trucks", truck.ID); err == nil {
		t.Fatal("uncertain accepted")
	}
	snapshot, err := dt.Status(ctx, "trucks", truck.ID)
	if err != nil || snapshot.State != "unconfirmed" {
		t.Fatal(snapshot, err)
	}
	before := fake.calls
	if err = dt.RetryTruck(ctx, truck.ID, snapshot.Version); err == nil {
		t.Fatal("uncertain requeued")
	}
	_ = dt.Enroll(ctx, "trucks", truck.ID)
	if fake.calls != before {
		t.Fatal("uncertain POST repeated")
	}
	fake.uncertain = false
	vin = "1FUJGLDR9CLBP1236"
	fake.duplicate = true
	truck, err = fleet.CreateTruck(ctx, TruckInput{UnitNumber: "DT-DUPLICATE", VIN: &vin, Make: &make, Model: &model, Year: &year, LicensePlate: &plate, LicenseState: &state, Status: "available", Active: true, AddToDataTruck: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = dt.Enroll(ctx, "trucks", truck.ID); err == nil || fake.calls != before {
		t.Fatal("duplicate submitted")
	}
	// Missing provider prerequisites never undo the native record or send a POST.
	if err = dt.Enroll(ctx, "trucks", *driver.TruckID); err == nil || fake.calls != before {
		t.Fatal("incomplete truck submitted")
	}
	var audit int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM datatruck_enrollment_events WHERE entity_id=$1`, truck.ID).Scan(&audit); err != nil || audit < 3 {
		t.Fatal("receipt history missing", audit, err)
	}
}
