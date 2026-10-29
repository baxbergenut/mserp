package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testManualNextLoads(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := *original
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Manual Next Driver", PayType: "cpm", Active: true})
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
		exec(`DELETE FROM loads WHERE id BETWEEN 989001 AND 989003`)
	}()
	e := DriverBoardEntry{DriverID: d.ID, Notes: "Keep manual notes", ETA: "12:00"}
	saved, err := repo.Save(ctx, []DriverBoardEntry{e})
	if err != nil {
		t.Fatal(err)
	}
	e = saved[0]
	view := func() BoardLoads {
		t.Helper()
		v, err := repo.Loads(ctx, d.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	act := func(action BoardLoadAction) BoardLoadResult {
		t.Helper()
		v := view()
		action.DriverID, action.Version, action.FromDate, action.Revision = d.ID, e.Version, v.FromDate, v.Revision
		r, err := repo.ChangeLoads(ctx, action, "")
		if err != nil {
			t.Fatal(err)
		}
		e = r.Entry
		return r
	}
	r := act(BoardLoadAction{Action: "add", Number: "  000-LATE  "})
	if len(r.Loads.Next) != 1 || r.Loads.Next[0].Number != "000-LATE" || r.Loads.Next[0].LoadID != nil || e.CurrentLoad != "" || e.ETA != "12:00" || e.Notes != "Keep manual notes" {
		t.Fatalf("placeholder: %+v", r)
	}
	h, err := repo.History(ctx, []string{d.ID}, 0)
	if err != nil || len(h.Items) == 0 {
		t.Fatal("missing add history", err)
	}
	e, err = repo.Undo(ctx, h.Items[0].ID, d.ID, e.Version, "")
	if err != nil || len(view().Next) != 0 {
		t.Fatal("add undo", err)
	}
	r = act(BoardLoadAction{Action: "add", Number: "000-LATE"})
	planID := r.Loads.Next[0].PlanID
	v := view()
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: e.Version, FromDate: v.FromDate, Revision: v.Revision, Action: "add", Number: "000-late"}, ""); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("duplicate accepted", err)
	}
	now = now.AddDate(0, 0, 10)
	if v = view(); len(v.Next) != 1 || v.Next[0].PlanID != planID {
		t.Fatal("placeholder aged out", v)
	}
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989001,'000-LATE','Dispatched',0,0,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Dallas","state":"TX"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Boston","state":"MA"}}]}')`)
	v = view()
	if len(v.Next) != 1 || v.Next[0].LoadID == nil || *v.Next[0].LoadID != 989001 || v.Next[0].PlanID != planID || len(v.Next[0].Stops) != 2 {
		t.Fatal("late import", v)
	}
	ids, err := NewLoadRepository(pool).OperationalLoadIDs(ctx, now.AddDate(0, 0, -1), now)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		if id == 989001 {
			found = true
		}
	}
	if !found {
		t.Fatal("manual queue omitted from operational refresh")
	}
	// A later Gross Board entry does not create a second queue item.
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,$2,'000-LATE',989001)`, d.ID, now.Format(time.DateOnly))
	if len(view().Next) != 1 || len(view().Week) != 1 {
		t.Fatal("Gross Board duplicate")
	}
	// An old manual entry must not dispatch again after its source is invoiced.
	exec(`UPDATE loads SET status=' Invoiced ' WHERE id=989001`)
	if v = view(); len(v.Next) != 0 || len(v.Unavailable) != 1 {
		t.Fatal("invoiced manual load remained upcoming", v)
	}
	ids, err = NewLoadRepository(pool).OperationalLoadIDs(ctx, now.AddDate(0, 0, -1), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == 989001 {
			t.Fatal("invoiced manual load remained an operational refresh candidate")
		}
	}
	exec(`UPDATE loads SET status='Dispatched' WHERE id=989001`)
	r = act(BoardLoadAction{Action: "hide", PlanID: planID})
	if len(r.Loads.Next) != 0 || len(r.Loads.Hidden) != 1 {
		t.Fatal("hide")
	}
	r = act(BoardLoadAction{Action: "restore", PlanID: planID})
	if len(r.Loads.Next) != 1 {
		t.Fatal("restore")
	}
	r = act(BoardLoadAction{Action: "select", PlanID: planID})
	if r.Loads.SourceDestination != "Dallas, TX" || e.Status != "DISPATCHED" {
		t.Fatal("current", r)
	}
	exec(`UPDATE loads SET status='invoiced' WHERE id=989001`)
	if v = view(); v.Current == nil || v.Current.PlanID != planID || v.Current.Warning == "" || len(v.Next) != 0 {
		t.Fatal("invoiced status must retain the explicit current selection for review", v)
	}
	exec(`UPDATE loads SET status='Dispatched' WHERE id=989001`)
	act(BoardLoadAction{Action: "advance"})
	if view().SourceDestination != "Boston, MA" {
		t.Fatal("delivery")
	}
	act(BoardLoadAction{Action: "advance"})
	if len(view().Next) != 0 {
		t.Fatal("completed requeued")
	}
	// Unknown current loads also hydrate after selection, without advancing progress.
	r = act(BoardLoadAction{Action: "add", Number: "NEXT-UNKNOWN"})
	unknownID := r.Loads.Next[0].PlanID
	act(BoardLoadAction{Action: "select", PlanID: unknownID})
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989002,'NEXT-UNKNOWN','Dispatched',0,0,'{"stops":[{"stop_type":"pickup","location":{"city":"Miami","state":"FL"}}]}')`)
	if v = view(); v.Current.LoadID == nil || v.SourceDestination != "Miami, FL" {
		t.Fatal("late current details", v)
	}
	// Multiple source rows are ambiguous, never silently picked.
	act(BoardLoadAction{Action: "add", Number: "AMBIGUOUS"})
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989003,'AMBIGUOUS','Dispatched',0,0,'{}')`)
	exec(`UPDATE loads SET load_id='AMBIGUOUS' WHERE id=989002`)
	if p := view().Next[0]; p.LoadID != nil || len(p.Stops) != 0 || p.Warning == "" {
		t.Fatal("ambiguous source guessed", p)
	}
	// An earlier Gross Board plan can still be entered as the next dispatch.
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number) VALUES($1,$2,1,'EARLIER-NEXT')`, d.ID, now.AddDate(0, 0, -1).Format(time.DateOnly))
	r = act(BoardLoadAction{Action: "add", Number: "EARLIER-NEXT"})
	if len(r.Loads.Next) != 2 || r.Loads.Next[1].Number != "EARLIER-NEXT" || !r.Loads.Next[1].Manual {
		t.Fatal("earlier plan blocked manual dispatch", r)
	}
}
