package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestInvestorAdjustmentSaveDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const date = "2026-10-05"
		week, _ := chargeWeek(date)
		if _, err := pool.Exec(ctx, `UPDATE driver_escrows SET start_date=$1,amount=2500 WHERE truck_id=$2`, week, projectionTruck); err != nil {
			t.Fatal(err)
		}
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Adjustment editor',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.resourceConcurrency = true
		repo.ConfigurePreparedReads(true)
		repo.ConfigurePreparedFinalization(true)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		page, err := repo.PreparedPayroll(ctx, "investor", week, PayPageQuery{ID: projectionTruck}, true)
		if err != nil || len(page.Drivers) != 1 {
			t.Fatal("investor statement", err)
		}
		card := page.Drivers[0]
		input := card.Edits
		if len(input.ExpenseDeductions) != 1 || input.ExpenseDeductions[0].Source != "escrow" || input.ExpenseDeductions[0].Saved {
			t.Fatal("expected a suggested investor escrow", input.ExpenseDeductions)
		}
		input.RequestID = "18200000-0000-0000-0000-000000008701"
		input.ExpectedStatementRevision = card.StatementRevision
		input.Adjustments = []DriverPayAdjustment{{ID: "18200000-0000-0000-0000-000000008702", Kind: "addition", Name: "Adjustment", Amount: "2500"}}
		other := week.AddDate(0, 0, -7)
		if err = repo.RequestPayrollPreparation(ctx, other, false); err != nil {
			t.Fatal(err)
		}
		// Simulate contention on shared payroll state. The target statement has
		// a valid review token and does not need to update any of these rows.
		hold, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Rollback(ctx)
		if _, err = hold.Exec(ctx, `SELECT week_start FROM payroll_projection_jobs FOR UPDATE;
SELECT table_name FROM payroll_cache_revisions FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		work, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		priority := NewPayrollWorkPriority()
		repo.ConfigurePayrollWorkPriority(priority)
		background, release, err := priority.background(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		measured, metrics := db.WithMetrics(WithPayrollReceipt(work))
		start := time.Now()
		saved, err := repo.SaveInvestorPay(measured, input, actor)
		if err != nil {
			t.Fatal("save while another week is queued", err)
		}
		t.Logf("investor adjustment with pending payroll work: %v", time.Since(start))
		if !errors.Is(context.Cause(background), errPayrollWorkYielded) {
			t.Fatal("active background worker did not yield to the save")
		}
		if err = hold.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if saved.CommittedStatement == nil || saved.CommittedStatement.NetPayCents() != card.NetPayCents()+250000 ||
			len(saved.AffectedStatements) != 1 || saved.AffectedStatements[0].ID != card.ID ||
			metrics.Events()["payroll_calculation"] != 0 || metrics.Events()["source_validation_write_queries"] != 0 {
			t.Fatal("expected a single statement receipt with the adjustment", saved.AffectedStatements, metrics.Events())
		}
		var payments int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_escrow_payments`).Scan(&payments); err != nil || payments != 0 {
			t.Fatal("adjustment posted escrow", payments, err)
		}
		retry, err := repo.SaveInvestorPay(WithPayrollReceipt(ctx), input, actor)
		if err != nil || retry.Version != saved.Version || retry.StatementRevision != saved.StatementRevision {
			t.Fatal("receipt replay", err)
		}
		input.RequestID = "18200000-0000-0000-0000-000000008703"
		input.Adjustments[0].Amount = "3000"
		if _, err = repo.SaveInvestorPay(WithPayrollReceipt(ctx), input, actor); !errors.Is(err, ErrDriverPayConflict) {
			t.Fatal("stale edit to same statement", err)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		final, err := repo.SettleInvestor(WithPayrollReceipt(ctx), week, projectionTruck, saved.InputRevision, actor, "", false, true,
			PayrollSettlementCommand{RequestID: "18200000-0000-0000-0000-000000008704", ExpectedStatementRevision: saved.StatementRevision})
		if err != nil || len(final.Drivers) != 1 || final.Drivers[0].NetPayCents() != card.NetPayCents()+250000 {
			t.Fatal("finalized adjustment", err)
		}
		var collected string
		if err = pool.QueryRow(ctx, `SELECT amount::text FROM driver_escrow_payments WHERE week_start=$1`, week).Scan(&collected); err != nil || collected != "2500.00" {
			t.Fatal("finalized escrow", collected, err)
		}
	})
}
