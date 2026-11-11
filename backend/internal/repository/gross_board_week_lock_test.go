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
		locked, err := repo.SetWeekLock(ctx, week, true)
		if err != nil || !locked.Locked || locked.Version != 1 {
			t.Fatal("lock week", locked, err)
		}
		retry, err := repo.SetWeekLock(ctx, week, true)
		if err != nil || retry != locked {
			t.Fatal("same-state retry", retry, err)
		}
		board, err := repo.Get(ctx, week)
		if err != nil || board.WeekLock == nil || *board.WeekLock != locked || len(board.Entries) == 0 {
			t.Fatal("read locked week", board.WeekLock, err)
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
		unlocked, err := repo.SetWeekLock(ctx, week, false)
		if err != nil || unlocked.Locked || unlocked.Version != 2 {
			t.Fatal("unlock week", unlocked, err)
		}
		if _, err = repo.SaveChanges(ctx, []GrossBoardEntry{edit}, ""); err != nil {
			t.Fatal("edit after unlocking", err)
		}
	})
}
