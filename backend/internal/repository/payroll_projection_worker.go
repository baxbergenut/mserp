package repository

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// Seed actual source, saved and recent periods, never collection records. A remote
// historical date must not expand into centuries of empty weekly jobs. Other
// weeks remain available through explicit on-demand preparation.
func (r *DriverPayRepository) SeedPayrollPreparation(ctx context.Context) error {
	current := ChargeCurrentWeek()
	// Serialize retirement with source/financial publication. Immutable versions
	// and receipts survive; only automatic empty pointers and future views retire.
	tx, closeWrite, err := r.beginPayrollWrite(ctx)
	if err != nil {
		return err
	}
	defer closeWrite()
	_, err = tx.Exec(ctx, `WITH saved AS (
 SELECT week_start FROM driver_pay_weeks UNION SELECT week_start FROM investor_pay_weeks
 UNION SELECT week_start FROM payroll_settlements
 UNION SELECT week_start FROM expense_payments
 UNION SELECT week_start FROM expense_reimbursement_payments
 UNION SELECT week_start FROM driver_escrow_payments
 UNION SELECT week_start FROM driver_pay_cost_collections
 UNION SELECT week_start FROM pay_source_collections
 ), source_days AS (
 SELECT service_date AS source_day FROM gross_board_entries
 UNION SELECT expense_date FROM expenses WHERE charge_driver_id IS NOT NULL OR owner_id IS NOT NULL OR driver_id IS NOT NULL
 UNION SELECT start_week FROM driver_charge_schedules
 UNION SELECT week_start FROM truck_charge_phases
 UNION SELECT week_start FROM truck_settlement_terms
 UNION SELECT week_start FROM payroll_load_placements
 UNION SELECT start_date FROM driver_escrows
 UNION SELECT purchased_on FROM fuel_transactions
 UNION SELECT posting_date FROM tolls
 UNION SELECT transaction_date FROM weighmytruck_transactions
 ), weeks AS (
 SELECT ($1::date+7*n)::date week_start,CASE WHEN n=0 THEN 90 ELSE 80-abs(n) END priority FROM generate_series(-8,0)n
 UNION ALL SELECT week_start,60 FROM saved
 UNION ALL SELECT date_trunc('week',source_day)::date,10 FROM source_days WHERE source_day IS NOT NULL
 ), wanted AS MATERIALIZED (
 SELECT week_start,max(priority) priority FROM weeks WHERE week_start<=$1::date GROUP BY week_start
 ), retired AS MATERIALIZED (
 SELECT j.week_start FROM payroll_projection_jobs j WHERE j.week_start>$1::date OR (j.priority<=10
 AND (j.lease_until IS NULL OR j.lease_until<clock_timestamp())
 AND (j.interactive_until IS NULL OR j.interactive_until<clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM wanted w WHERE w.week_start=j.week_start)
 AND NOT EXISTS(SELECT 1 FROM payroll_statement_state s WHERE s.week_start=j.week_start))
 UNION SELECT week_start FROM payroll_projection_weeks WHERE week_start>$1::date
 ), retired_statements AS (
 DELETE FROM payroll_statement_state WHERE week_start IN (SELECT week_start FROM retired)
 ), retired_headers AS (
 DELETE FROM payroll_projection_weeks WHERE week_start IN (SELECT week_start FROM retired)
 ), pruned AS (
 DELETE FROM payroll_projection_jobs WHERE week_start IN (SELECT week_start FROM retired)
 ) INSERT INTO payroll_projection_jobs(week_start,priority)
 SELECT week_start,priority FROM wanted ON CONFLICT(week_start) DO NOTHING`, current)
	if err != nil {
		return err
	}
	// Calendar-dependent eligibility and code upgrades are real dependencies even
	// when no source transaction has run. Only enqueue once while already pending.
	_, err = tx.Exec(ctx, `UPDATE payroll_projection_jobs j SET requested_generation=j.requested_generation+1,impact_roots=NULL,
 requested_at=clock_timestamp(),available_at=clock_timestamp()
 FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id
 WHERE j.week_start=w.week_start AND j.requested_generation=j.completed_generation
 AND (g.calendar_week<>$1::date OR g.calculator_version<>$2 OR g.schema_version<>$3)`, current, payrollCalculatorVersion, payrollProjectionSchemaVersion)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Exactly one worker loop per server. PostgreSQL leases also protect against
// overlapping compatible server processes. Notifications are latency hints only.
func (r *DriverPayRepository) RunPayrollShadow(ctx context.Context, logger *slog.Logger) {
	var listener *pgx.Conn
	defer func() {
		if listener != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = listener.Close(closeCtx)
		}
	}()
	var nextSeed, nextHealth time.Time
	for ctx.Err() == nil {
		now := time.Now()
		if !now.Before(nextSeed) {
			if err := r.SeedPayrollPreparation(ctx); err != nil {
				logger.Error("seed payroll shadow", "error", err)
			}
			nextSeed = now.Add(time.Minute)
		}
		if _, err := r.DispatchPayrollChanges(ctx); err != nil && ctx.Err() == nil {
			logger.Error("dispatch payroll shadow", "error", err)
		}
		worked, err := r.PrepareNextPayrollWeek(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("prepare payroll shadow", "error", err)
		}
		if !now.Before(nextHealth) {
			var pending int
			var age float64
			if e := r.pool.QueryRow(ctx, `SELECT count(*),coalesce(extract(epoch FROM clock_timestamp()-min(requested_at)),0)::float8
 FROM payroll_projection_jobs WHERE requested_generation>completed_generation`).Scan(&pending, &age); e == nil && age > 30 {
				logger.Warn("payroll preparation delayed", "pendingWeeks", pending, "oldestSeconds", age)
			}
			nextHealth = now.Add(30 * time.Second)
		}
		if worked && err == nil {
			continue
		}
		if listener == nil {
			connectCtx, cancel := context.WithTimeout(ctx, time.Second)
			listener, err = pgx.ConnectConfig(connectCtx, r.pool.Config().ConnConfig.Copy())
			if err == nil {
				_, err = listener.Exec(connectCtx, `LISTEN payroll_projection_work`)
			}
			cancel()
			if err != nil && listener != nil {
				_ = listener.Close(ctx)
				listener = nil
			}
		}
		if listener != nil {
			waitCtx, cancel := context.WithTimeout(ctx, time.Second)
			_, _ = listener.WaitForNotification(waitCtx)
			cancel()
			if listener.IsClosed() {
				listener = nil
			}
		} else {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}
}
