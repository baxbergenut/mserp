package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// A hire-date correction moves setup boundaries, not subsequent transfers or
// reactivation dates. The caller holds the fleet and financial transaction locks.
func syncDriverHireStart(ctx context.Context, tx pgx.Tx, driver string, hire *time.Time, actor string) error {
	if hire == nil {
		return nil // Clearing an unknown date must not erase recorded history.
	}
	var previous, roster *time.Time
	if err := tx.QueryRow(ctx, `SELECT hire_date,roster_start_week FROM drivers WHERE id=$1 FOR UPDATE`, driver).Scan(&previous, &roster); err != nil {
		return mapNotFound(err)
	}
	date := hire.Format(time.DateOnly)
	if previous != nil && previous.Format(time.DateOnly) == date {
		return nil
	}
	old := previous
	if old == nil {
		old = roster
	}
	if old == nil {
		// A newly confirmed date can fence the roster without inventing the
		// unknown previous assignment/escrow boundaries.
		old = hire
	}
	oldDate := old.Format(time.DateOnly)
	oldWeek := old.AddDate(0, 0, -(int(old.Weekday())+6)%7).Format(time.DateOnly)
	newWeek := date
	// Team membership remains weekly; ordinary crew setup uses the actual day.
	var hasTeam bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM driver_teams WHERE main_driver_id=$1 OR team_driver_id=$1)", driver).Scan(&hasTeam); err != nil {
		return err
	}
	if hasTeam {
		newWeek = hire.AddDate(0, 0, -(int(hire.Weekday())+6)%7).Format(time.DateOnly)
	}
	firstWeek := min(oldWeek, newWeek)
	if _, err := tx.Exec(ctx, `SELECT assert_assignment_payroll_open($1::uuid,NULL,$2::date)`, driver, firstWeek); err != nil {
		return err
	}
	var blocked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM `+grossBoardEntriesSQL+` e WHERE e.driver_id=$1 AND NOT e.deleted
 AND e.service_date >= $2::date AND e.service_date < $3::date
 AND (btrim(e.load_number)<>'' OR e.day_status<>'' OR btrim(coalesce(e.note,''))<>''
 OR e.original_rate IS NOT NULL OR e.driver_rate IS NOT NULL OR e.miles IS NOT NULL)
 UNION ALL SELECT 1 FROM driver_pay_weeks WHERE driver_id=$1 AND week_start >= $2::date AND week_start < $3::date
 UNION ALL SELECT 1 FROM driver_teams WHERE (main_driver_id=$1 OR team_driver_id=$1)
 AND start_week < $3::date AND (end_week IS NULL OR end_week > $2::date)
)`, driver, oldWeek, newWeek).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return chargeInvalid("Hire date would move past recorded work, payroll or a team assignment; review those dates before changing the hire date")
	}
	type boundary struct {
		Kind, ID, Truck string
		End             *time.Time
	}
	rows, err := tx.Query(ctx, `SELECT 'truck',a.id::text,a.truck_id::text,a.unassigned_at
 FROM truck_driver_assignments a WHERE a.driver_id=$1
 AND (a.assigned_at=$2::date::timestamp AT TIME ZONE 'America/New_York' OR a.assigned_at=date_trunc('week',$2::date::timestamp) AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>a.assigned_at)
 AND NOT EXISTS(SELECT 1 FROM truck_driver_assignments h WHERE h.driver_id=a.driver_id
 AND h.assigned_at<a.assigned_at AND (h.unassigned_at IS NULL OR h.unassigned_at>h.assigned_at))
 UNION ALL SELECT 'dispatcher',a.id::text,'',a.unassigned_at
 FROM driver_dispatcher_assignments a WHERE a.driver_id=$1 AND a.start_known
 AND (a.assigned_at=$2::date::timestamp AT TIME ZONE 'America/New_York' OR a.assigned_at=date_trunc('week',$2::date::timestamp) AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at>a.assigned_at)
 AND NOT EXISTS(SELECT 1 FROM driver_dispatcher_assignments h WHERE h.driver_id=a.driver_id
 AND (NOT h.start_known OR h.assigned_at<a.assigned_at)
 AND (h.unassigned_at IS NULL OR h.unassigned_at>h.assigned_at))`, driver, oldDate)
	if err != nil {
		return err
	}
	var boundaries []boundary
	for rows.Next() {
		var b boundary
		if err = rows.Scan(&b.Kind, &b.ID, &b.Truck, &b.End); err != nil {
			rows.Close()
			return err
		}
		boundaries = append(boundaries, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return err
	}
	newBoundary, err := time.ParseInLocation(time.DateOnly, newWeek, loc)
	if err != nil {
		return err
	}
	for _, b := range boundaries {
		if b.End != nil && !newBoundary.Before(*b.End) {
			return chargeInvalid("Hire date would cross a later assignment; review assignment history before changing the hire date")
		}
		if b.Kind == "truck" {
			if _, err = tx.Exec(ctx, `SELECT assert_assignment_payroll_open($1::uuid,$2::uuid,$3::date)`, driver, b.Truck, firstWeek); err != nil {
				return err
			}
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM truck_driver_assignments h WHERE h.id<>$1::uuid
 AND (h.driver_id=$2::uuid OR h.truck_id=$3::uuid) AND (h.unassigned_at IS NULL OR h.unassigned_at>h.assigned_at)
 AND tstzrange(h.assigned_at,h.unassigned_at,'[)') && tstzrange($4::timestamptz,$5::timestamptz,'[)'))`, b.ID, driver, b.Truck, newBoundary, b.End).Scan(&blocked); err != nil {
				return err
			}
			if blocked {
				return chargeInvalid("Hire date would overlap another truck assignment; review assignment history before changing the hire date")
			}
		}
	}
	// Only setup escrow follows the hire date. Independent escrow dates and all
	// amounts remain explicit. Collected/released funds require accounting review.
	var escrowID string
	if oldDate != date {
		if err = tx.QueryRow(ctx, `SELECT coalesce((SELECT e.id::text FROM driver_escrows e JOIN drivers d ON d.id=e.driver_id
 WHERE e.driver_id=$1 AND e.start_date=$2::date AND e.legacy_expense IS NULL AND e.created_at=d.created_at),'')`, driver, oldDate).Scan(&escrowID); err != nil {
			return err
		}
	}
	if escrowID != "" {
		if err = tx.QueryRow(ctx, `SELECT opening_paid<>0
 OR EXISTS(SELECT 1 FROM driver_escrow_payments WHERE escrow_id=e.id)
 OR EXISTS(SELECT 1 FROM driver_escrow_releases WHERE escrow_id=e.id)
 FROM driver_escrows e WHERE id=$1 FOR UPDATE`, escrowID).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return chargeInvalid("Setup escrow already has collections or releases; review its start date in Escrow before changing the hire date")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE drivers SET roster_start_week=$3::date WHERE id=$1 AND (roster_start_week=$2::date OR roster_start_week=date_trunc('week',$2::date::timestamp)::date OR roster_start_week IS NULL)`, driver, oldDate, newWeek); err != nil {
		return err
	}
	for _, b := range boundaries {
		table := "truck_driver_assignments"
		if b.Kind == "dispatcher" {
			table = "driver_dispatcher_assignments"
		}
		if _, err = tx.Exec(ctx, `UPDATE `+table+` SET assigned_at=$2 WHERE id=$1`, b.ID, newBoundary); err != nil {
			return err
		}
	}
	if escrowID != "" {
		if _, err = tx.Exec(ctx, `UPDATE driver_escrows SET start_date=$2::date,balance_version=balance_version+1,updated_at=now() WHERE id=$1`, escrowID, date); err != nil {
			return err
		}
	}
	changes, err := json.Marshal(boundaries)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_audit(actor_id,action,target_id,details)
 VALUES(nullif($1,'')::uuid,'driver_hire_start_changed',$2::uuid,jsonb_build_object(
 'previousHireDate',$3::date,'hireDate',$4::text,'previousWeek',$5::text,'week',$6::text,
 'previousRosterWeek',$7::date,'assignments',$8::jsonb,'escrowId',nullif($9,'')))`, actor, driver, previous, date, oldWeek, newWeek, roster, changes, escrowID)
	return err
}
