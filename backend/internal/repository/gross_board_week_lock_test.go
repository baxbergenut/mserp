package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGrossBoardWeekLockDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewGrossBoardRepository(pool)
		week := time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC)
		legacyWeek := week.AddDate(0, 0, -7)
		if _, err := pool.Exec(ctx, `INSERT INTO gross_board_week_locks(week_start, locked, version) VALUES ($1, true, 1)`, legacyWeek); err != nil {
			t.Fatal(err)
		}
		legacy, err := repo.SetWeekLock(ctx, legacyWeek, true, "Test Admin")
		if err != nil || !legacy.Locked || legacy.LockedBy != "" || legacy.Version != 1 {
			t.Fatal("existing lock stays unattributed", legacy, err)
		}
		locked, err := repo.SetWeekLock(ctx, week, true, "Test Admin")
		if err != nil || !locked.Locked || locked.Version != 1 || locked.LockedBy != "Test Admin" {
			t.Fatal("lock week", locked, err)
		}
		retry, err := repo.SetWeekLock(ctx, week, true, "Other Admin")
		if err != nil || retry != locked {
			t.Fatal("same-state retry", retry, err)
		}
		board, err := repo.Get(ctx, week)
		if err != nil || board.WeekLock == nil || *board.WeekLock != locked || len(board.Entries) == 0 {
			t.Fatal("read locked week", board.WeekLock, err)
		}
		if len(board.Drivers) != 1 || board.Drivers[0].DriverType != "M-O" {
			t.Fatal("current driver type for a mileage driver on an investor truck", board.Drivers)
		}
		edit := GrossBoardEntry{DriverID: projectionDriver, Date: "2026-10-11", LoadNumber: "WEEK-LOCK-EDIT"}
		if _, err = repo.SaveChanges(ctx, []GrossBoardEntry{edit}, ""); !errors.Is(err, ErrGrossBoardWeekLocked) {
			t.Fatal("load edit", err)
		}
		if _, err = repo.SavePartialChanges(ctx, []GrossBoardEntry{edit}, ""); !errors.Is(err, ErrGrossBoardWeekLocked) {
			t.Fatal("partial load edit", err)
		}
		if _, err = repo.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{{DriverID: projectionDriver, Date: edit.Date, Kind: "note", Text: "Locked status"}}, ""); !errors.Is(err, ErrGrossBoardWeekLocked) {
			t.Fatal("planning text edit", err)
		}
		if _, _, err = repo.ResetRateBalance(ctx, week, projectionDriver, "100", ""); !errors.Is(err, ErrGrossBoardWeekLocked) {
			t.Fatal("rate balance reset", err)
		}
		companion := edit
		companion.Date = "2026-10-12"
		companion.SplitReview = &LoadSplitReview{Entries: []SplitReviewEntry{{Entry: edit}}}
		if _, err = repo.SavePartialChanges(ctx, []GrossBoardEntry{companion}, ""); !errors.Is(err, ErrGrossBoardWeekLocked) {
			t.Fatal("split companion in locked week", err)
		}
		companion.SplitReview = nil
		companion.LoadNumber = "OTHER-WEEK-EDIT"
		if _, err = repo.SaveChanges(ctx, []GrossBoardEntry{companion}, ""); err != nil {
			t.Fatal("edit in another week", err)
		}
		unlocked, err := repo.SetWeekLock(ctx, week, false, "Other Admin")
		if err != nil || unlocked.Locked || unlocked.Version != 2 || unlocked.LockedBy != "" {
			t.Fatal("unlock week", unlocked, err)
		}
		if _, err = repo.SaveChanges(ctx, []GrossBoardEntry{edit}, ""); err != nil {
			t.Fatal("edit after unlocking", err)
		}
	})
}
