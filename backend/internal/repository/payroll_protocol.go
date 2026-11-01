package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Deployment uses this marker before restarting a release against a switched
// database. It is separate from calculator/display versions.
const PayrollProtocolCapability = "payroll-resources-v1"

type PayrollProtocolStatus struct {
	Mode       string `json:"mode"`
	Epoch      int64  `json:"epoch"`
	Capture    bool   `json:"capture"`
	Capability string `json:"capability"`
}

func (r *DriverPayRepository) PayrollProtocolStatus(ctx context.Context) (PayrollProtocolStatus, error) {
	status := PayrollProtocolStatus{Capability: PayrollProtocolCapability}
	err := r.pool.QueryRow(ctx, `SELECT protocol_mode,protocol_epoch,capture_enabled FROM payroll_projection_control WHERE singleton`).Scan(&status.Mode, &status.Epoch, &status.Capture)
	return status, err
}

// The deployment helper must stop every application writer before invoking this
// operation, in either direction. The exclusive gate plus canonical table locks
// drain in-flight direct SQL too. Revision updates invalidate snapshots pinned
// before the transition, including repeatable-read writers from global mode.
// No canonical financial row, allocation, frozen report or receipt is changed.
func (r *DriverPayRepository) SwitchPayrollProtocol(ctx context.Context, target string, expectedEpoch int64) (PayrollProtocolStatus, error) {
	var result PayrollProtocolStatus
	if target != "global" && target != "dual" && target != "resources" {
		return result, chargeInvalid("Invalid payroll lock protocol")
	}
	if expectedEpoch < 1 {
		return result, chargeInvalid("The reviewed protocol epoch is required")
	}
	// The operator entry always takes the exclusive gate, even if this repository
	// also serves resource commands. Never attempt an in-transaction upgrade.
	operator := NewDriverPayRepository(r.pool)
	tx, closeWrite, err := operator.beginPayrollWrite(ctx)
	if err != nil {
		return result, err
	}
	defer closeWrite()
	if err = payrollProjectionCoverage(ctx, tx); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT table_name FROM payroll_projection_sources WHERE classification IN ('financial','display') ORDER BY table_name`)
	if err != nil {
		return result, err
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return result, err
		}
		tables = append(tables, pgx.Identifier{name}.Sanitize())
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(tables) == 0 {
		return result, ErrPayrollProjectionCoverage
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE `+strings.Join(tables, ",")+` IN ACCESS EXCLUSIVE MODE`); err != nil {
		return result, err
	}
	result.Capability = PayrollProtocolCapability
	if err = tx.QueryRow(ctx, `SELECT protocol_mode,protocol_epoch,capture_enabled FROM payroll_projection_control WHERE singleton FOR UPDATE`).Scan(&result.Mode, &result.Epoch, &result.Capture); err != nil {
		return result, err
	}
	if result.Epoch != expectedEpoch {
		return result, ErrPayrollResourceChanged
	}
	if target != "global" && !result.Capture {
		return result, fmt.Errorf("payroll capture must be enabled before switching protocol")
	}
	if result.Mode == target {
		return result, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO payroll_protocol_events(protocol_epoch,previous_mode,protocol_mode,capability)
 VALUES($1,$2,$3,$4)`, result.Epoch+1, result.Mode, target, PayrollProtocolCapability); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE payroll_cache_revisions SET revision=revision+1 WHERE NOT table_name=ANY($1::text[])`, payrollUnrelatedTables); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode=$1,protocol_epoch=protocol_epoch+1 WHERE singleton;
 UPDATE payroll_projection_jobs SET requested_generation=requested_generation+1,impact_roots=NULL,lease_token=NULL,lease_until=NULL,
 requested_at=clock_timestamp(),available_at=clock_timestamp(),last_error='',attempts=0;
 SELECT pg_notify('payroll_projection_work','')`, pgx.QueryExecModeSimpleProtocol, target); err != nil {
		return result, err
	}
	result.Mode = target
	result.Epoch++
	return result, tx.Commit(ctx)
}

// Check before any startup worker or HTTP listener. An environment toggle is
// never itself permission to switch a live database's lock protocol.
func (r *DriverPayRepository) ConfigurePayrollProtocol(ctx context.Context, resources bool) error {
	if err := r.loadPayrollFirstWeek(ctx); err != nil {
		return err
	}
	status, err := r.PayrollProtocolStatus(ctx)
	if err != nil {
		return err
	}
	if (status.Mode == "resources") != resources {
		return fmt.Errorf("payroll protocol configuration differs from database; use the quiesced protocol switch")
	}
	r.resourceLockAudit = status.Mode == "dual"
	r.resourceConcurrency = resources
	return nil
}
