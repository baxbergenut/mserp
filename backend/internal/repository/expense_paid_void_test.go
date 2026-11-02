package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPaidMaintenanceDeletionDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		repo := NewExpenseRepository(pool)
		maintenance := NewMaintenanceRepository(pool)
		id := func(q string, args ...any) string {
			t.Helper()
			var s string
			if err := pool.QueryRow(ctx, q, args...).Scan(&s); err != nil {
				t.Fatal(err)
			}
			return s
		}
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		category, err := maintenance.CategoryID(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"driver", "investor", "reimburse", "legacy snapshot"} {
			t.Run(kind, func(t *testing.T) {
				driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.75) RETURNING id::text`, "Paid void "+kind)
				owner := id(`INSERT INTO investors(full_name) VALUES($1) RETURNING id::text`, "Void owner "+kind)
				truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES($1,$2) RETURNING id::text`, "PAID-VOID-"+kind, owner)
				in := ExpenseInput{CategoryID: category, TruckID: &truck, DriverID: &driver, ExpenseDate: time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC), Amount: "100", CoveredBy: payTestString("Driver"), ExpenseType: payTestString("Road service")}
				if kind == "investor" {
					in.OwnerID = &owner
					in.CoveredBy = payTestString("Truck owner")
				}
				if kind == "reimburse" {
					in.PaymentType = payTestString("Reimburse")
					in.CoveredBy = payTestString("Company")
				}
				e, err := repo.CreateExpense(ctx, in)
				if err != nil {
					t.Fatal(err)
				}
				exec(`UPDATE expenses SET accounting_verified=true WHERE id=$1`, e.ID)
				var record string
				var version int
				if err := pool.QueryRow(ctx, `SELECT id::text,version FROM maintenance_records WHERE expense_id=$1`, e.ID).Scan(&record, &version); err != nil {
					t.Fatal(err)
				}
				table := "expense_payments"
				if kind == "reimburse" {
					table = "expense_reimbursement_payments"
				}
				if kind == "investor" {
					exec(`INSERT INTO expense_payments(expense_id,week_start,amount,investor_truck_id) VALUES($1,'2026-10-19',100,$2)`, e.ID, truck)
					exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits,report,finalized) VALUES($1,$2,'2026-10-19','{}','{}',true)`, truck, owner)
				} else {
					if kind != "legacy snapshot" {
						exec(`INSERT INTO `+table+`(expense_id,week_start,amount) VALUES($1,'2026-10-19',100)`, e.ID)
					}
					exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19',jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',$2::text)))))`, driver, e.ID)
				}
				// Both HTTP entry points must reject the same frozen invoice.
				for _, remove := range []func() error{func() error { return repo.DeleteExpense(ctx, e.ID, "Tester") }, func() error { return maintenance.Void(ctx, record, version, "Duplicate", "Tester") }} {
					if err := remove(); !errors.Is(err, ErrExpenseFinalized) {
						t.Fatal("frozen maintenance invoice allowed deletion", err)
					}
					if got := id(`SELECT status FROM maintenance_records WHERE id=$1`, record); got == "voided" {
						t.Fatal("failed void was not rolled back")
					}
				}
				if kind == "investor" {
					exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, truck)
				} else {
					exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver)
				}
				if err := repo.DeleteExpense(ctx, e.ID, "Tester"); err != nil {
					t.Fatal("open maintenance invoice rejected", err)
				}
				if got := id(`SELECT count(*)::text FROM `+table+` WHERE expense_id=$1`, e.ID); got != "0" {
					t.Fatal("draft payment retained")
				}
				if got := id(`SELECT voided_expense->>'id' FROM maintenance_records WHERE id=$1 AND status='voided' AND expense_id IS NULL`, record); got != e.ID {
					t.Fatal("void lost invoice")
				}
			})
		}
	})
}
