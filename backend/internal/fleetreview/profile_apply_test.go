package fleetreview

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestProfileBatchDatabase(t *testing.T) {
	ctx := context.Background()
	a := testPool(t, "MSERP_PORTAL_TEST_DATABASE_URL")
	b := testPool(t, "FLEETSCOPE_PORTAL_TEST_DATABASE_URL")
	company, local, remote, truck, remoteTruck := id(), id(), id(), id(), id()
	d := &DB{Source: a, Receiver: b, CompanyID: company, CompanyName: "Profile Preview", DOT: company[:8]}
	suffix := time.Now().UnixNano() % 1000000
	vin := "1FUJGLDR9CS" + fmt.Sprintf("%06d", suffix)
	phone := fmt.Sprintf("5555%06d", suffix)
	run := func(q string, args ...any) {
		t.Helper()
		if _, err := a.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	fs := func(q string, args ...any) {
		t.Helper()
		if _, err := b.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	run(ReceiptSchema)
	fs(ReceiptSchema)
	run("INSERT INTO drivers(id,full_name,normalized_name,email,phone,pay_type,pay_rate) VALUES($1,'Jane Doe '||$2,'jane doe '||$2,'old@example.test',$3,'cpm',0.5)", local, local[:8], phone)
	run("INSERT INTO trucks(id,unit_number,vin,year,make,license_plate,license_state) VALUES($1,$2,$3,2022,'Freightliner','P100','NY')", truck, truck[:8], vin)
	run("INSERT INTO datatruck_enrollments(entity_kind,entity_id,environment,state,provider_id,provider_carrier_id,profile_baseline) VALUES('drivers',$1,'production','done',$2,33,'{}')", local, time.Now().UnixNano()%1000000000+1)
	fs("INSERT INTO companies(id,name,dot_number,email,phone_number,address) VALUES($1,'Profile Preview',$2,'test@example.test','5555550100','Test')", company, d.DOT)
	fs("INSERT INTO drivers(id,company_id,first_name,last_name,email,phone_number,ssn,ssn_last4,ssn_digest,address,status) VALUES($1,$2,'Jane','Doe '||$3,'new@example.test',$4,'','','','','inactive')", remote, company, local[:8], phone)
	fs("INSERT INTO vehicles(id,company_id,unit_number,vin,year,make,model,license_plate,license_plate_state,color,truck_class,gvwr_lbs,ownership,status) VALUES($1,$2,$3,$4,2022,'Freightliner','','P100','NY','White','8',80000,'company','out_of_service')", remoteTruck, company, truck[:8], vin)
	t.Cleanup(func() {
		// This fixture uses disposable databases, including immutable receipts and managed vehicles.
		run("DELETE FROM datatruck_enrollments WHERE entity_id=$1", local)
		run("DELETE FROM trucks WHERE id=$1", truck)
		run("DELETE FROM drivers WHERE id=$1", local)
	})

	source, receiver, err := d.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := initial(source, receiver, Policy{})
	preview, err := d.profilePreview(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.Drivers != 1 || preview.Plan.Trucks != 1 {
		t.Fatalf("expected both records to be included: %+v", preview.Plan)
	}
	assert := func(q queryer, sql, want string, args ...any) {
		t.Helper()
		var got string
		if err := q.QueryRow(ctx, sql, args...).Scan(&got); err != nil || got != want {
			t.Fatalf("got %q, want %q, error %v", got, want, err)
		}
	}
	assert(a, "SELECT email FROM drivers WHERE id=$1", "old@example.test", local)
	assert(a, "SELECT coalesce(color,'') FROM trucks WHERE id=$1", "", truck)
	s.Profile = preview
	s.Batch = &Batch{ID: id(), Hash: batchHash(s)}
	fs("UPDATE drivers SET updated_at=clock_timestamp() WHERE id=$1", remote)
	if err = d.Apply(ctx, s, "test"); err == nil {
		t.Fatal("stale preview applied")
	}
	assert(a, "SELECT email FROM drivers WHERE id=$1", "old@example.test", local)

	s.Profile, err = d.profilePreview(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s.Batch.Hash = batchHash(s)
	if err := d.Apply(ctx, s, "test"); err != nil {
		t.Fatal(err)
	}
	assert(a, "SELECT email||':'||status||':'||pay_rate FROM drivers WHERE id=$1", "new@example.test:active:0.5000", local)
	assert(a, "SELECT color||':'||coalesce(model,'')||':'||status FROM trucks WHERE id=$1", "White::available", truck)
	assert(a, "SELECT count(*)::text FROM datatruck_driver_updates WHERE driver_id=$1", "0", local)
	assert(b, "SELECT status FROM drivers WHERE id=$1", "active", remote)
	assert(b, "SELECT status||':'||mserp_operational_status||':'||mserp_coverage_review FROM vehicles WHERE id=$1", "out_of_service:available:false", remoteTruck)
	if err := d.Apply(ctx, s, "test"); err != nil {
		t.Fatal("receipt replay failed:", err)
	}
}
