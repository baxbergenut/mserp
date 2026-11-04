package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollLiveDriverExpenseDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		if os.Getenv("MSERP_PAYROLL_SQL_DIAGNOSTIC") == "1" {
			cfg := pool.Config()
			cfg.ConnConfig.Tracer = payrollTestTracer{}
			traced, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer traced.Close()
			pool = traced
		}
		seedPayrollProjectionTest(t, ctx, pool)
		var actor, expense string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Live expense test',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,covered_by,payment_type)
 SELECT id,name,'Live unpaid expense','2026-11-20',100,$1,$1,'Driver','' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id::text`, projectionDriver).Scan(&expense); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(true)
		repo.ConfigurePreparedFinalization(true)
		repo.ConfigurePreparedCommands(true)
		repo.resourceConcurrency = true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		read := func(summary bool) DriverPayWeek {
			t.Helper()
			page, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{Summary: summary, ID: projectionDriver}, true)
			if err != nil || len(page.Drivers) != 1 || page.Preparation.State != "ready" {
				t.Fatalf("live page: %v %+v", err, page)
			}
			return page
		}
		before := read(false)
		measured, metrics := db.WithMetrics(WithPayrollPublication(ctx))
		tx, err := beginPayrollMutation(measured, pool)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(measured, `UPDATE expenses SET amount=150 WHERE id=$1`, expense); err != nil {
			t.Fatal(err)
		}
		if err = commitPayrollMutation(measured, tx); err != nil {
			t.Fatal(err)
		}
		if metrics.Events()["payroll_calculation"] != 0 {
			t.Fatal("expense save rebuilt payroll", metrics.Events())
		}
		after := read(false)
		if before.Drivers[0].StatementRevision != after.Drivers[0].StatementRevision ||
			before.Drivers[0].ExpenseRevision == after.Drivers[0].ExpenseRevision ||
			after.Drivers[0].Edits.ExpenseDeductions[0].Amount != "150.00" {
			t.Fatal("live balance did not update independently", before.Drivers[0], after.Drivers[0])
		}
		summary := read(true)
		if summary.Pagination.Payable != PaginatePay(after, PayPageQuery{}).Pagination.Payable || summary.Drivers[0].ExpenseRevision != after.Drivers[0].ExpenseRevision {
			t.Fatal("summary and detail disagree", summary, after)
		}
		command := PayrollSettlementCommand{RequestID: "19800000-0000-0000-0000-000000000001", ExpectedStatementRevision: before.Drivers[0].StatementRevision, ExpenseRevision: before.Drivers[0].ExpenseRevision}
		if _, err = repo.Settle(WithPayrollReceipt(ctx), week, projectionDriver, before.Revision, actor, "", false, true, command); !errors.Is(err, ErrDriverPayConflict) {
			t.Fatal("stale expense review was accepted", err)
		}
		command.RequestID = "19800000-0000-0000-0000-000000000002"
		command.ExpenseRevision = after.Drivers[0].ExpenseRevision
		finalCtx, finalMetrics := db.WithMetrics(WithPayrollReceipt(ctx))
		trace := &payrollTestQueries{}
		finalCtx = context.WithValue(finalCtx, payrollTestQueriesKey{}, trace)
		if _, err = repo.Settle(finalCtx, week, projectionDriver, after.Revision, actor, "", false, true, command); err != nil {
			t.Fatal("reviewed finalization", err)
		}
		if finalMetrics.Events()["payroll_calculation"] != 0 {
			t.Fatal("expense finalization recalculated payroll", finalMetrics.Events())
		}
		t.Logf("expense edit %d queries, finalization %d queries, zero calculations", metrics.Queries.Load(), finalMetrics.Queries.Load())
		if os.Getenv("MSERP_PAYROLL_SQL_DIAGNOSTIC") == "1" {
			t.Log("phase counts", finalMetrics.Events())
			for i, sql := range trace.SQL {
				line := strings.Join(strings.Fields(sql), " ")
				t.Logf("query %02d %s", i+1, line[:min(len(line), 260)])
			}
		}
		frozen := read(false)
		var investorRevision string
		if err = pool.QueryRow(ctx, `SELECT version_id::text FROM payroll_statement_state WHERE kind='investor' AND week_start=$1 LIMIT 1`, week).Scan(&investorRevision); err != nil {
			t.Fatal(err)
		}
		reopenCtx, reopenMetrics := db.WithMetrics(WithPayrollReceipt(ctx))
		if _, err = repo.Settle(reopenCtx, week, projectionDriver, frozen.Revision, actor, "Review choice", true, false,
			PayrollSettlementCommand{RequestID: "19800000-0000-0000-0000-000000000003", ExpectedStatementRevision: frozen.Drivers[0].StatementRevision}); err != nil {
			t.Fatal("reopen for choice", err)
		}
		var preserved bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_statement_state WHERE kind='investor' AND version_id::text=$1)`, investorRevision).Scan(&preserved); err != nil || !preserved || reopenMetrics.Events()["payroll_calculation"] != 1 {
			t.Fatal("expense reopen rebuilt unrelated statements", preserved, err, reopenMetrics.Events())
		}
		open := read(false)
		edit := open.Drivers[0].Edits
		edit.RequestID, edit.ExpectedStatementRevision = "19800000-0000-0000-0000-000000000004", open.Drivers[0].StatementRevision
		edit.ExpenseDeductions[0].Mode, edit.ExpenseDeductions[0].Amount, edit.ExpenseDeductions[0].Apply = "fixed", "50.00", true
		saveCtx, saveMetrics := db.WithMetrics(ctx)
		saved, err := repo.Save(saveCtx, edit, actor)
		if err != nil || saved.CommittedStatement == nil {
			t.Fatal("save choice", err)
		}
		if saveMetrics.Events()["payroll_calculation"] != 0 || saved.CommittedStatement.ExpenseRevision == open.Drivers[0].ExpenseRevision {
			t.Fatal("choice save recalculated or returned an obsolete review token", saveMetrics.Events())
		}
		// Finalize directly from the save receipt, without another page/detail read.
		retryCtx, retryMetrics := db.WithMetrics(WithPayrollReceipt(ctx))
		retryTrace := &payrollTestQueries{}
		retryCtx = context.WithValue(retryCtx, payrollTestQueriesKey{}, retryTrace)
		if _, err = repo.Settle(retryCtx, week, projectionDriver, saved.InputRevision, actor, "", false, true,
			PayrollSettlementCommand{RequestID: "19800000-0000-0000-0000-000000000005", ExpectedStatementRevision: saved.StatementRevision, ExpenseRevision: saved.CommittedStatement.ExpenseRevision}); err != nil {
			current := read(false).Drivers[0]
			t.Log("receipt", saved.CommittedStatement.ExpenseRevision, "current", current.ExpenseRevision, "phases", retryMetrics.Events())
			if len(retryTrace.SQL) > 0 {
				t.Log("last query", retryTrace.SQL[len(retryTrace.SQL)-1])
			}
			t.Fatal("finalization from choice receipt", err)
		}
		var paid string
		if err = pool.QueryRow(ctx, `SELECT sum(amount)::text FROM expense_payments WHERE expense_id=$1`, expense).Scan(&paid); err != nil || paid != "50.00" {
			t.Fatal("choice receipt collected wrong amount", paid, err)
		}
	})
}
