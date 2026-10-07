package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Fleet forms change the current assignment, with an explicit accounting Monday.
// Older clients omit the week and retain a safe current-week default.
func setAssignmentWeek(ctx context.Context, tx pgx.Tx, week string) error {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return err
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	monday := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	if week == "" {
		week = monday.Format(time.DateOnly)
	}
	date, err := time.ParseInLocation(time.DateOnly, week, loc)
	if err != nil || date.Weekday() != time.Monday {
		return chargeInvalid("Assignment start must be a Monday")
	}
	// Finalization takes the exclusive counterpart before reading its snapshot.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`); err != nil {
		return err
	}
	// Serialize fleet form changes before acquiring payroll/driver row locks.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(734912086)`); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT set_config('mserp.assignment_week',$1,true)`, week)
	return err
}

func prepareTruckAssignmentBoundary(ctx context.Context, tx pgx.Tx, column, id string) error {
	var week string
	if err := tx.QueryRow(ctx, `SELECT coalesce(current_setting('mserp.assignment_week',true),'')`).Scan(&week); err != nil {
		return err
	}
	if week == "" {
		return nil
	} // Upstream imports retain their observed timestamps.
	// Protect the selected driver even when they have no truck history, plus
	// every driver/truck whose period is being shortened (including displacement).
	if column == "driver_id" {
		if _, err := tx.Exec(ctx, `SELECT assert_assignment_payroll_open($1::uuid,NULL,$2::date)`, id, week); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `SELECT assert_assignment_payroll_open(NULL,$1::uuid,$2::date)`, id, week); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `SELECT assert_assignment_payroll_open(driver_id,truck_id,$2::date)
 FROM truck_driver_assignments WHERE `+column+`=$1
 AND (unassigned_at IS NULL OR unassigned_at > $2::date::timestamp AT TIME ZONE 'America/New_York')`, id, week); err != nil {
		return err
	}
	// Superseded edits retain zero-length history records. Their boundaries
	// must move together so truck cost attribution never sees overlapping periods.
	_, err := tx.Exec(ctx, `UPDATE truck_driver_assignments SET
 assigned_at=least(assigned_at,$2::date::timestamp AT TIME ZONE 'America/New_York'),
 unassigned_at=CASE WHEN unassigned_at IS NULL THEN NULL ELSE least(unassigned_at,$2::date::timestamp AT TIME ZONE 'America/New_York') END
 WHERE `+column+`=$1 AND (assigned_at >= $2::date::timestamp AT TIME ZONE 'America/New_York'
 OR unassigned_at >= $2::date::timestamp AT TIME ZONE 'America/New_York')`, id, week)
	return err
}

// Setup/reactivation controls the unsaved roster. Saved historical work remains
// visible separately; legacy drivers with unknown starts keep their old coverage.
const weeklyDriverStartedSQL = `(d.roster_start_week IS NULL OR d.roster_start_week < $1::date+7)`

// Weekly boards use the assignment at the end of the selected New York week.
// An unknown migration start is a baseline; a known start is never extrapolated.
const weeklyAssignmentJoins = `
 LEFT JOIN LATERAL (
  SELECT h.dispatcher_id,h.dispatcher_name FROM driver_dispatcher_assignments h
  WHERE h.driver_id=d.id
   AND (NOT h.start_known OR h.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
   AND (h.unassigned_at IS NULL OR h.unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
  ORDER BY h.assigned_at DESC LIMIT 1
 ) historical_dispatcher ON true
 LEFT JOIN dispatchers dp ON dp.id=historical_dispatcher.dispatcher_id
 LEFT JOIN LATERAL (
  SELECT h.truck_id FROM truck_driver_assignments h WHERE h.driver_id=d.id
   AND h.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York')
   AND (h.unassigned_at IS NULL OR h.unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
  ORDER BY h.assigned_at DESC LIMIT 1
 ) a ON true
 LEFT JOIN trucks t ON t.id=a.truck_id
`
