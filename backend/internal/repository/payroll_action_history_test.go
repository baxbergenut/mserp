package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These tables also contain unrelated financial/rule events, which must remain.
// Only redundant pay-page action snapshots have been retired.
func assertNoPayrollActionHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM payroll_settlement_events) +
 (SELECT count(*) FROM truck_settlement_events WHERE action IN ('finalized','reopened','pay_edits')) +
 (SELECT count(*) FROM driver_charge_events WHERE action IN ('payroll_finalized','payroll_reopened')) +
 (SELECT count(*) FROM dispatcher_pay_history WHERE action IN ('finalize','reopen','weekly_input'))`).Scan(&count)
	if err != nil || count != 0 {
		t.Fatal("pay-page actions must not save history snapshots", count, err)
	}
}
