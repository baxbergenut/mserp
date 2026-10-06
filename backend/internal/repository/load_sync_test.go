package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
)

func testOperationalLoadSync(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := NewLoadRepository(pool)
	board := *original
	board.now = func() time.Time { return time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Late Import Driver", PayType: "cpm", PayRate: 0.6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		exec(`DELETE FROM drivers WHERE id=$1`, d.ID)
		exec(`DELETE FROM loads WHERE id BETWEEN 88101 AND 88107`)
	}()
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number) VALUES($1,'2026-10-05','LATE-SOURCE')`, d.ID)
	entries, err := board.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "LATE-SOURCE", ETA: "2026-10-06T15:00", Notes: "Keep notes"}})
	if err != nil {
		t.Fatal(err)
	}
	e := entries[0]
	view := func() BoardLoads {
		t.Helper()
		v, err := board.Loads(ctx, d.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := view()
	if before.Current == nil || before.Current.LoadID != nil || before.DestinationSource {
		t.Fatalf("unmatched selection %+v", before)
	}
	history, err := board.History(ctx, []string{d.ID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,pickup_time,created_datetime,raw_payload) VALUES
 (88101,'DATE-TODAY','delivered',100,100,'2026-10-06T00:01:00Z',NULL,'{}'),
 (88102,'DATE-START','cancelled',100,100,'2026-09-29T00:00:00Z',NULL,'{}'),
 (88103,'DATE-END','dispatched',100,100,'2026-10-10T00:00:00Z',NULL,'{}'),
 (88104,'DATE-OLD','dispatched',100,100,'2026-09-28T23:59:59Z',NULL,'{}'),
 (88105,'NEW-UNDATED','booked',100,100,NULL,'2026-10-06T12:00:00Z','{}'),
 (88106,'LATE-SOURCE','booked',100,100,'2025-01-01T00:01:00Z',NULL,
 '{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Atlanta","state":"GA"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}')`)
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	ids, err := repo.OperationalLoadIDs(ctx, from, from.AddDate(0, 0, 11))
	if err != nil {
		t.Fatal(err)
	}
	selected := map[int]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	for _, id := range []int{88101, 88102, 88105, 88106} {
		if !selected[id] {
			t.Fatalf("load %d missing from operational window", id)
		}
	}
	for _, id := range []int{88103, 88104} {
		if selected[id] {
			t.Fatalf("out-of-window load %d selected", id)
		}
	}
	if ids[0] != 88106 {
		t.Fatal("current load not prioritized", ids)
	}
	late := view()
	if late.Current == nil || late.Current.LoadID == nil || *late.Current.LoadID != 88106 || !late.DestinationSource || late.SourceDestination != "Atlanta, GA" {
		t.Fatalf("late source did not hydrate: %+v", late)
	}
	afterHistory, err := board.History(ctx, []string{d.ID}, 0)
	if err != nil || len(afterHistory.Items) != len(history.Items) {
		t.Fatal("source read wrote history", err)
	}
	var storedStatus, storedETA, storedNotes string
	var storedVersion int
	if err := pool.QueryRow(ctx, `SELECT status,eta,notes,version FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&storedStatus, &storedETA, &storedNotes, &storedVersion); err != nil {
		t.Fatal(err)
	}
	if storedStatus != e.Status || storedETA != e.ETA || storedNotes != e.Notes || storedVersion != e.Version {
		t.Fatal("read changed operational fields")
	}
	manual, err := board.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: e.Version, HomeVersion: e.HomeVersion, Revision: late.Revision, FromDate: late.FromDate, Action: "manual"}, "")
	if err != nil || manual.Entry.Destination != "Atlanta, GA" || manual.Loads.DestinationSource {
		t.Fatal("manual override lost projected source", err)
	}
	e = manual.Entry
	e.Destination = ""
	entries, err = board.Save(ctx, []DriverBoardEntry{e})
	if err != nil {
		t.Fatal(err)
	}
	e = entries[0]
	if view().DestinationSource {
		t.Fatal("explicit manual blank was replaced by late source")
	}
	e.ResolveCurrentLoad = true
	entries, err = board.Save(ctx, []DriverBoardEntry{e})
	if err != nil {
		t.Fatal(err)
	}
	e = entries[0]
	late = view()
	if !late.DestinationSource {
		t.Fatal("explicit reselection did not restore source")
	}
	advanced, err := board.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: e.Version, HomeVersion: e.HomeVersion, Revision: late.Revision, FromDate: late.FromDate, Action: "advance"}, "")
	if err != nil || advanced.Loads.SourceDestination != "Dallas, TX" || advanced.Entry.Status != "ENROUTE" {
		t.Fatalf("one-click pickup failed: %+v %v", advanced, err)
	}
	// Refresh replaces source details and resolves existing identities, without
	// replaying the source truck assignment or inserting missing IDs.
	truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "SYNC-CURRENT", DriverID: &d.ID, Status: "available", Active: true, IsCompanyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { exec(`DELETE FROM trucks WHERE id=$1`, truck.ID) }()
	other, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "SYNC-SOURCE", Status: "available", Active: true, IsCompanyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { exec(`DELETE FROM trucks WHERE id=$1`, other.ID) }()
	name, unit, number := "Late Import Driver", "SYNC-SOURCE", "RENAMED"
	load := datatruck.Load{ID: 88105, LoadID: &number, Status: "delivered", Trip: &datatruck.Trip{DriverFullName: &name, TruckUnitNumber: &unit}}
	payload, _ := json.Marshal(load)
	record, err := LoadToRecord(load, payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	missing := record
	missing.ID = 88107
	if err := repo.RefreshLoads(ctx, []LoadRecord{record, missing}); err != nil {
		t.Fatal(err)
	}
	var actualTruck, actualDriver, actualNumber, actualStatus string
	if err := pool.QueryRow(ctx, `SELECT truck_id::text FROM truck_driver_assignments WHERE driver_id=$1 AND unassigned_at IS NULL`, d.ID).Scan(&actualTruck); err != nil {
		t.Fatal(err)
	}
	if actualTruck != truck.ID {
		t.Fatal("source refresh reassigned driver")
	}
	if err := pool.QueryRow(ctx, `SELECT driver_id::text,load_id,status FROM loads WHERE id=88105`).Scan(&actualDriver, &actualNumber, &actualStatus); err != nil {
		t.Fatal(err)
	}
	if actualDriver != d.ID || actualNumber != number || actualStatus != "delivered" {
		t.Fatal("source metadata not refreshed")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM loads WHERE id=88107)`).Scan(&exists); err != nil || exists {
		t.Fatal("refresh inserted a new ID", err)
	}
	// Morning reconciliation still repairs holes below its starting watermark.
	if err := repo.ReconcileLoads(ctx, []LoadRecord{missing}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM loads WHERE id=88107)`).Scan(&exists); err != nil || !exists {
		t.Fatal("morning reconciliation did not recover missing source", err)
	}
}

func TestLateBoardStopRespectsManualAndIdentity(t *testing.T) {
	id, otherID := 12, 13
	current := BoardLoad{PlanID: "plan", LoadID: &id, Stops: []BoardStop{{Key: "pu", Type: "pickup", Location: "Atlanta, GA"}, {Key: "del", Type: "delivery", Location: "Dallas, TX"}}}
	for _, tc := range []struct {
		name, status, destination string
		manual                    bool
		snapshot                  BoardLoad
		key                       string
	}{
		{"pickup", "DISPATCHED", "", false, BoardLoad{PlanID: "plan"}, "pu"},
		{"delivery", "ENROUTE", "", false, BoardLoad{PlanID: "plan", LoadID: &id}, "del"},
		{"reserved", "RESERVED", "", false, BoardLoad{PlanID: "plan"}, "del"},
		{"manual text", "DISPATCHED", "My city", false, BoardLoad{PlanID: "plan"}, ""},
		{"manual blank", "DISPATCHED", "", true, BoardLoad{PlanID: "plan"}, ""},
		{"replaced identity", "DISPATCHED", "", false, BoardLoad{PlanID: "plan", LoadID: &otherID}, ""},
		{"replaced plan", "DISPATCHED", "", false, BoardLoad{PlanID: "other"}, ""},
		{"manual status", "HOME", "", false, BoardLoad{PlanID: "plan"}, ""},
		{"existing stops", "DISPATCHED", "", false, current, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stop := lateBoardStop(boardLoadState{Current: &tc.snapshot, DestinationManual: tc.manual}, current, tc.status, tc.destination)
			if tc.key == "" {
				if stop != nil {
					t.Fatal("unexpected source projection", stop)
				}
			} else if stop == nil || stop.Key != tc.key {
				t.Fatal("missing expected stop", stop)
			}
		})
	}
}
