package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testBoardProgress(t *testing.T, ctx context.Context, pool *pgxpool.Pool, original *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	repo := *original
	repo.now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	d, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Progress Driver", PayType: "cpm", PayRate: 0.6, Active: true})
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
		exec(`DELETE FROM loads WHERE id IN(88121,88122)`)
	}()
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES
 (88121,'PROGRESS-A','Dispatched',100,100,100,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Atlanta","state":"GA"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}'),
 (88122,'PROGRESS-B','Dispatched',200,200,200,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Boston","state":"MA"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Miami","state":"FL"}}]}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-09-28','PROGRESS-A',88121)`, d.ID)
	actor := "progress-user-one"
	rows, err := repo.Save(ctx, []DriverBoardEntry{{DriverID: d.ID, CurrentLoad: "PROGRESS-A", Notes: "Keep notes", ETA: "2026-10-01T15:00"}}, actor)
	if err != nil {
		t.Fatal(err)
	}
	e := rows[0]
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
		action.DriverID = d.ID
		action.Version = e.Version
		action.FromDate = v.FromDate
		action.Revision = v.Revision
		result, err := repo.ChangeLoads(ctx, action, actor)
		if err != nil {
			t.Fatal(err)
		}
		e = result.Entry
		return result
	}
	undo := func(id int64) {
		t.Helper()
		var err error
		e, err = repo.Undo(ctx, id, d.ID, e.Version, actor, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Manual-only plans retain remove/restore and its progress/undo behavior.
	act(BoardLoadAction{Action: "add", Number: "PROGRESS-B"})
	if e.Status != "DISPATCHED" || view().SourceDestination != "Atlanta, GA" || e.UndoID == 0 {
		t.Fatal("new load did not start at pickup", e, view())
	}
	picked := act(BoardLoadAction{Action: "advance"})
	if e.Status != "RESERVED" || picked.Loads.SourceDestination != "Dallas, TX" {
		t.Fatal("pickup with next", picked)
	}
	next := picked.Loads.Next[0].PlanID
	hidden := act(BoardLoadAction{Action: "hide", PlanID: next})
	if e.Status != "ENROUTE" {
		t.Fatal("removing next did not change status", e)
	}
	restored := act(BoardLoadAction{Action: "restore", PlanID: next})
	if e.Status != "RESERVED" {
		t.Fatal("restoring next did not change status", e)
	}
	staleView := view()
	staleEntry := e
	delivered := act(BoardLoadAction{Action: "advance"})
	if e.CurrentLoad != "PROGRESS-B" || e.Status != "DISPATCHED" || delivered.Loads.SourceDestination != "Boston, MA" || e.Notes != "Keep notes" || e.ETA != "" {
		t.Fatal("delivery/promotion", delivered)
	}
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: staleEntry.Version, FromDate: staleView.FromDate, Revision: staleView.Revision, Action: "advance"}, actor); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("duplicate advance accepted", err)
	}
	if _, err := repo.Undo(ctx, e.UndoID, d.ID, e.Version, "progress-user-two", true); !errors.Is(err, ErrNotFound) {
		t.Fatal("personal undo accepted another actor", err)
	}
	undo(delivered.Entry.UndoID)
	if e.CurrentLoad != "PROGRESS-A" || view().SourceDestination != "Dallas, TX" {
		t.Fatal("promotion undo failed")
	}
	undo(restored.Entry.UndoID)
	undo(hidden.Entry.UndoID)
	undo(picked.Entry.UndoID)
	if e.Status != "DISPATCHED" || view().SourceDestination != "Atlanta, GA" || len(view().Next) != 1 {
		t.Fatal("consecutive personal undo failed")
	}
	act(BoardLoadAction{Action: "advance"})
	act(BoardLoadAction{Action: "advance"})
	lastPickup := act(BoardLoadAction{Action: "advance"})
	if e.Status != "ENROUTE" || lastPickup.Loads.SourceDestination != "Miami, FL" {
		t.Fatal("pickup without next", lastPickup)
	}
	finished := act(BoardLoadAction{Action: "advance"})
	if e.Status != "READY" || e.CurrentLoad != "" || e.Destination != "" || finished.Loads.Current != nil || len(finished.Loads.Next) != 0 || len(view().Next) != 0 {
		t.Fatal("last delivery should clear without NO LOAD", finished)
	}
	undo(finished.Entry.UndoID)
	if e.CurrentLoad != "PROGRESS-B" || e.Status != "ENROUTE" {
		t.Fatal("final delivery undo failed")
	}
	// Gross Board changes update RESERVED/ENROUTE on reads without progress writes.
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number) VALUES($1,'2026-10-02',1,'NEW-NEXT')`, d.ID)
	week, _ := time.Parse(time.DateOnly, "2026-09-28")
	board, err := repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range board.Entries {
		if entry.DriverID == d.ID && entry.Status != "RESERVED" {
			t.Fatal("new planned next not reflected", entry)
		}
	}
	exec(`UPDATE gross_board_extra_entries SET deleted=true WHERE driver_id=$1`, d.ID)
	e.Status = "SHOP"
	e.StatusEdited = true
	rows, err = repo.Save(ctx, []DriverBoardEntry{e}, actor)
	if err != nil {
		t.Fatal(err)
	}
	e = rows[0]
	v := view()
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: e.Version, FromDate: v.FromDate, Revision: v.Revision, Action: "advance"}, actor); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("manual exception advanced", err)
	}
	e.Notes = "my action"
	rows, err = repo.Save(ctx, []DriverBoardEntry{e}, actor)
	if err != nil {
		t.Fatal(err)
	}
	e = rows[0]
	mine := e.UndoID
	e.Notes = "someone else's edit"
	rows, err = repo.Save(ctx, []DriverBoardEntry{e}, "progress-user-two")
	if err != nil {
		t.Fatal(err)
	}
	e = rows[0]
	if _, err := repo.Undo(ctx, mine, d.ID, e.Version, actor, true); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("personal undo overwrote newer user edit", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loads WHERE id IN(88121,88122) AND status='Dispatched'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("progress changed upstream load statuses", err)
	}
	// A second delivery must survive the first delivery, including a reviewed
	// out-of-order choice, a manual address, and a personal undo.
	exec(`UPDATE loads SET raw_payload='{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Boston","state":"MA"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Miami","state":"FL"}},{"ordering":3,"stop_type":"delivery","location":{"city":"Orlando","state":"FL"}}]}' WHERE id=88122`)
	e.Status, e.StatusEdited = "DISPATCHED", true
	rows, err = repo.Save(ctx, []DriverBoardEntry{e}, actor)
	if err != nil {
		t.Fatal(err)
	}
	e = rows[0]
	v = view()
	orlando := v.Current.Stops[2].Key
	miami := v.Current.Stops[1].Key
	act(BoardLoadAction{Action: "advance", StopKey: orlando})
	if view().SourceDestination != "Orlando, FL" || e.CurrentLoad != "PROGRESS-B" {
		t.Fatal("chosen delivery not retained", view(), e)
	}
	pickup := v.Current.Stops[0].Key
	backToPickup := act(BoardLoadAction{Action: "advance", StopKey: pickup})
	if e.Status != "DISPATCHED" || view().SourceDestination != "Boston, MA" || len(view().CompletedStopKeys) != 0 {
		t.Fatal("pickup selection did not restore destination and Dispatched", view(), e)
	}
	undo(backToPickup.Entry.UndoID)
	if view().StopKey != orlando || e.Status != "ENROUTE" {
		t.Fatal("pickup selection undo did not restore delivery", view(), e)
	}
	e.Destination = "123 Custom Avenue, Orlando, FL"
	rows, err = repo.Save(ctx, []DriverBoardEntry{e}, actor)
	if err != nil {
		t.Fatal(err)
	}
	e = rows[0]
	if view().DestinationSource || view().StopKey != orlando {
		t.Fatal("manual address lost stop identity", view())
	}
	e.Destination = ""
	rows, err = repo.Save(ctx, []DriverBoardEntry{e}, actor)
	if err != nil {
		t.Fatal(err)
	}
	e = rows[0]
	if !view().DestinationSource || view().SourceDestination != "Orlando, FL" {
		t.Fatal("clearing override did not restore selected stop", view())
	}
	intermediate := act(BoardLoadAction{Action: "advance", StopKey: miami})
	if e.CurrentLoad != "PROGRESS-B" || intermediate.Loads.SourceDestination != "Miami, FL" || len(intermediate.Loads.CompletedStopKeys) != 1 {
		t.Fatal("first delivery completed whole load", intermediate)
	}
	undo(intermediate.Entry.UndoID)
	if view().StopKey != orlando || len(view().CompletedStopKeys) != 0 {
		t.Fatal("undo did not restore delivery progress", view())
	}
	act(BoardLoadAction{Action: "advance", StopKey: miami})
	act(BoardLoadAction{Action: "advance"})
	if e.CurrentLoad != "" || view().Current != nil {
		t.Fatal("final delivery did not finish load", view(), e)
	}

	// Recover a load after another one has become current and the plan date
	// has passed. Only the restored load's completion should be reset.
	repo.now = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
	e.CurrentLoad = "PROGRESS-A"
	rows, err = repo.Save(ctx, []DriverBoardEntry{e}, actor)
	if err != nil {
		t.Fatal(err)
	}
	e = rows[0]
	act(BoardLoadAction{Action: "advance"})
	e.ETA = "2026-10-02T14:00"
	rows, err = repo.Save(ctx, []DriverBoardEntry{e}, actor)
	if err != nil {
		t.Fatal(err)
	}
	e = rows[0]
	restore := BoardLoadAction{Action: "restore_completed", PlanID: next}
	exec(`UPDATE loads SET status='Invoiced' WHERE id=88122`)
	v = view()
	if len(v.Unavailable) != 1 || !v.Unavailable[0].Completed || v.Unavailable[0].CanRestoreToQueue {
		t.Fatal("provider-finished load should be read-only history", v.Unavailable)
	}
	restore.DriverID, restore.Version, restore.FromDate, restore.Revision = d.ID, e.Version, v.FromDate, v.Revision
	if _, err := repo.ChangeLoads(ctx, restore, actor); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("provider-finished load restored", err)
	}
	selectCompleted := restore
	selectCompleted.Action = "select"
	if _, err := repo.ChangeLoads(ctx, selectCompleted, actor); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("provider-finished load selected", err)
	}
	typedCompleted := e
	typedCompleted.CurrentLoad = "PROGRESS-B"
	if _, err := repo.Save(ctx, []DriverBoardEntry{typedCompleted}, actor); !errors.Is(err, ErrBoardLoadCompleted) {
		t.Fatal("provider-finished load assigned by typing", err)
	}
	exec(`UPDATE loads SET status='Dispatched' WHERE id=88122`)
	before := view()
	if len(before.Unavailable) != 1 || !before.Unavailable[0].Completed || !before.Unavailable[0].CanRestoreToQueue {
		t.Fatal("board-completed load has no recovery action", before.Unavailable)
	}
	requeued := act(restore)
	if len(requeued.Loads.Next) != 1 || requeued.Loads.Next[0].PlanID != next ||
		e.CurrentLoad != "PROGRESS-A" || e.Status != "RESERVED" || e.ETA != "2026-10-02T14:00" ||
		e.Notes != "someone else's edit" || requeued.Loads.StopKey != before.StopKey ||
		!slices.Equal(requeued.Loads.CompletedStopKeys, before.CompletedStopKeys) || e.UndoID == 0 {
		t.Fatal("restore did not preserve the current load and queue the completed plan", requeued)
	}
	ids, err := NewLoadRepository(pool).OperationalLoadIDs(ctx, repo.now(), repo.now().Add(time.Hour))
	if err != nil || !slices.Contains(ids, 88122) {
		t.Fatal("restored older plan is missing from source refresh", ids, err)
	}
	undo(e.UndoID)
	if v = view(); len(v.Next) != 0 || len(v.Unavailable) != 1 || !v.Unavailable[0].CanRestoreToQueue {
		t.Fatal("restore undo did not reinstate completion", v)
	}
	act(restore)
	selected := act(BoardLoadAction{Action: "select", PlanID: next})
	if selected.Entry.CurrentLoad != "PROGRESS-B" || selected.Loads.SourceDestination != "Boston, MA" ||
		len(selected.Loads.CompletedStopKeys) != 0 {
		t.Fatal("restored load retained old delivery progress", selected)
	}
}
