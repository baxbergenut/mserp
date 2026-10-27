package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/prepass"
	"testing"
	"time"
)

func testPrePassTruckLinks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	truck := id(`INSERT INTO trucks(unit_number,vin,license_plate) VALUES('01.','4V4NC9EG4NN297330','DF28HU') RETURNING id`)
	other := id(`INSERT INTO trucks(unit_number) VALUES('001') RETURNING id`)
	driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Toll Test','toll test','cpm',0.7) RETURNING id`)
	exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01')`, truck, driver)
	repo := NewTollRepository(pool)
	makeToll := func(n int64, unit, day, tag string) prepass.Transaction {
		return prepass.Transaction{TollID: n, AccountNumber: json.Number("1"), PostDateTime: "2026-10-05T12:00:00", ExitDateTime: day + "T12:00:00", VehicleNumber: unit, PPDeviceID: tag, DeviceNumber: "00413749983", TollCharge: json.Number("2.50")}
	}
	txns := []prepass.Transaction{makeToll(90001, "01", "2026-10-01", "777780673"), makeToll(90002, "", "2026-10-02", "777780673"), makeToll(90003, "044", "2026-07-18", "777780673"), makeToll(90004, "001", "2026-10-03", "777780673")}
	if _, err := repo.UpsertDay(ctx, "production", time.Now(), txns, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	list, err := repo.PrePassTruckLinks(ctx, "2026-09-01")
	if err != nil {
		t.Fatal(err)
	}
	var group *PrePassTruckGroup
	for i := range list.Groups {
		if list.Groups[i].IdentityKey == "tag:777780673" {
			group = &list.Groups[i]
		}
	}
	if group == nil || group.Count != 3 {
		t.Fatalf("group=%+v", group)
	}
	in := PrePassTruckLinkInput{ID: id(`SELECT gen_random_uuid()::text`), Environment: "production", IdentityKey: "tag:777780673", TruckID: truck, From: "2026-10-01"}
	preview, err := repo.PreviewPrePassTruckLink(ctx, in)
	if err != nil || preview.Count != 2 || preview.Amount != "5.00" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	in.Revision = preview.Revision
	exec(`UPDATE tolls SET amount=3 WHERE prepass_toll_id=90001`)
	if err = repo.LinkPrePassTruck(ctx, in, actor); err == nil {
		t.Fatal("accepted stale preview")
	}
	exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-05','{}')`, driver)
	preview, err = repo.PreviewPrePassTruckLink(ctx, in)
	if err != nil || !preview.Closed {
		t.Fatalf("finalized not detected: %+v %v", preview, err)
	}
	in.Revision = preview.Revision
	if err = repo.LinkPrePassTruck(ctx, in, actor); err == nil {
		t.Fatal("changed finalized driver statement")
	}
	exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver)
	if err = repo.LinkPrePassTruck(ctx, in, actor); err != nil {
		t.Fatal(err)
	}
	if err = repo.LinkPrePassTruck(ctx, in, actor); err != nil {
		t.Fatal("idempotent link", err)
	}
	if got := id(`SELECT count(*)::text FROM tolls WHERE truck_mapping_id=$1 AND truck_id=$2`, in.ID, truck); got != "2" {
		t.Fatal("wrong mapped count", got)
	}
	if got := id(`SELECT truck_id::text FROM tolls WHERE prepass_toll_id=90004`); got != other {
		t.Fatal("existing association overwritten")
	}
	if got := id(`SELECT count(*)::text FROM tolls WHERE prepass_toll_id=90003 AND truck_id IS NULL`); got != "1" {
		t.Fatal("old tag usage linked")
	}
	next := makeToll(90005, "001", "2026-10-06", "777780673") // Mapping beats unit-name guess on a new import.
	result, err := repo.UpsertDay(ctx, "production", time.Now(), []prepass.Transaction{next}, time.Now(), false)
	if err != nil || result.Unmatched != 0 {
		t.Fatalf("future import %+v %v", result, err)
	}
	if got := id(`SELECT truck_id::text FROM tolls WHERE prepass_toll_id=90005`); got != truck {
		t.Fatal("future mapping not applied")
	}
	if _, err = repo.UpsertDay(ctx, "production", time.Now(), txns, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if got := id(`SELECT truck_id::text FROM tolls WHERE prepass_toll_id=90001`); got != truck {
		t.Fatal("resync moved mapped truck")
	}
	// Other environments and dates never inherit the mapping.
	if _, err = repo.UpsertDay(ctx, "nonproduction", time.Now(), []prepass.Transaction{makeToll(90006, "01", "2026-10-06", "777780673")}, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if got := id(`SELECT count(*)::text FROM tolls WHERE prepass_toll_id=90006 AND truck_id IS NULL`); got != "1" {
		t.Fatal("cross environment link")
	}
	exec(`UPDATE payroll_settlements SET finalized=true WHERE driver_id=$1`, driver)
	if err = repo.UndoPrePassTruckLink(ctx, in.ID, 1, actor); err == nil {
		t.Fatal("undo affected finalized statement")
	}
	exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver)
	if err = repo.UndoPrePassTruckLink(ctx, in.ID, 1, actor); err != nil {
		t.Fatal(err)
	}
	if got := id(`SELECT count(*)::text FROM tolls WHERE prepass_toll_id IN(90001,90002,90005) AND truck_id IS NULL AND truck_mapping_id IS NULL`); got != "3" {
		t.Fatal("undo missed future imports", got)
	}
	if err = repo.UndoPrePassTruckLink(ctx, in.ID, 1, actor); err == nil {
		t.Fatal("stale undo accepted")
	}
	if got := id(`SELECT count(*)::text FROM prepass_truck_mapping_events WHERE mapping_id=$1`, in.ID); got != "2" {
		t.Fatal("missing mapping audit", got)
	}
	// Closed investor statements also block manual links, even with payroll open.
	in.ID = id(`SELECT gen_random_uuid()::text`)
	in.To = "2026-10-02"
	exec(`INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report)
 SELECT $1,'2026-10-05',id,'{}',true,'{}' FROM investors WHERE is_company`, truck)
	preview, err = repo.PreviewPrePassTruckLink(ctx, in)
	if err != nil || !preview.Closed {
		t.Fatalf("investor freeze: %+v %v", preview, err)
	}
	in.Revision = preview.Revision
	if err = repo.LinkPrePassTruck(ctx, in, actor); err == nil {
		t.Fatal("changed finalized investor statement")
	}
	exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, truck)
	if err = repo.LinkPrePassTruck(ctx, in, actor); err != nil {
		t.Fatal(err)
	}
	bounded := []prepass.Transaction{makeToll(90007, "01", "2026-10-02", "777780673"), makeToll(90008, "01", "2026-10-03", "777780673")}
	if _, err = repo.UpsertDay(ctx, "production", time.Now(), bounded, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if got := id(`SELECT truck_id::text FROM tolls WHERE prepass_toll_id=90007`); got != truck {
		t.Fatal("end date not inclusive")
	}
	if got := id(`SELECT count(*)::text FROM tolls WHERE prepass_toll_id=90008 AND truck_id IS NULL`); got != "1" {
		t.Fatal("mapping continued past end date")
	}
	in.ID = id(`SELECT gen_random_uuid()::text`)
	in.From = "2026-10-02"
	in.To = "2026-10-03"
	preview, err = repo.PreviewPrePassTruckLink(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.Revision = preview.Revision
	if err = repo.LinkPrePassTruck(ctx, in, actor); err == nil {
		t.Fatal("overlapping active mapping accepted")
	}
	testSelectedTollLinks(t, ctx, pool, actor)
}
