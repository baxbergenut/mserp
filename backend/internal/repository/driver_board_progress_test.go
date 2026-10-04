package repository

import (
	"context"
	"errors"
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
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-09-28','PROGRESS-A',88121),($1,'2026-10-01','PROGRESS-B',88122)`, d.ID)
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
		action.HomeVersion = e.HomeVersion
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
		e, err = repo.Undo(ctx, id, d.ID, e.Version, e.HomeVersion, actor, true)
		if err != nil {
			t.Fatal(err)
		}
	}
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
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: staleEntry.Version, HomeVersion: staleEntry.HomeVersion, FromDate: staleView.FromDate, Revision: staleView.Revision, Action: "advance"}, actor); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("duplicate advance accepted", err)
	}
	if _, err := repo.Undo(ctx, e.UndoID, d.ID, e.Version, e.HomeVersion, "progress-user-two", true); !errors.Is(err, ErrNotFound) {
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
	if e.Status != "" || e.CurrentLoad != "" || e.Destination != "" || finished.Loads.Current != nil || len(finished.Loads.Next) != 0 || len(view().Next) != 0 {
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
	if _, err := repo.ChangeLoads(ctx, BoardLoadAction{DriverID: d.ID, Version: e.Version, HomeVersion: e.HomeVersion, FromDate: v.FromDate, Revision: v.Revision, Action: "advance"}, actor); !errors.Is(err, ErrBoardLoadSelection) {
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
	if _, err := repo.Undo(ctx, mine, d.ID, e.Version, e.HomeVersion, actor, true); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("personal undo overwrote newer user edit", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loads WHERE id IN(88121,88122) AND status='Dispatched'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("progress changed upstream load statuses", err)
	}
}
