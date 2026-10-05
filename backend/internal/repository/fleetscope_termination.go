package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/fleetscope"
)

func (r *FleetRepository) AcceptFleetScopeTermination(ctx context.Context, event fleetscope.Event, hash string) (IntakeResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return IntakeResult{}, err
	}
	defer tx.Rollback(ctx)
	// Shared with hires/completions so delivery order cannot race identity setup.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fleetscope:intake', 0))`); err != nil {
		return IntakeResult{}, err
	}
	result := IntakeResult{Status: "duplicate"}
	var existingHash string
	err = tx.QueryRow(ctx, `SELECT coalesce(termination_id::text,''),body_sha256 FROM fleetscope_webhook_receipts WHERE event_id=$1`, event.EventID).Scan(&result.TerminationID, &existingHash)
	if err == nil {
		if hash != existingHash || result.TerminationID == "" {
			return IntakeResult{}, ErrFleetScopeEventConflict
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IntakeResult{}, err
	}
	err = tx.QueryRow(ctx, `SELECT id FROM fleetscope_driver_terminations WHERE company_id=$1 AND fleetscope_driver_id=$2`, event.CompanyID, event.Driver.ID).Scan(&result.TerminationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return IntakeResult{}, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// Never infer destructive identity changes from names or contact information.
		// The explicit intake completion is the authoritative external identity link.
		if err = setAssignmentWeek(ctx, tx, ""); err != nil {
			return IntakeResult{}, err
		}
		var driverID *string
		var completedAt *time.Time
		err = tx.QueryRow(ctx, `SELECT driver_id,completed_at FROM fleetscope_driver_intake WHERE company_id=$1 AND fleetscope_driver_id=$2 FOR UPDATE`, event.CompanyID, event.Driver.ID).Scan(&driverID, &completedAt)
		foundIntake := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return IntakeResult{}, err
		}
		notes := fmt.Sprintf("FleetScope termination date: %s\nFleetScope driver ID: %s\n", event.TerminationDate, event.Driver.ID)
		if driverID != nil {
			if err = lockChargeDrivers(ctx, tx, []string{*driverID}); err != nil {
				return IntakeResult{}, err
			}
			// Financial exceptions must be reviewed without preventing operational offboarding.
			// A savepoint ensures a rejected pause never leaves partially changed charges.
			pauseTx, pauseErr := tx.Begin(ctx)
			if pauseErr != nil {
				return IntakeResult{}, pauseErr
			}
			pauseErr = pauseDriverCharges(ctx, pauseTx, *driverID, ChargeCurrentWeek(), "")
			if pauseErr != nil {
				if err = pauseTx.Rollback(ctx); err != nil {
					return IntakeResult{}, err
				}
				var validation *ChargeValidationError
				if !errors.As(pauseErr, &validation) {
					return IntakeResult{}, pauseErr
				}
				notes += "ACTION REQUIRED: Charges were not paused. " + validation.Message + ". Review and pause charges in Accounting.\n"
			} else {
				if err = pauseTx.Commit(ctx); err != nil {
					return IntakeResult{}, err
				}
				notes += "Driver charges paused from the current New York week (where applicable). Review the termination date for any earlier settlement corrections.\n"
			}
			if _, err = tx.Exec(ctx, `SELECT set_config('mserp.assignment_source','fleetscope_termination',true)`); err != nil {
				return IntakeResult{}, err
			}
			if _, err = tx.Exec(ctx, `UPDATE drivers SET active=false,dispatcher_id=NULL,updated_at=now() WHERE id=$1`, *driverID); err != nil {
				return IntakeResult{}, err
			}
			if err = setDriverTruck(ctx, tx, *driverID, nil); err != nil {
				return IntakeResult{}, err
			}
			notes += "Driver marked inactive; current truck and dispatcher assignments disconnected.\nMSERP driver ID: " + *driverID + "\n"
		} else if foundIntake && completedAt == nil {
			notes += "Pending driver setup cancelled. No managed driver was linked. Review any existing MSERP record before marking it inactive.\n"
		} else {
			notes += "ACTION REQUIRED: No saved MSERP driver link exists (or the linked driver was deleted). Verify the driver's identity in Drivers, mark the correct record inactive, and review charges. No driver was automatically changed.\n"
		}
		if _, err = tx.Exec(ctx, `UPDATE fleetscope_driver_intake SET terminated_at=$3 WHERE company_id=$1 AND fleetscope_driver_id=$2`, event.CompanyID, event.Driver.ID, event.OccurredAt); err != nil {
			return IntakeResult{}, err
		}
		notes += "\nOffboarding: collect company equipment and documents; disable fuel/toll cards and external system access; review remaining expenses, deductions and final settlement; confirm completion with the team."
		var taskID string
		// Title remains within the existing Tasks contract even for long source names.
		name := []rune(formatPersonName(event.Driver.FullName))
		if len(name) > 190 {
			name = name[:190]
		}
		if err = tx.QueryRow(ctx, `INSERT INTO custom_tasks(title,notes) VALUES($1,$2) RETURNING id`, "Offboard "+string(name), notes).Scan(&taskID); err != nil {
			return IntakeResult{}, err
		}
		if err = tx.QueryRow(ctx, `INSERT INTO fleetscope_driver_terminations(company_id,fleetscope_driver_id,driver_name,termination_date,occurred_at,driver_id,task_id)
 VALUES($1,$2,$3,$4::date,$5,$6,$7) RETURNING id`, event.CompanyID, event.Driver.ID, event.Driver.FullName, event.TerminationDate, event.OccurredAt, driverID, taskID).Scan(&result.TerminationID); err != nil {
			return IntakeResult{}, err
		}
		result.Status = "accepted"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO fleetscope_webhook_receipts(event_id,body_sha256,termination_id) VALUES($1,$2,$3)`, event.EventID, hash, result.TerminationID); err != nil {
		return IntakeResult{}, err
	}
	return result, tx.Commit(ctx)
}
