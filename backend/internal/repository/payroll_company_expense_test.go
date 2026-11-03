package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollCompanyExpenseDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); err != nil {
			t.Fatal(err)
		}
		var category string
		if err := pool.QueryRow(ctx, `SELECT id FROM expense_settings WHERE kind='category' AND name='Other'`).Scan(&category); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE expense_sheet_export_config SET enabled=true`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if worked, err := pay.PrepareNextPayrollWeek(ctx); err != nil || !worked {
			t.Fatal(worked, err)
		}
		fence := func() string {
			t.Helper()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			value, err := payrollProjectionFence(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := fence()
		hold, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Rollback(ctx)
		if _, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`); err != nil {
			t.Fatal(err)
		}
		company, label := "Company", "Unlinked legacy name"
		input := ExpenseInput{CategoryID: category, ExpenseDate: week, Amount: "25.00", CoveredBy: &company, DriverName: &label}
		repo := NewExpenseRepository(pool)
		work, cancel := context.WithTimeout(WithPayrollPublication(ctx), 3*time.Second)
		expense, err := repo.CreateExpense(work, input)
		cancel()
		if err != nil || expense.ID == "" {
			t.Fatal("company expense could not save beside payroll", err)
		}
		var exported bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expense_sheet_exports WHERE expense_id=$1)`, expense.ID).Scan(&exported); err != nil || !exported {
			t.Fatal("company expense lost its Sheets export receipt", err)
		}
		if before != fence() {
			t.Fatal("unassigned expense changed payroll inputs")
		}
		if count, err := pay.DispatchPayrollChanges(ctx); err != nil || count != 0 {
			t.Fatal("unassigned expense queued preparation", count, err)
		}
		page, err := pay.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil || page.State != "ready" {
			t.Fatal("unassigned expense invalidated the ready page", page.State, err)
		}
		_ = hold.Rollback(ctx)

		driver := projectionDriver
		input.DriverID = &driver
		marked, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer marked.Rollback(ctx)
		if _, err = marked.Exec(ctx, `SELECT set_config('mserp.payroll.company_expense','on',true)`); err != nil {
			t.Fatal(err)
		}
		_, err = insertExpense(ctx, marked, input)
		var guard *pgconn.PgError
		if !errors.As(err, &guard) || guard.ConstraintName != "payroll_company_expense" {
			t.Fatal("linked expense bypassed the insert guard", err)
		}
		_ = marked.Rollback(ctx)
		coveredByDriver := "Driver"
		input.CoveredBy = &coveredByDriver
		if _, err = repo.CreateExpense(ctx, input); err != nil {
			t.Fatal(err)
		}
		if before == fence() {
			t.Fatal("driver expense stopped advancing payroll inputs")
		}
	})
}
