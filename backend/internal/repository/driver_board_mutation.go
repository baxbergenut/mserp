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
