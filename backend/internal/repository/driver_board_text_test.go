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
}
