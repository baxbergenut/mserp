package repository

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrGrossBoardWeekLocked = errors.New("This Gross Board week is locked")

type GrossBoardWeekLock struct {
	Locked   bool   `json:"locked"`
	Version  int    `json:"version"`
	LockedBy string `json:"lockedBy,omitempty"`
}

type grossBoardWeekLockOverrideKey struct{}

// WithGrossBoardWeekLockOverride is set only for requests whose authenticated
// role can manage the Gross Board. Ordinary write permissions still apply.
func WithGrossBoardWeekLockOverride(ctx context.Context, allowed bool) context.Context {
	return context.WithValue(ctx, grossBoardWeekLockOverrideKey{}, allowed)
}

func readGrossBoardWeekLock(ctx context.Context, tx pgx.Tx, week time.Time) (GrossBoardWeekLock, error) {
	var state GrossBoardWeekLock
	err := tx.QueryRow(ctx, `
 SELECT locked, version, coalesce(CASE WHEN locked AND locked_by_version=version THEN locked_by_name END, '')
 FROM gross_board_week_locks WHERE week_start=$1`, week).
		Scan(&state.Locked, &state.Version, &state.LockedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return state, err
}

func (r *GrossBoardRepository) SetWeekLock(ctx context.Context, week time.Time, locked bool, actorName string) (GrossBoardWeekLock, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return GrossBoardWeekLock{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1163352402, $1::date - date '2000-01-03')`, week); err != nil {
		return GrossBoardWeekLock{}, err
	}
	state, err := readGrossBoardWeekLock(ctx, tx, week)
	if err != nil {
		return state, err
	}
	if state.Locked != locked {
		next := GrossBoardWeekLock{Locked: locked, Version: state.Version + 1}
		if locked {
			next.LockedBy = strings.TrimSpace(actorName)
		}
		_, err = tx.Exec(ctx, `
 INSERT INTO gross_board_week_locks (week_start, locked, version, locked_by_name, locked_by_version)
 VALUES ($1, $2, $3::integer, nullif($4, ''), CASE WHEN $2 THEN $3::integer END)
 ON CONFLICT (week_start) DO UPDATE SET locked=excluded.locked, version=excluded.version,
   locked_by_name=excluded.locked_by_name, locked_by_version=excluded.locked_by_version`,
			week, next.Locked, next.Version, next.LockedBy)
		if err != nil {
			return state, err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_notify('mserp_gross_board','')`); err != nil {
			return state, err
		}
		state = next
	}
	return state, tx.Commit(ctx)
}

func assertGrossBoardWeekEditable(ctx context.Context, tx pgx.Tx, week time.Time) error {
	if allowed, _ := ctx.Value(grossBoardWeekLockOverrideKey{}).(bool); allowed {
		return nil
	}
	// Keep the shared lock until the write commits. Read the state in a separate
	// statement so a writer waiting for a toggle sees that toggle's commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(1163352402, $1::date - date '2000-01-03')`, week); err != nil {
		return err
	}
	state, err := readGrossBoardWeekLock(ctx, tx, week)
	if err == nil && state.Locked {
		return ErrGrossBoardWeekLocked
	}
	return err
}

func assertGrossBoardDaysEditable(ctx context.Context, tx pgx.Tx, days []grossBoardDay) error {
	weeks := make(map[time.Time]struct{})
	for _, day := range days {
		date, err := time.Parse(time.DateOnly, day.date)
		if err != nil {
			return ErrGrossBoardConflict
		}
		sinceMonday := (int(date.Weekday()) + 6) % 7
		weeks[date.AddDate(0, 0, -sinceMonday)] = struct{}{}
	}
	ordered := make([]time.Time, 0, len(weeks))
	for week := range weeks {
		ordered = append(ordered, week)
	}
	slices.SortFunc(ordered, time.Time.Compare)
	for _, week := range ordered {
		if err := assertGrossBoardWeekEditable(ctx, tx, week); err != nil {
			return err
		}
	}
	return nil
}
