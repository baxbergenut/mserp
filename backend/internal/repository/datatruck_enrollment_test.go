package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
	"testing"
)

type dataTruckFake struct {
	updates              int
	remote               map[string]any
	calls                int
	uncertain, duplicate bool
	payload              map[string]any
	kind                 string
}

func (f *dataTruckFake) Configured() bool    { return true }
func (f *dataTruckFake) Environment() string { return "test-tenant" }
func (f *dataTruckFake) UpdateDriver(_ context.Context, _ int64, body json.RawMessage) (json.RawMessage, error) {
	f.updates++
	if f.uncertain {
		return nil, &datatruck.FleetError{Uncertain: true}
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	if f.remote != nil {
		f.remote["account"] = payload["account"]
		f.remote["contact_number"] = payload["contact_number"]
	}
	return json.RawMessage(`{"detail":"success"}`), nil
}
func (f *dataTruckFake) FleetList(context.Context, string, []map[string]string) ([]map[string]any, error) {
	if f.remote != nil {
		return []map[string]any{f.remote}, nil
	}
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
	identity, err := dt.Status(ctx, "drivers", driver.ID)
	if err != nil || identity.ProviderID == nil || *identity.ProviderID != 123 {
		t.Fatal("successful provider ID was not retained", identity, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO datatruck_enrollments(entity_kind,entity_id,state,environment,provider_id) VALUES('drivers',gen_random_uuid(),'done','test-tenant',123)`); err == nil {
		t.Fatal("duplicate provider identity accepted")
	}
	if _, err = pool.Exec(ctx, `INSERT INTO datatruck_enrollments(entity_kind,entity_id,state,environment,provider_id) VALUES('drivers',gen_random_uuid(),'done','other-tenant',123)`); err != nil {
		t.Fatal("different tenant identity rejected", err)
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
	testDataTruckDriverUpdates(t, ctx, pool)
}

func testDataTruckDriverUpdates(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	var driver string
	err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,first_name,last_name,email,phone,pay_type,pay_rate,date_of_birth) VALUES('Update Driver','update driver','Update','Driver','update@example.test','5551112222','cpm',0,'1990-01-01') RETURNING id::text`).Scan(&driver)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO datatruck_enrollments(entity_kind,entity_id,state,environment,provider_id,provider_carrier_id,profile_baseline) SELECT 'drivers',id,'done','test-tenant',999,17,datatruck_driver_profile(d) FROM drivers d WHERE id=$1`, driver)
	if err != nil {
		t.Fatal(err)
	}
	fake := &dataTruckFake{remote: map[string]any{"id": 999, "account": map[string]any{"first_name": "UPDATE", "last_name": "DRIVER", "email": "update@example.test"}, "contact_number": "+15551112222", "driver_type": "company_driver", "employee_status": "active", "status": "in_transit"}}
	repo := NewDataTruckRepository(pool, fake, 17)
	_, err = pool.Exec(ctx, `UPDATE drivers SET phone='5551113333' WHERE id=$1`, driver)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ProcessDriverUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT state,request_payload FROM datatruck_driver_updates WHERE driver_id=$1 ORDER BY id DESC LIMIT 1`, driver).Scan(&state, &payload); err != nil || state != "accepted" || fake.updates != 1 {
		t.Fatal("update not delivered", state, err)
	}
	var sent map[string]any
	_ = json.Unmarshal(payload, &sent)
	if sent["status"] != "in_transit" || sent["assigned_truck"] != nil || sent["driver_license"] != nil || sent["account"].(map[string]any)["first_name"] != "UPDATE" {
		t.Fatal("unrelated provider fields changed", sent)
	}
	// An uncertain PUT blocks all later edits and never gets replayed.
	fake.uncertain = true
	_, err = pool.Exec(ctx, `UPDATE drivers SET phone='5551114444' WHERE id=$1`, driver)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ProcessDriverUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE drivers SET phone='5551115555' WHERE id=$1`, driver)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ProcessDriverUpdates(ctx); err != nil || fake.updates != 2 {
		t.Fatal("uncertain update repeated", fake.updates, err)
	}
	// Source imports and rolled-back edits must never issue future updates.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `SELECT set_config('mserp.datatruck_import','on',true); UPDATE drivers SET date_of_birth='1991-01-01' WHERE id='`+driver+`'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM datatruck_driver_updates WHERE driver_id=$1`, driver).Scan(&count); err != nil || count != 3 {
		t.Fatal("import queued a profile update", count, err)
	}
	// Success-only creation receipts discover the new identity without another
	// POST. Missing DOB holds delivery; recording it supersedes the rejected
	// draft and sends every outstanding profile change against the old baseline.
	var pending string
	err = pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,first_name,last_name,email,phone,pay_type,pay_rate) VALUES('Pending Identity','pending identity','Pending','Identity','pending@example.test','5552223333','cpm',0) RETURNING id::text`).Scan(&pending)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO datatruck_enrollments(entity_kind,entity_id,state,environment,provider_carrier_id,payload,receipt,sent_at,updated_at,profile_baseline) SELECT 'drivers',id,'done','test-tenant',17,datatruck_driver_profile(d),'{"detail":"success"}',now(),now()-interval '2 minutes',datatruck_driver_profile(d) FROM drivers d WHERE id=$1`, pending)
	if err != nil {
		t.Fatal(err)
	}
	fake.remote = map[string]any{"id": 1000, "account": map[string]any{"first_name": "Pending", "last_name": "Identity", "email": "pending@example.test"}, "contact_number": "+15552223333", "driver_type": "company_driver", "employee_status": "active", "status": "available"}
	if err = repo.ProcessDriverUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	identity, err := repo.Status(ctx, "drivers", pending)
	if err != nil || identity.ProviderID == nil || *identity.ProviderID != 1000 || fake.calls != 0 {
		t.Fatal("success-only receipt did not resolve safely", identity, err)
	}
	_, err = pool.Exec(ctx, `UPDATE drivers SET phone='5552224444' WHERE id=$1`, pending)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ProcessDriverUpdates(ctx); err != nil || fake.updates != 2 {
		t.Fatal("missing DOB sent a PUT", err)
	}
	fake.uncertain = false
	_, err = pool.Exec(ctx, `UPDATE drivers SET date_of_birth='1990-01-01' WHERE id=$1`, pending)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ProcessDriverUpdates(ctx); err != nil || fake.updates != 3 {
		t.Fatal("DOB completion did not resume outstanding changes", fake.updates, err)
	}
	identity, err = repo.Status(ctx, "drivers", pending)
	if err != nil || identity.SyncState != "accepted" || fake.remote["contact_number"] != "+15552224444" {
		t.Fatal("outstanding profile change was lost", identity, err)
	}
}
