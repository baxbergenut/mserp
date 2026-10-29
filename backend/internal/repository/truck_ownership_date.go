package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Fleet writes already hold the assignment lock and the shared financial lock.
// Only the latest ownership boundary may be edited; earlier known transfers
// cannot be silently overwritten by a backdated form save.
func setDatedTruckOwner(ctx context.Context, tx pgx.Tx, truck string, input TruckInput) error {
	loc, _ := time.LoadLocation("America/New_York")
	today := time.Now().In(loc).Format(time.DateOnly)
	day := input.OwnershipEffectiveDate
	if day != "" {
		if _, err := time.Parse(time.DateOnly, day); err != nil || day < "1900-01-01" || day > today {
			return chargeInvalid("Ownership effective date must be a valid date on or before today")
		}
	}
	var current, owner, version string
	var start time.Time
	var known bool
	if err := tx.QueryRow(ctx, `SELECT h.id::text,h.owner_id::text,h.assigned_at,h.start_known,md5(row_to_json(h)::text)
 FROM truck_ownership_history h WHERE truck_id=$1 AND unassigned_at IS NULL FOR UPDATE`, truck).Scan(&current, &owner, &start, &known, &version); err != nil {
		return err
	}
	if input.ExpectedOwnershipVersion != "" && input.ExpectedOwnershipVersion != version {
		return chargeInvalid("Truck ownership changed; reload the truck before saving")
	}
	changed := input.OwnerID != nil && *input.OwnerID != owner
	if !changed && (day == "" || (known && day == start.In(loc).Format(time.DateOnly))) {
		if day != "" {
			return correctOwnershipFees(ctx, tx, truck, day, input.StatusActor)
		}
		return nil
	}
	if day == "" {
		day = today
	}
	boundary, _ := time.ParseInLocation(time.DateOnly, day, loc)
	previous := current
	previousOwner := owner
	previousStart, previousKnown := start, known
	if !changed {
		err := tx.QueryRow(ctx, `SELECT id::text,owner_id::text,assigned_at,start_known FROM truck_ownership_history
 WHERE truck_id=$1 AND id<>$2 AND unassigned_at=$3 ORDER BY assigned_at DESC,id LIMIT 1 FOR UPDATE`, truck, current, start).Scan(&previous, &previousOwner, &previousStart, &previousKnown)
		if errors.Is(err, pgx.ErrNoRows) {
			previous = ""
		} else if err != nil {
			return err
		}
	}
	if previous != "" && previousKnown && boundary.Before(previousStart) {
		if day < previousStart.In(loc).Format(time.DateOnly) {
			return chargeInvalid("Ownership date crosses an earlier recorded transfer; correct that history first")
		}
		boundary = previousStart
	}
	from, through := day, today
	if !changed {
		oldDay := start.In(loc).Format(time.DateOnly)
		if oldDay < from {
			from = oldDay
		}
		through = oldDay
		if day > through {
			through = day
		}
		if !known {
			from = "1900-01-01"
		}
	}
	newOwner := owner
	if input.OwnerID != nil {
		newOwner = *input.OwnerID
	}
	var closed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investor_pay_weeks w
 WHERE w.truck_id=$1 AND w.finalized AND w.week_start<=$3::date AND w.week_start+7>$2::date)
 OR EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.finalized
 AND s.week_start<=$3::date AND s.week_start+7>$2::date AND (
 s.driver_id IN (SELECT driver_id FROM investors WHERE id IN($4::uuid,$5::uuid,$6::uuid))
 OR EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=$1 AND a.driver_id=payroll_driver_at(s.driver_id,s.week_start+6)
 AND a.assigned_at < ((s.week_start+7)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at > (s.week_start::timestamp AT TIME ZONE 'America/New_York')))))`, truck, from, through, owner, newOwner, previousOwner).Scan(&closed); err != nil {
		return err
	}
	if closed {
		return chargeInvalid("Reopen affected driver or investor statements before changing ownership history")
	}
	if changed {
		if err := setTruckOwner(ctx, tx, truck, input.OwnerID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id::text FROM truck_ownership_history WHERE truck_id=$1 AND unassigned_at IS NULL`, truck).Scan(&current); err != nil {
			return err
		}
	}
	if previous != "" {
		// Unknown migration baselines are not evidence of an acquisition date.
		if _, err := tx.Exec(ctx, `UPDATE truck_ownership_history SET unassigned_at=$2,
 assigned_at=CASE WHEN start_known THEN assigned_at ELSE least(assigned_at,$2) END WHERE id=$1`, previous, boundary); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE truck_ownership_history SET assigned_at=$2,start_known=true WHERE id=$1`, current, boundary)
	if err != nil {
		return err
	}
	return correctOwnershipFees(ctx, tx, truck, from, input.StatusActor)
}
