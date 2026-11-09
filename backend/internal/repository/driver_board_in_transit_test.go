package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testBoardInTransitQueue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := *original
	repo.now = func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "In Transit Queue Driver", PayType: "cpm", PayRate: .6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		exec(`DELETE FROM drivers WHERE id=$1`, d.ID)
		exec(`DELETE FROM loads WHERE id BETWEEN 88961 AND 88964`)
	}()
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES
 (88961,'TRANSIT-PAST','in_transit',0,0,'{}'),(88962,'TRANSIT-FUTURE',' In Transit ',0,0,'{}'),
 (88963,'TRANSIT-MANUAL','IN-TRANSIT',0,0,'{}'),(88964,'TRANSIT-PENDING','Dispatched',0,0,'{}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES
 ($1,'2026-10-01','TRANSIT-PAST',88961),($1,'2026-11-20','TRANSIT-FUTURE',88962),
 ($1,'2026-11-08','TRANSIT-PENDING',88964),($1,'2026-11-09','TRANSIT-UNMATCHED',NULL)`, d.ID)
	exec(`INSERT INTO driver_board(driver_id,status) VALUES($1,'ENROUTE')`, d.ID)
	exec(`INSERT INTO driver_board_load_state(driver_id,payload) VALUES($1,jsonb_build_object(
 'manual',jsonb_build_array(jsonb_build_object('planId',gen_random_uuid(),'number','TRANSIT-MANUAL','manual',true)),
 'restoredPlans',jsonb_build_object((SELECT plan_id::text FROM gross_board_entries WHERE driver_id=$1 AND load_number='TRANSIT-PAST'),true)))`, d.ID)
	view := func() BoardLoads {
		t.Helper()
		v, err := repo.Loads(ctx, d.ID, "2026-11-09")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	find := func(list []BoardLoad, number string) *BoardLoad {
		i := slices.IndexFunc(list, func(p BoardLoad) bool { return p.Number == number })
		if i < 0 {
			return nil
		}
		return &list[i]
	}
	// In-transit activity must not depend on an old explicit restoration.
	exec(`UPDATE driver_board_load_state SET payload=payload-'restoredPlans' WHERE driver_id=$1`, d.ID)
	v := view()
	if len(v.Next) != 5 || find(v.Next, "TRANSIT-PENDING") == nil || find(v.Next, "TRANSIT-UNMATCHED") == nil || find(v.Next, "TRANSIT-PENDING").Overdue {
		t.Fatalf("driver status suppressed pending/unmatched plans: %+v", v)
	}
	for _, number := range []string{"TRANSIT-PAST", "TRANSIT-FUTURE", "TRANSIT-MANUAL"} {
		p := find(v.Next, number)
		if p == nil || p.Overdue || p.Completed || find(v.Earlier, number) != nil {
			t.Fatalf("in-transit plan must be active in Next: %s %+v", number, v)
		}
	}
	if find(v.Next, "TRANSIT-PAST").CanRemoveFromQueue {
		t.Fatal("Gross Board removal protection changed")
	}
	var version int
	if err := pool.QueryRow(ctx, `SELECT version FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: version, FromDate: v.FromDate, Revision: v.Revision, Action: "add", Number: " transit-past "}, ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("in-transit queue accepted a manual duplicate", err)
	}
	if view().Revision != v.Revision {
		t.Fatal("rejected add mutated the board")
	}
	// A status change returns an old plan to the ordinary date window.
	exec(`UPDATE loads SET status='Dispatched' WHERE id=88961`)
	changed := view()
	if p := find(changed.Earlier, "TRANSIT-PAST"); p == nil || p.Overdue || changed.Revision == v.Revision {
		t.Fatal("old pending source did not follow the ordinary window", changed)
	}
	exec(`UPDATE loads SET status='in_transit' WHERE id=88961`)
	// Explicit current selection stays visible, never silently completed/cleared.
	current := *find(v.Next, "TRANSIT-PAST")
	exec(`UPDATE driver_board_load_state SET payload=payload || jsonb_build_object('current',$2::jsonb) WHERE driver_id=$1`, d.ID, current)
	exec(`UPDATE driver_board SET current_load='TRANSIT-PAST' WHERE driver_id=$1`, d.ID)
	selected := view()
	if selected.Current == nil || selected.Current.LoadID == nil || *selected.Current.LoadID != 88961 || selected.Current.Completed || find(selected.Next, "TRANSIT-PAST") != nil || find(selected.Earlier, "TRANSIT-PAST") != nil {
		t.Fatal("in-transit current selection changed", selected)
	}
	exec(`UPDATE driver_board SET current_load='' WHERE driver_id=$1`, d.ID)
	exec(`UPDATE driver_board_load_state SET payload=(payload-'current') || '{"completed":["load:88961"]}'::jsonb WHERE driver_id=$1`, d.ID)
	v = view()
	p := find(v.Unavailable, "TRANSIT-PAST")
	if p == nil || !p.Completed || !p.CanRestoreToQueue {
		t.Fatal("explicitly completed in-transit load must allow correction", v)
	}
	if err := pool.QueryRow(ctx, `SELECT version FROM driver_board WHERE driver_id=$1`, d.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: version, FromDate: v.FromDate, Revision: v.Revision, Action: "restore_completed", PlanID: p.PlanID}, ""); err != nil {
		t.Fatal("in-transit restore failed", err)
	}
	if restored := find(view().Next, "TRANSIT-PAST"); restored == nil || restored.Completed || restored.Overdue {
		t.Fatal("restored in-transit load is not active", view())
	}

	// Reproduce current slot zero followed by an in-transit slot one, with no
	// saved completion/order/restoration. The same-day Gross ordering must win.
	d2, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Transit Same Day Driver", PayType: "cpm", PayRate: .6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	defer exec(`DELETE FROM drivers WHERE id=$1`, d2.ID)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-11-09','TRANSIT-PENDING',88964)`, d2.ID)
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id) VALUES($1,'2026-11-09',1,'TRANSIT-FUTURE',88962)`, d2.ID)
	exec(`INSERT INTO driver_board(driver_id,status,current_load) VALUES($1,'ENROUTE','TRANSIT-PENDING')`, d2.ID)
	exec(`INSERT INTO driver_board_load_state(driver_id,payload) VALUES($1,jsonb_build_object('current',jsonb_build_object('planId',(SELECT plan_id FROM gross_board_entries WHERE driver_id=$1),'number','TRANSIT-PENDING','loadId',88964)))`, d2.ID)
	sameDay, err := repo.Loads(ctx, d2.ID, "2026-11-09")
	if err != nil {
		t.Fatal(err)
	}
	if sameDay.Current == nil || sameDay.Current.Number != "TRANSIT-PENDING" || len(sameDay.Next) != 1 || sameDay.Next[0].Number != "TRANSIT-FUTURE" || sameDay.Next[0].Slot != 1 || sameDay.Next[0].Completed || sameDay.Next[0].Overdue || len(sameDay.Earlier) != 0 || len(sameDay.Unavailable) != 0 {
		t.Fatal("same-day in-transit next slot moved to history", sameDay)
	}
}
