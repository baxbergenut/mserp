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
	// Gross Board precedence before a source arrives, without replacing active dispatch.
	act(BoardLoadAction{Action: "select", PlanID: planID})
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number) VALUES($1,$2,2,' 000-late ')`, d.ID, now.Format(time.DateOnly))
	if v = view(); v.Current == nil || v.Current.PlanID != planID || len(v.Next) != 0 {
		t.Fatal("Gross arrival replaced active dispatch or duplicated it", v)
	}
	act(BoardLoadAction{Action: "clear"})
	if v = view(); len(v.Next) != 1 || v.Next[0].Manual || v.Next[0].CanRemoveFromQueue || v.Next[0].Slot != 2 || v.Next[0].Date != now.Format(time.DateOnly) || len(v.Week) != 1 {
		t.Fatal("unimported Gross Board did not take precedence", v)
	}
	grossPlanID := v.Next[0].PlanID
	act(BoardLoadAction{Action: "complete_queued", PlanID: grossPlanID})
	if v = view(); len(v.Next) != 0 || len(v.Unavailable) != 1 || v.Unavailable[0].PlanID != grossPlanID {
		t.Fatal("completion lost Gross identity", v)
	}
	act(BoardLoadAction{Action: "restore_completed", PlanID: grossPlanID})
	if len(view().Next) != 1 {
		t.Fatal("Gross completion alias did not restore", view())
	}
	act(BoardLoadAction{Action: "select", PlanID: grossPlanID})
	if v = view(); v.Current == nil || v.Current.PlanID != grossPlanID || len(v.Next) != 0 {
		t.Fatal("unresolved Gross current load duplicated in queue", v)
	}
	act(BoardLoadAction{Action: "clear"})
	exec(`DELETE FROM gross_board_extra_entries WHERE driver_id=$1`, d.ID)
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
	// Preserve the queued pickup override and custom position across replacement.
	v = view()
	act(BoardLoadAction{Action: "stop_time", PlanID: planID, StopKey: v.Next[0].Stops[0].Key, ManualStopTime: "09:30"})
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number) VALUES($1,$2,3,'QUEUE-OTHER')`, d.ID, now.Format(time.DateOnly))
	v = view()
	otherPlan := v.Next[1].PlanID
	act(BoardLoadAction{Action: "order", Order: []string{otherPlan, planID}})
	// A later Gross Board entry does not create a second queue item.
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,$2,'000-LATE',989001)`, d.ID, now.Format(time.DateOnly))
	v = view()
	if len(v.Next) != 2 || len(v.Week) != 2 || v.Next[0].PlanID != otherPlan || v.Next[1].Manual || v.Next[1].Slot != 0 || v.Next[1].Stops[0].ManualStopTime != "09:30" {
		t.Fatal("Gross replacement lost queue position, slot or stop override", v)
	}
	exec(`DELETE FROM gross_board_extra_entries WHERE driver_id=$1 AND slot=3`, d.ID)

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
	v = view()
	if len(v.Next) != 1 || v.Next[0].Manual || v.Next[0].CanRemoveFromQueue || v.Next[0].Date != now.Format(time.DateOnly) {
		t.Fatal("Gross counterpart did not take precedence", v)
	}
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: e.Version, FromDate: v.FromDate, Revision: v.Revision, Action: "hide", PlanID: v.Next[0].PlanID}, ""); !errors.Is(err, ErrBoardGrossPlanRemoval) {
		t.Fatal("Gross counterpart became removable", err)
	}
	exec(`DELETE FROM gross_board_entries WHERE driver_id=$1 AND load_record_id=989001`, d.ID)
	if v = view(); len(v.Next) != 1 || !v.Next[0].CanRemoveFromQueue {
		t.Fatal("manual-only plan did not regain removal", v)
	}
	r = act(BoardLoadAction{Action: "hide", PlanID: planID})
	if len(r.Loads.Next) != 0 || len(r.Loads.Hidden) != 1 {
		t.Fatal("hide")
	}
	// A hidden manual record never hides a newly added Gross Board plan.
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,$2,'000-LATE',989001)`, d.ID, now.Format(time.DateOnly))
	if v = view(); len(v.Next) != 1 || v.Next[0].Manual || len(v.Hidden) != 1 {
		t.Fatal("hidden manual suppressed Gross plan", v)
	}
	exec(`DELETE FROM gross_board_entries WHERE driver_id=$1`, d.ID)
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
	// Recreated source: a deleted number match must not block the sole live record.
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number) VALUES($1,$2,'AMBIGUOUS')`, d.ID, now.Format(time.DateOnly))
	if p := view().Next[0]; p.LoadID != nil {
		t.Fatal("ambiguous Gross source guessed", p)
	}
	exec(`INSERT INTO load_source_availability(load_id,present,checked_at) VALUES(989003,false,now())`)
	v = view()
	if len(v.Next) != 1 || v.Next[0].Manual || v.Next[0].LoadID == nil || *v.Next[0].LoadID != 989002 || len(v.Next[0].Stops) == 0 {
		t.Fatal("deleted duplicate blocked live Gross source", v)
	}
	exec(`UPDATE gross_board_entries SET load_record_id=989003 WHERE driver_id=$1`, d.ID)
	v = view()
	foundMissing := false
	for _, p := range v.Unavailable {
		if p.LoadID != nil && *p.LoadID == 989003 && p.SourceMissing {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatal("explicit missing source silently retargeted", v)
	}
	exec(`DELETE FROM load_source_availability WHERE load_id=989003`)
	exec(`DELETE FROM gross_board_entries WHERE driver_id=$1`, d.ID)
	// An ambiguous manual number and a pinned Gross Board plan remain distinct.
	// Removing the placeholder must leave the real planned source untouched.
	manualAmbiguous := view().Next[0].PlanID
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,$2,'AMBIGUOUS',989003)`, d.ID, now.Format(time.DateOnly))
	v = view()
	if len(v.Next) != 2 || !v.Next[0].Manual || !v.Next[0].CanRemoveFromQueue || v.Next[1].CanRemoveFromQueue {
		t.Fatal("ambiguous duplicate removal policy", v)
	}
	r = act(BoardLoadAction{Action: "hide", PlanID: manualAmbiguous})
	if len(r.Loads.Next) != 1 || r.Loads.Next[0].Manual || len(r.Loads.Hidden) != 1 {
		t.Fatal("ambiguous removal affected Gross Board", r.Loads)
	}
	act(BoardLoadAction{Action: "restore", PlanID: manualAmbiguous})
	exec(`DELETE FROM gross_board_entries WHERE driver_id=$1 AND load_record_id=989003`, d.ID)
	// An unresolved number cannot reappear as upcoming when every candidate is finished.
	exec(`UPDATE loads SET status='Delivered' WHERE id IN (989002,989003)`)
	v = view()
	if len(v.Next) != 0 {
		t.Fatal("finished ambiguous number requeued", v)
	}
	foundCompleted := false
	for _, p := range v.Unavailable {
		if p.PlanID == manualAmbiguous {
			foundCompleted = p.Completed && p.LoadID == nil && len(p.Stops) == 0
		}
	}
	if !foundCompleted {
		t.Fatal("ambiguous completion guessed source or lost history", v)
	}
	// One unfinished candidate must keep the placeholder available for review.
	exec(`UPDATE loads SET status='Dispatched' WHERE id=989003`)
	if len(view().Next) != 1 || view().Next[0].LoadID != nil {
		t.Fatal("mixed-status ambiguity was hidden or guessed", view())
	}
	// An earlier Gross Board plan can still be entered as the next dispatch.
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number) VALUES($1,$2,1,'EARLIER-NEXT')`, d.ID, now.AddDate(0, 0, -14).Format(time.DateOnly))
	r = act(BoardLoadAction{Action: "add", Number: "EARLIER-NEXT"})
	if len(r.Loads.Next) != 2 || r.Loads.Next[1].Number != "EARLIER-NEXT" || !r.Loads.Next[1].Manual {
		t.Fatal("earlier plan blocked manual dispatch", r)
	}
}
