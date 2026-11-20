package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testConcurrentBoardText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *DriverBoardRepository, fleet *FleetRepository) {
	t.Helper()
	driver, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Cell Edit Driver", PayType: "cpm", PayRate: 0.5, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Save(ctx, []DriverBoardEntry{{DriverID: driver.ID, Status: "READY"}})
	if err != nil {
		t.Fatal(err)
	}
	base := rows[0]
	bases := map[string]DriverBoardEntry{driver.ID: base}
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err = lock.Exec(ctx, `SELECT fleet_status_lock(),pg_advisory_xact_lock(736281940)`); err != nil {
		t.Fatal(err)
	}
	// A concurrent foreign-key check can continue referencing this driver.
	if _, err = lock.Exec(ctx, `SELECT id FROM drivers WHERE id=$1 FOR KEY SHARE`, driver.ID); err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	save := func(e DriverBoardEntry) (DriverBoardEntry, error) {
		result, err := repo.SaveWithTextBase(work, []DriverBoardEntry{e}, bases, "")
		if err != nil {
			return DriverBoardEntry{}, err
		}
		return result[0], nil
	}
	text, trailer := base, base
	text.Notes = "Pickup confirmed"
	trailer.TrailerNumber = "93"
	trailer.TrailerNote = "Reefer"
	start := time.Now()
	first, err := save(text)
	if err != nil || first.UndoID == 0 {
		t.Fatal("notes under held fleet and payroll locks", first.Version, err)
	}
	second, err := save(trailer)
	if err != nil || second.Notes != text.Notes || second.TrailerNote != trailer.TrailerNote || second.Status != base.Status || second.Version != first.Version+1 || second.UndoID == 0 {
		t.Fatal("disjoint cells from same baseline", second, err)
	}
	t.Logf("two text edits committed against one baseline with fleet and payroll locks held in %v", time.Since(start))
	again, err := save(trailer)
	if err != nil || again.Version != second.Version || again.UndoID != 0 {
		t.Fatal("identical retry wrote new history", again, err)
	}
	text.Notes = "A different note"
	if _, err = save(text); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("silent note overwrite", err)
	}
	trailer.TrailerNumber = base.TrailerNumber
	trailer.TrailerNote = "Note for old trailer"
	if _, err = save(trailer); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("crossed trailer identities", err)
	}
	// An already-open client can save ordinary text under the same held locks.
	oldTab := second
	oldTab.Notes = "Legacy tab edit"
	legacy, err := repo.SaveWithTextBase(work, []DriverBoardEntry{oldTab}, nil, "")
	if err != nil || legacy[0].TrailerNumber != second.TrailerNumber || legacy[0].Notes != oldTab.Notes || legacy[0].UndoID == 0 {
		t.Fatal("legacy tab", err)
	}
	if _, err = repo.SaveWithTextBase(work, []DriverBoardEntry{oldTab}, nil, ""); !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("legacy stale version", err)
	}
	second = legacy[0]
	// A dispatch change must still wait behind the held fleet lock.
	mixed := second
	mixed.Status = "ENROUTE"
	mixed.Notes = "Related change"
	wait, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = repo.SaveWithTextBase(wait, []DriverBoardEntry{mixed}, map[string]DriverBoardEntry{driver.ID: second}, "")
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("mixed write bypassed fleet lock", err)
	}
	if err = lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// An actual team configuration writer forces the ordinary versioned path.
	team, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer team.Rollback(ctx)
	if _, err = team.Exec(ctx, `LOCK TABLE driver_teams IN ROW EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	latest := second
	latest.Notes = "After team check"
	result, err := repo.SaveWithTextBase(ctx, []DriverBoardEntry{latest}, map[string]DriverBoardEntry{driver.ID: second}, "")
	if err != nil || result[0].Version != second.Version+1 {
		t.Fatal("membership write fallback", err)
	}
	if err := team.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	testLoadActionAcrossTextEdits(t, ctx, repo, result[0])
}

func testLoadActionAcrossTextEdits(t *testing.T, ctx context.Context, repo *DriverBoardRepository, base DriverBoardEntry) {
	t.Helper()
	view, err := repo.Loads(ctx, base.DriverID, "")
	if err != nil {
		t.Fatal(err)
	}
	add := BoardLoadAction{DriverID: base.DriverID, Version: base.Version, FromDate: view.FromDate, Revision: view.Revision, Action: "add", Number: "DISPATCH-TEXT-RACE"}
	save := func(before, after DriverBoardEntry) DriverBoardEntry {
		t.Helper()
		rows, err := repo.SaveWithTextBase(ctx, []DriverBoardEntry{after}, map[string]DriverBoardEntry{before.DriverID: before}, "")
		if err != nil {
			t.Fatal(err)
		}
		return rows[0]
	}
	change := base
	change.Notes = "Changed while drawer open"
	current := save(base, change)
	change = current
	change.TrailerNote = "Second text save"
	current = save(current, change)

	invalid := add
	invalid.Revision = "outdated plan"
	if _, err := repo.ChangeLoads(ctx, invalid, ""); !errors.Is(err, ErrBoardLoadPlansChanged) {
		t.Fatal("stale plans crossed text edits", err)
	}
	added, err := repo.ChangeLoads(ctx, add, "")
	if err != nil || added.Entry.Notes != current.Notes || added.Entry.TrailerNote != current.TrailerNote ||
		added.Entry.Version <= current.Version || added.Entry.UndoID == 0 {
		t.Fatal("load action lost an intervening text edit or receipt", err, added.Entry)
	}
	if _, err := repo.ChangeLoads(ctx, add, ""); !errors.Is(err, ErrBoardLoadRowChanged) {
		t.Fatal("second application of the command", err)
	}
	// Changing a dispatch field away and back still invalidates the old command.
	stale := BoardLoadAction{DriverID: base.DriverID, Version: added.Entry.Version, FromDate: added.Loads.FromDate, Revision: added.Loads.Revision, Action: "add", Number: "STALE-DISPATCH"}
	change = added.Entry
	change.Destination = "Other destination"
	current = save(added.Entry, change)
	change = current
	change.Destination = added.Entry.Destination
	current = save(current, change)
	change = current
	change.Notes = "After dispatch change"
	save(current, change)
	if _, err := repo.ChangeLoads(ctx, stale, ""); !errors.Is(err, ErrBoardLoadRowChanged) {
		t.Fatal("dispatch field changed away and back", err)
	}
}
