package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/prepass"
)

// Runs on both the fresh schema and the incremental migration as mserp_app.
func testFleetTollLinking(t *testing.T, ctx context.Context, admin *pgx.Conn, repo *TollRepository, fake *prepassFleetFake, actor, truck, other string) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, err := admin.Exec(ctx, sql, args...); must(err) }
	id := func(sql string, args ...any) string {
		t.Helper()
		var v string
		must(admin.QueryRow(ctx, sql, args...).Scan(&v))
		return v
	}
	makeToll := func(n int64, unit, tag, day string) prepass.Transaction {
		return prepass.Transaction{TollID: n, AccountNumber: json.Number("123"), VehicleNumber: unit, PPDeviceID: tag, DeviceNumber: "00413749983", PostDateTime: "2026-10-05T12:00:00", ExitDateTime: day + "T12:00:00", TollCharge: json.Number("2.50")}
	}
	importTolls := func(env string, values ...prepass.Transaction) {
		t.Helper()
		_, err := repo.UpsertDay(ctx, env, time.Now(), values, time.Now(), false)
		must(err)
	}
	request := func() PrePassFleetChange {
		t.Helper()
		l := mustPrePassList(t, repo, ctx)
		v := l.Vehicles[0]
		in := PrePassFleetChange{RequestID: id(`SELECT gen_random_uuid()::text`), Action: "link", AccountNumber: v.Vehicle.AccountNumber, VIN: v.Vehicle.VIN, Revision: v.Revision, TruckID: truck, TollFrom: "2026-10-05"}
		for _, c := range l.Trucks {
			if c.ID == truck {
				in.TruckUpdatedAt = c.UpdatedAt.Format(time.RFC3339Nano)
			}
		}
		return in
	}
	preview := func(in *PrePassFleetChange) PrePassTruckPreview {
		t.Helper()
		p, err := repo.PreviewPrePassFleetLink(ctx, *in)
		must(err)
		in.TollRevision = p.Revision
		return p
	}
	assertTruck := func(n int64, want string) {
		t.Helper()
		if got := id(`SELECT coalesce(truck_id::text,'') FROM tolls WHERE prepass_toll_id=$1`, n); got != want {
			t.Fatalf("toll %d truck=%s, want %s", n, got, want)
		}
	}
	// Current roster was edited from 003 to 003A and assigned this tag. Neither
	// change silently turns the old unit rule into a newly reviewed identity.
	if mustPrePassList(t, repo, ctx).Vehicles[0].TollsLinked {
		t.Fatal("changed provider identity looks reviewed")
	}
	importTolls("production", makeToll(81001, "unrecognized", "777111222", "2026-10-05"), makeToll(81002, "003A", "different-tag", "2026-10-06"), makeToll(81003, "unrecognized", "777111222", "2026-10-04"), makeToll(81004, "004", "777111222", "2026-10-06"))
	foreign := makeToll(81005, "unrecognized", "777111222", "2026-10-06")
	foreign.AccountNumber = json.Number("456")
	importTolls("production", foreign)
	importTolls("nonproduction", makeToll(81006, "unrecognized", "777111222", "2026-10-06"))
	assertTruck(81004, other) // Existing exact-unit link must survive a different tag rule.
	in := request()
	p := preview(&in)
	if p.Count != 2 || p.Amount != "5.00" {
		t.Fatalf("wrong fleet preview %+v", p)
	}
	exec(`UPDATE tolls SET amount=3 WHERE prepass_toll_id=81001`)
	if repo.ChangePrePassFleet(ctx, in, actor) == nil {
		t.Fatal("stale preview accepted")
	}
	if id(`SELECT count(*)::text FROM prepass_fleet_operations WHERE id=$1`, in.RequestID) != "0" {
		t.Fatal("failed link left receipt or partial write")
	}
	// A finalized investor statement blocks the same atomic link operation.
	exec(`INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report) SELECT $1,'2026-10-05',id,'{}',true,'{}' FROM investors WHERE is_company`, truck)
	if !preview(&in).Closed {
		t.Fatal("finalized statement not detected")
	}
	if repo.ChangePrePassFleet(ctx, in, actor) == nil {
		t.Fatal("finalized statement changed")
	}
	exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, truck)
	preview(&in)
	must(repo.ChangePrePassFleet(ctx, in, actor))
	must(repo.ChangePrePassFleet(ctx, in, actor))
	for _, n := range []int64{81001, 81002} {
		assertTruck(n, truck)
	}
	for _, n := range []int64{81003, 81005, 81006} {
		assertTruck(n, "")
	}
	assertTruck(81004, other)
	if !mustPrePassList(t, repo, ctx).Vehicles[0].TollsLinked {
		t.Fatal("saved fleet link still flagged")
	}
	if id(`SELECT sum(jsonb_array_length(e.transaction_ids))::text FROM prepass_truck_mapping_events e JOIN prepass_truck_mappings m ON m.id=e.mapping_id WHERE m.fleet_request_id=$1`, in.RequestID) != "2" {
		t.Fatal("backfilled IDs missing from audit")
	}
	// Tag beats an unreviewed alias on new imports; exact unit is the fallback
	// even when the source has a tag for which no reviewed mapping exists.
	importTolls("production", makeToll(81007, "004", "777111222", "2026-10-07"), makeToll(81008, "003A", "new-tag", "2026-10-07"))
	assertTruck(81007, truck)
	assertTruck(81008, truck)
	importTolls("production", makeToll(81004, "003A", "777111222", "2026-10-06"))
	assertTruck(81004, other)
	// A later start closes this vehicle's old rules without unlinking old tolls.
	in = request()
	in.TollFrom = "2026-10-10"
	if preview(&in).Count != 0 {
		t.Fatal("already linked tolls appeared in preview")
	}
	must(repo.ChangePrePassFleet(ctx, in, actor))
	assertTruck(81001, truck)
	assertTruck(81004, other)
	if id(`SELECT count(*)::text FROM prepass_truck_mappings WHERE fleet_vin=$1 AND effective_to='2026-10-09'`, in.VIN) != "2" {
		t.Fatal("previous rules not dated closed")
	}
	importTolls("production", makeToll(81009, "unrecognized", "777111222", "2026-10-09"), makeToll(81010, "unrecognized", "777111222", "2026-10-10"))
	assertTruck(81009, truck)
	assertTruck(81010, truck)
	// An independently reviewed tag for another truck is never overwritten by
	// the unit fallback, and its conflict prevents creation of an ambiguous rule.
	exec(`INSERT INTO prepass_truck_mappings(id,environment,identity_key,truck_id,effective_from,actor_id) VALUES(gen_random_uuid(),'production','tag:OTHER-REVIEWED-TAG',$1,'2026-10-01',$2)`, other, actor)
	importTolls("production", makeToll(81011, "003A", "other-reviewed-tag", "2026-10-12"))
	assertTruck(81011, other)
	exec(`INSERT INTO prepass_truck_mappings(id,environment,identity_key,truck_id,effective_from,actor_id) VALUES(gen_random_uuid(),'production','tag:777111222',$1,'2026-10-01',$2)`, other, actor)
	in = request()
	if _, err := repo.PreviewPrePassFleetLink(ctx, in); err == nil {
		t.Fatal("overlapping truck rule accepted")
	}
	exec(`DELETE FROM prepass_truck_mappings WHERE identity_key='tag:777111222' AND fleet_vin IS NULL`)
	// Provider ambiguity invalidates the link before any writes.
	duplicate := fake.snapshot.Vehicles[0]
	duplicate.VIN = "1XKADP9X8DJ364749"
	fake.snapshot.Vehicles = append(fake.snapshot.Vehicles, duplicate)
	if _, err := repo.PreviewPrePassFleetLink(ctx, in); err == nil {
		t.Fatal("duplicate provider unit/tag accepted")
	}
	fake.snapshot.Vehicles = fake.snapshot.Vehicles[:len(fake.snapshot.Vehicles)-1]
}
