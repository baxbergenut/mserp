package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/prepass"
	"testing"
	"time"
)

func testSelectedTollLinks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(sql string, args ...any) string {
		t.Helper()
		var out string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	source := scalar(`INSERT INTO trucks(unit_number) VALUES('TX-SOURCE') RETURNING id`)
	target := scalar(`INSERT INTO trucks(unit_number) VALUES('TX-TARGET') RETURNING id`)
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,truck_unit,pickup_time,delivery_time) VALUES(991000,'PAST-SOURCE','delivered',100,100,'TX-SOURCE','2026-09-01','2026-09-02')`)
	repo := NewTollRepository(pool)
	makeToll := func(n int64, unit string) prepass.Transaction {
		return prepass.Transaction{TollID: n, AccountNumber: json.Number("1"), PostDateTime: "2026-10-05T12:00:00", ExitDateTime: "2026-10-03T12:00:00", VehicleNumber: unit, PPDeviceID: "SELECTED-TAG", DeviceNumber: "different-device-number", TollCharge: json.Number("2.50")}
	}
	rows := []prepass.Transaction{makeToll(91001, "TX-UNKNOWN"), makeToll(91002, "TX-SOURCE"), makeToll(91003, "TX-UNKNOWN")}
	if _, err := repo.UpsertDay(ctx, "production", time.Now(), rows, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	first := scalar(`SELECT id FROM tolls WHERE prepass_toll_id=91001`)
	second := scalar(`SELECT id FROM tolls WHERE prepass_toll_id=91002`)
	list, err := repo.PrePassTruckLinks(ctx, "2026-09-01")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Toll{}
	for _, v := range list.Transactions {
		found[v.ID] = v
	}
	if found[first].ID == "" || found[second].Flag == nil || found[second].TruckID == nil {
		t.Fatal("attention must include separate unlinked and linked flagged transactions")
	}
	in := PrePassTruckLinkInput{ID: scalar(`SELECT gen_random_uuid()`), TruckID: target, TransactionIDs: []string{first, second}}
	preview, err := repo.PreviewPrePassTruckLink(ctx, in)
	if err != nil || preview.Count != 2 || preview.Amount != "5.00" {
		t.Fatalf("selection preview: %+v %v", preview, err)
	}
	in.Revision = preview.Revision
	exec(`UPDATE tolls SET amount=3 WHERE id=$1`, first)
	if repo.LinkPrePassTruck(ctx, in, actor) == nil {
		t.Fatal("accepted stale selected transaction")
	}
	preview, err = repo.PreviewPrePassTruckLink(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.Revision = preview.Revision
	exec(`INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report) SELECT $1,'2026-10-05',id,'{}',true,'{}' FROM investors WHERE is_company`, source)
	frozen, err := repo.PreviewPrePassTruckLink(ctx, in)
	if err != nil || !frozen.Closed {
		t.Fatalf("source statement protection %+v %v", frozen, err)
	}
	if repo.LinkPrePassTruck(ctx, in, actor) == nil {
		t.Fatal("changed finalized source statement")
	}
	if scalar(`SELECT count(*) FROM tolls WHERE id=ANY($1::uuid[]) AND truck_id=$2`, in.TransactionIDs, target) != "0" {
		t.Fatal("partial batch on failure")
	}
	exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, source)
	exec(`INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report) SELECT $1,'2026-10-05',id,'{}',true,'{}' FROM investors WHERE is_company`, target)
	if repo.LinkPrePassTruck(ctx, in, actor) == nil {
		t.Fatal("changed finalized destination statement")
	}
	exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, target)
	driver := scalar(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Selected Toll Driver','selected toll driver','cpm',0.7) RETURNING id`)
	exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01')`, source, driver)
	exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-05','{}')`, driver)
	if repo.LinkPrePassTruck(ctx, in, actor) == nil {
		t.Fatal("changed finalized source driver statement")
	}
	exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver)
	if err = repo.LinkPrePassTruck(ctx, in, actor); err != nil {
		t.Fatal(err)
	}
	if err = repo.LinkPrePassTruck(ctx, in, actor); err != nil {
		t.Fatal("idempotency", err)
	}
	if scalar(`SELECT count(*) FROM tolls WHERE id=ANY($1::uuid[]) AND truck_id=$2 AND transaction_link_id=$3 AND truck_mapping_id IS NULL`, in.TransactionIDs, target, in.ID) != "2" {
		t.Fatal("wrong selected links")
	}
	if scalar(`SELECT count(*) FROM tolls WHERE prepass_toll_id=91003 AND truck_id IS NULL`) != "1" {
		t.Fatal("linked unselected row with same tag")
	}
	if scalar(`SELECT jsonb_array_length(transactions_before) FROM toll_transaction_links WHERE id=$1`, in.ID) != "2" {
		t.Fatal("missing before snapshots")
	}
	if _, err = repo.UpsertDay(ctx, "production", time.Now(), append(rows, makeToll(91004, "TX-UNKNOWN")), time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if scalar(`SELECT truck_id FROM tolls WHERE id=$1`, second) != target {
		t.Fatal("resync overwrote explicit correction")
	}
	if scalar(`SELECT count(*) FROM tolls WHERE prepass_toll_id=91004 AND truck_id IS NULL`) != "1" {
		t.Fatal("selected link changed future imports")
	}
	// A pre-existing rule must not silently replace the selected destination.
	mapping := scalar(`INSERT INTO prepass_truck_mappings(id,environment,identity_key,truck_id,effective_from,actor_id) VALUES(gen_random_uuid(),'production','tag:SELECTED-TAG',$1,'2026-10-01',$2) RETURNING id`, source, actor)
	third := scalar(`SELECT id FROM tolls WHERE prepass_toll_id=91003`)
	explicit := PrePassTruckLinkInput{ID: scalar(`SELECT gen_random_uuid()`), TruckID: target, TransactionIDs: []string{third}}
	p, e := repo.PreviewPrePassTruckLink(ctx, explicit)
	if e != nil {
		t.Fatal(e)
	}
	explicit.Revision = p.Revision
	if e = repo.LinkPrePassTruck(ctx, explicit, actor); e != nil {
		t.Fatal(e)
	}
	if scalar(`SELECT truck_id FROM tolls WHERE id=$1`, third) != target {
		t.Fatal("mapping replaced explicit destination")
	}
	if e = repo.UndoPrePassTruckLink(ctx, mapping, 1, actor); e != nil {
		t.Fatal(e)
	}
	if scalar(`SELECT truck_id FROM tolls WHERE id=$1`, third) != target {
		t.Fatal("mapping undo removed explicit link")
	}
	// Corrected attribution reassesses coverage without rewriting source units.
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,truck_unit,pickup_time,delivery_time) VALUES(991001,'COVER-TARGET','delivered',100,100,'TX-TARGET','2026-10-02','2026-10-04')`)
	flags, e := transactionFlags(ctx, pool, "toll", []string{second}, time.Now().UTC())
	if e != nil || flags[second] != nil {
		t.Fatalf("corrected truck coverage: %+v %v", flags, e)
	}
	if scalar(`SELECT equipment_unit FROM tolls WHERE id=$1`, second) != "TX-SOURCE" {
		t.Fatal("rewrote source identity")
	}
	attention, e := repo.attentionTolls(ctx, "2026-09-01")
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range attention {
		if v.ID == second {
			t.Fatal("covered linked transaction remained in attention")
		}
	}
	changed := in
	changed.TruckID = source
	if repo.LinkPrePassTruck(ctx, changed, actor) == nil {
		t.Fatal("reused batch id with different truck")
	}
	for _, ids := range [][]string{{}, {first, first}, {"bad-id"}, make([]string, 101)} {
		if _, err = repo.PreviewPrePassTruckLink(ctx, PrePassTruckLinkInput{TruckID: target, TransactionIDs: ids}); err == nil {
			t.Fatal("accepted invalid selection", ids)
		}
	}
	testTollFlagReviews(t, ctx, pool, actor)
}
