package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Read-only response evidence from the same transaction as the board write.
type BoardFleetStatus struct {
	AvailabilityStatus   string `json:"availabilityStatus"`
	StatusVersion        int    `json:"statusVersion"`
	TruckID              string `json:"truckId"`
	TruckOperatingStatus string `json:"truckOperatingStatus"`
}

func attachBoardFleetStatus(ctx context.Context, tx pgx.Tx, entry *DriverBoardEntry) error {
	entry.FleetStatus = &BoardFleetStatus{}
	s := entry.FleetStatus
	return tx.QueryRow(ctx, `SELECT d.availability_status,d.status_version,coalesce(t.id::text,''),coalesce(fleet_truck_operating_status(t.id),'')
 FROM drivers d LEFT JOIN trucks t ON t.id=fleet_status_driver_truck(d.id) WHERE d.id=$1`, entry.DriverID).Scan(&s.AvailabilityStatus, &s.StatusVersion, &s.TruckID, &s.TruckOperatingStatus)
}

const boardLoadReturnReason = "Load assigned from Status Board"

// Return the existing crew to service as part of an explicit load assignment.
// The fleet lock serializes assignment, profile and work-order changes.
func prepareBoardLoadAssignment(ctx context.Context, tx pgx.Tx, driver, explicitStatus string) (bool, error) {
	var returnDriver, releaseShop, setsRestriction bool
	var truck, previousCommand, previousReason, previousRef string
	err := tx.QueryRow(ctx, `SELECT fleet_status_enabled() AND d.availability_status IN ('home','rest','reset'),
 fleet_status_enabled() AND coalesce(t.status='maintenance' AND NOT EXISTS (
   SELECT 1 FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status<>''
 ),false),coalesce(t.id::text,''),
 coalesce(current_setting('mserp.status_command',true),''),coalesce(current_setting('mserp.status_reason',true),''),
 coalesce(current_setting('mserp.status_source_ref',true),''),
 EXISTS (SELECT 1 FROM fleet_status_rules WHERE board_status=$2
   AND (availability NOT IN ('','active') OR truck_condition<>''))
 FROM drivers d LEFT JOIN trucks t ON t.id=fleet_status_driver_truck(d.id) WHERE d.id=$1`, driver, explicitStatus).
		Scan(&returnDriver, &releaseShop, &truck, &previousCommand, &previousReason, &previousRef, &setsRestriction)
	if err != nil {
		return false, err
	}
	if setsRestriction || (!returnDriver && !releaseShop) {
		return false, requireDispatchableTruck(ctx, tx, driver)
	}
	// Keep the pending board row/version intact while fleet triggers record the
	// canonical transitions. The caller writes the new board status afterward.
	const statusContext = `SELECT set_config('mserp.status_command',$1,true),set_config('mserp.status_reason',$2,true),set_config('mserp.status_source_ref',$3,true)`
	if _, err = tx.Exec(ctx, statusContext, driver, boardLoadReturnReason, driver); err != nil {
		return false, err
	}
	if returnDriver {
		if _, err = tx.Exec(ctx, `UPDATE drivers SET availability_status='active',updated_at=now() WHERE id=$1`, driver); err != nil {
			return false, err
		}
	}
	if releaseShop {
		if _, err = tx.Exec(ctx, `UPDATE trucks SET status='assigned',updated_at=now() WHERE id=$1`, truck); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec(ctx, statusContext, previousCommand, previousReason, previousRef); err != nil {
		return false, err
	}
	return true, requireDispatchableTruck(ctx, tx, driver)
}

// Board saves commit operational data and status history. Canonical fleet
// changes are captured atomically for the payroll worker; a board request never
// runs payroll calculations before acknowledging the save.
func runBoardCommand[T any](run func(bool) (T, error)) (T, error) {
	value, err := run(false)
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.ConstraintName == "board_requires_payroll_source" {
		// The first transaction rolled back completely. Revalidate the same
		// caller versions after taking source -> fleet -> driver locks in order.
		return run(true)
	}
	return value, err
}

func beginBoardMutation(ctx context.Context, pool *pgxpool.Pool, source bool) (pgx.Tx, error) {
	var tx pgx.Tx
	var err error
	if source {
		tx, err = beginPayrollSourceMutation(ctx, pool)
	} else {
		tx, err = pool.Begin(ctx)
	}
	if err != nil {
		return nil, err
	}
	// Take the fleet lock before driver rows, including load actions and undo.
	if _, err = tx.Exec(ctx, `SET LOCAL jit=off; SELECT fleet_status_lock()`); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if !source {
		if _, err = tx.Exec(ctx, `SET LOCAL mserp.payroll.board_operational='on'`); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
	}
	return tx, nil
}
