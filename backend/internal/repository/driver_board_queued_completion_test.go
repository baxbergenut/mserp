package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"slices"
	"testing"
	"time"
)

func testBoardQueuedCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := *original
	repo.now = func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Queued Completion Driver", PayType: "cpm", PayRate: .6, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		exec(`DELETE FROM drivers WHERE id=$1`, d.ID)
		exec(`DELETE FROM loads WHERE id BETWEEN 88971 AND 88973`)
	}()
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES (88971,'84V7859','in_transit',100,100,'{}'),(88972,'COMPLETE-CURRENT','Dispatched',200,200,'{}'),(88973,'COMPLETE-MANUAL','Dispatched',0,0,'{}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-11-02','84V7859',88971)`, d.ID)
	rows, err := repo.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "COMPLETE-CURRENT", Status: "RESERVED", Notes: "Keep current", Destination: "Manual destination", ETA: "2026-11-09T15:00"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	e := rows[0]
	exec("UPDATE driver_board SET status='RESERVED' WHERE driver_id=$1", d.ID)
	if err := pool.QueryRow(ctx, "SELECT status,version FROM driver_board WHERE driver_id=$1", d.ID).Scan(&e.Status, &e.Version); err != nil {
		t.Fatal(err)
	}
	view := func() BoardLoads {
		t.Helper()
		v, err := repo.Loads(ctx, d.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	find := func(list []BoardLoad, n string) *BoardLoad {
		i := slices.IndexFunc(list, func(p BoardLoad) bool { return p.Number == n })
		if i < 0 {
			return nil
		}
		return &list[i]
	}
	action := func(kind, plan string) BoardLoadAction {
		v := view()
		return BoardLoadAction{DriverID: d.ID, Version: e.Version, FromDate: v.FromDate, Revision: v.Revision, Action: kind, PlanID: plan}
	}
	act := func(a BoardLoadAction) BoardLoadResult {
		t.Helper()
		r, err := repo.ChangeLoads(ctx, a, "")
		if err != nil {
			t.Fatal(err)
		}
		e = r.Entry
		return r
	}
	initial := view()
	target := find(initial.Next, "84V7859")
	if target == nil || target.CanRemoveFromQueue {
		t.Fatal("Gross transit plan/protection", initial)
	}
	snapshot := func() string {
		t.Helper()
		var out string
		err := pool.QueryRow(ctx, `SELECT jsonb_build_object('gross',(SELECT to_jsonb(g) FROM gross_board_entries g WHERE driver_id=$1),'source',(SELECT to_jsonb(l) FROM loads l WHERE id=88971),'payroll',(SELECT coalesce(jsonb_agg(to_jsonb(p)),'[]'::jsonb) FROM payroll_statement_current p))::text`, d.ID).Scan(&out)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := snapshot()
	old := e
	stale := action("complete_queued", target.PlanID)
	bad := stale
	bad.Revision = "stale"
	if _, err = repo.ChangeLoads(ctx, bad, ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("stale revision", err)
	}
	bad = stale
	bad.Version--
	if _, err = repo.ChangeLoads(ctx, bad, ""); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("stale version", err)
	}
	bad = stale
	bad.PlanID = initial.Current.PlanID
	if _, err = repo.ChangeLoads(ctx, bad, ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("current accepted", err)
	}
	done := act(stale)
	if old.Status != "RESERVED" || done.Entry.Status != "ENROUTE" {
		t.Fatal("last queue completion did not preserve loaded progress", old, done.Entry)
	}
	p := find(done.Loads.Unavailable, "84V7859")
	if find(done.Loads.Next, "84V7859") != nil || p == nil || !p.Completed || !p.CanRestoreToQueue {
		t.Fatal("completion", done)
	}
	if e.CurrentLoad != old.CurrentLoad || e.Destination != old.Destination || e.ETA != old.ETA || e.Notes != old.Notes || done.Loads.Current.Number != initial.Current.Number || done.Loads.StopKey != initial.StopKey {
		t.Fatal("current changed", old, done)
	}
	if snapshot() != before {
		t.Fatal("Gross/source/payroll changed")
	}
	if _, err = repo.ChangeLoads(ctx, stale, ""); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("replay", err)
	}
	if _, err = repo.ChangeLoads(ctx, action("complete_queued", target.PlanID), ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("recomplete", err)
	}
	restored := act(action("restore_completed", target.PlanID))
	if restored.Entry.Status != "RESERVED" {
		t.Fatal("restoring queue did not reserve current trip", restored.Entry)
	}
	if find(restored.Loads.Next, "84V7859") == nil || restored.Loads.Current.Number != "COMPLETE-CURRENT" {
		t.Fatal("restore", restored)
	}
	act(action("complete_queued", target.PlanID))
	exec(`UPDATE loads SET status='Delivered' WHERE id=88971`)
	if p := find(view().Unavailable, "84V7859"); p != nil && p.CanRestoreToQueue {
		t.Fatal("provider terminal restore")
	}
	if _, err = repo.ChangeLoads(ctx, action("restore_completed", target.PlanID), ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("provider terminal accepted", err)
	}
	for _, number := range []string{"COMPLETE-MANUAL", "COMPLETE-UNMATCHED"} {
		a := action("add", "")
		a.Number = number
		r := act(a)
		p := find(r.Loads.Next, number)
		if p == nil {
			t.Fatal("manual add")
		}
		r = act(action("complete_queued", p.PlanID))
		if item := find(r.Loads.Unavailable, number); item == nil || !item.Completed || !item.CanRestoreToQueue {
			t.Fatal("manual completion", r)
		}
		r = act(action("restore_completed", p.PlanID))
		if find(r.Loads.Next, number) == nil {
			t.Fatal("manual restore", r)
		}
	}
}
