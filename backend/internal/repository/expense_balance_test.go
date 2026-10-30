package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func TestExpenseBalanceCorrectionsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		scalar := func(q string, args ...any) string {
			t.Helper()
			var s string
			if err := pool.QueryRow(ctx, q, args...).Scan(&s); err != nil {
				t.Fatal(err)
			}
			return s
		}
		driver := scalar(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Balance Driver','balance driver','cpm',0.75) RETURNING id::text`)
		category := scalar(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`)
		repo := NewExpenseRepository(pool)
		date, _ := time.Parse(time.DateOnly, "2026-10-11")
		expense, err := repo.CreateExpense(ctx, ExpenseInput{CategoryID: category, ExpenseDate: date, DriverID: &driver, Amount: "100.00", ExpenseType: payTestString("Balance repair"), CoveredBy: payTestString("Driver")})
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-12',20)`, expense.ID)
		current := func() Expense {
			t.Helper()
			e, err := repo.GetExpense(ctx, expense.ID)
			if err != nil {
				t.Fatal(err)
			}
			return e
		}
		request := func(remaining string) ExpenseBalanceCorrection {
			return ExpenseBalanceCorrection{ID: scalar(`SELECT gen_random_uuid()::text`), Remaining: remaining, Reason: "Reviewed balance", Version: current().BalanceVersion}
		}
		c := request("30.25")
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, c); err != nil {
			t.Fatal(err)
		}
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, c); err != nil {
			t.Fatal("idempotent replay", err)
		}
		got := current()
		if *got.Amount != "100.00" || *got.PaidAmount != "20.00" || *got.RemainingAmount != "30.25" {
			t.Fatalf("original and paid must remain intact: %+v", got)
		}
		c.ID = scalar(`SELECT gen_random_uuid()::text`)
		c.Remaining = "5"
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, c); !errors.Is(err, ErrDriverPayConflict) {
			t.Fatal("stale correction accepted", err)
		}
		for _, amount := range []string{"-1", "0.001", "no", "99999999999"} {
			if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, request(amount)); err == nil {
				t.Fatal("invalid amount accepted", amount)
			}
		}
		if _, err = pool.Exec(ctx, `INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-19',30.26)`, expense.ID); err == nil {
			t.Fatal("correction did not enforce allocation cap")
		}
		c = request("150.00")
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, c); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := expenseDeductions(ctx, tx, driver, "2026-10-19")
		tx.Rollback(ctx)
		if err != nil || len(rows) != 1 || rows[0].Available != "150.00" {
			t.Fatal("payroll did not use corrected balance", rows, err)
		}
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-19',150)`, expense.ID)
		if *current().RemainingAmount != "0.00" {
			t.Fatal("increased balance could not be fully collected")
		}
		history, err := repo.ExpenseBalanceHistory(ctx, expense.ID)
		if err != nil || len(history) != 2 || history[0].Previous != "30.25" || history[0].Remaining != "150.00" {
			t.Fatal("correction audit", history, err)
		}
		if _, err = pool.Exec(ctx, `UPDATE expense_balance_corrections SET reason='erase' WHERE expense_id=$1`, expense.ID); err == nil {
			t.Fatal("immutable audit changed")
		}
		if err = repo.DeleteExpense(ctx, expense.ID); err == nil {
			t.Fatal("retained correction audit deleted")
		}
		exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19','{}')`, driver)
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, request("5")); err == nil {
			t.Fatal("finalized allocation accepted correction")
		}
		exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver)
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, request("5")); err != nil {
			t.Fatal("reopened balance", err)
		}
		// Historical assumed-paid entries can be corrected without losing their baseline.
		legacy, err := repo.CreateExpense(ctx, ExpenseInput{CategoryID: category, ExpenseDate: date, DriverID: &driver, Amount: "50", ExpenseType: payTestString("Historical repair"), CoveredBy: payTestString("Driver")})
		if err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE expenses SET driver_settled=true WHERE id=$1`, legacy.ID)
		expense = legacy
		c = request("12.50")
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, c); err != nil {
			t.Fatal(err)
		}
		got = current()
		if *got.PaidAmount != "50.00" || *got.RemainingAmount != "12.50" {
			t.Fatal("historical baseline lost", got)
		}
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-26',2.50)`, expense.ID)
		got = current()
		if *got.PaidAmount != "52.50" || *got.RemainingAmount != "10.00" {
			t.Fatal("historical payment", got)
		}
		// Header filters execute before pagination and summary totals.
		page, err := repo.ListExpensesPage(ctx, ExpensePageQuery{ChargeDriverID: &driver, VisibleCategoryIDs: []string{category}, AccessibleCategoryIDs: []string{category}, ExpenseType: "historical", PaidMin: "52", RemainingMax: "10", Pagination: Pagination{Page: 1, PageSize: 1}})
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != expense.ID {
			t.Fatal("header filters", page, err)
		}
		page, err = repo.ListExpensesPage(ctx, ExpensePageQuery{ChargeDriverID: &driver, VisibleCategoryIDs: []string{category}, AccessibleCategoryIDs: []string{category}, RemainingMin: "11", Pagination: Pagination{Page: 1, PageSize: 1}})
		if err != nil || page.Total != 0 {
			t.Fatal("empty balance range", page, err)
		}
		// Truck-owner debt uses the same corrected total, including legacy EFS fees.
		owner := scalar(`INSERT INTO investors(full_name) VALUES('Balance owner') RETURNING id::text`)
		truck := scalar(`INSERT INTO trucks(unit_number,owner_id) VALUES('BALANCE-01',$1) RETURNING id::text`, owner)
		expense, err = repo.CreateExpense(ctx, ExpenseInput{CategoryID: category, ExpenseDate: date, TruckID: &truck, OwnerID: &owner, Amount: "100", ExpenseType: payTestString("Owner repair"), CoveredBy: payTestString("Truck Owner"), PaymentType: payTestString("EFS")})
		if err != nil || *expense.RemainingAmount != "103.00" {
			t.Fatal("legacy EFS total", expense, err)
		}
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, request("40")); err != nil {
			t.Fatal(err)
		}
		tx, err = pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rows, err = investorExpenseDeductions(ctx, tx, truck, owner, "2026-10-19", false)
		tx.Rollback(ctx)
		if err != nil || len(rows) != 1 || rows[0].Available != "40.00" {
			t.Fatal("investor corrected balance", rows, err)
		}
		if err = repo.RecordExternalSettlement(ctx, expense.ID, actor, ExternalExpenseSettlement{ID: scalar(`SELECT gen_random_uuid()::text`), Amount: "10", SettledOn: "2026-10-20", Reference: "Outside recovery", Version: current().BalanceVersion}); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount,investor_truck_id) VALUES($1,'2026-10-19',20,$2)`, expense.ID, truck)
		if *current().RemainingAmount != "10.00" {
			t.Fatal("investor and external payments did not share corrected balance")
		}
		exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits,report,finalized) VALUES($1,$2,'2026-10-19','{}','{}',true)`, truck, owner)
		if err = repo.CorrectExpenseBalance(ctx, expense.ID, actor, request("5")); err == nil {
			t.Fatal("finalized investor allocation accepted correction")
		}
	})
}
