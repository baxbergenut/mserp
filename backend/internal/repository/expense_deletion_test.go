package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpenseDraftDeletionDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		id := func(q string, args ...any) string {
			t.Helper()
			var value string
			if err := pool.QueryRow(ctx, q, args...).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		repo := NewExpenseRepository(pool)
		category := id(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`)
		date, _ := time.Parse(time.DateOnly, "2026-10-11")
		for _, kind := range []string{"driver", "reimburse", "investor", "team", "team reimburse"} {
			t.Run(kind, func(t *testing.T) {
				driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.75) RETURNING id::text`, "Deletion "+kind)
				main := driver
				if kind == "team" || kind == "team reimburse" {
					main = id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.75) RETURNING id::text`, "Main "+kind)
					exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,'2026-10-05')`, main, driver)
				}
				input := ExpenseInput{CategoryID: category, ExpenseDate: date, DriverID: &driver, Amount: "10.15", ExpenseType: payTestString("Gorilla tape"), CoveredBy: payTestString("Driver")}
				var owner, truck string
				if kind == "investor" {
					owner = id(`INSERT INTO investors(full_name) VALUES('Deletion owner') RETURNING id::text`)
					truck = id(`INSERT INTO trucks(unit_number,owner_id) VALUES('DELETE-060',$1) RETURNING id::text`, owner)
					input.TruckID, input.OwnerID, input.CoveredBy = &truck, &owner, payTestString("Truck owner")
				}
				reimburse := kind == "reimburse" || kind == "team reimburse"
				if reimburse {
					input.PaymentType, input.CoveredBy = payTestString("Reimburse"), payTestString("Company")
				}
				expense, err := repo.CreateExpense(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				duplicate, err := repo.CreateExpense(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				table := "expense_payments"
				if reimburse {
					table = "expense_reimbursement_payments"
				}
				for _, expenseID := range []string{expense.ID, duplicate.ID} {
					if kind == "investor" {
						exec(`INSERT INTO expense_payments(expense_id,week_start,amount,investor_truck_id) VALUES($1,'2026-10-19',10.15,$2)`, expenseID, truck)
					} else {
						exec(`INSERT INTO `+table+`(expense_id,week_start,amount) VALUES($1,'2026-10-19',10.15)`, expenseID)
					}
				}
				// An empty legacy snapshot still freezes its linked ledger allocations.
				if kind == "investor" {
					exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits,report,finalized) VALUES($1,$2,'2026-10-19','{}','{}',true)`, truck, owner)
				} else {
					exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19','{}')`, main)
				}
				if err = repo.DeleteExpense(ctx, duplicate.ID); !errors.Is(err, ErrExpenseFinalized) {
					t.Fatal("finalized statement must prevent deletion", err)
				}
				if got := id(`SELECT amount::text FROM `+table+` WHERE expense_id=$1`, duplicate.ID); got != "10.15" {
					t.Fatal("failed deletion changed the saved allocation", got)
				}
				if kind == "investor" {
					exec(`UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, truck)
				} else {
					exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, main)
				}
				if err = repo.DeleteExpense(ctx, duplicate.ID); err != nil {
					t.Fatal("open statement must allow deletion", err)
				}
				if _, err = repo.GetExpense(ctx, duplicate.ID); !errors.Is(err, ErrNotFound) {
					t.Fatal("deleted expense remains", err)
				}
				if got := id(`SELECT count(*)::text FROM `+table+` WHERE expense_id=$1`, duplicate.ID); got != "0" {
					t.Fatal("deleted expense allocation remains")
				}
				if got := id(`SELECT amount::text FROM `+table+` WHERE expense_id=$1`, expense.ID); got != "10.15" {
					t.Fatal("deletion changed the retained expense", got)
				}
				if reimburse && kind == "reimburse" {
					pay := NewDriverPayRepository(pool)
					week, _ := time.Parse(time.DateOnly, "2026-10-19")
					report, err := pay.GetDriverWeek(ctx, week, driver)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, d := range report.Drivers {
						if d.ID != driver {
							continue
						}
						found = true
						if len(d.Edits.ExpenseDeductions) != 1 || d.Edits.ExpenseDeductions[0].ExpenseID != expense.ID {
							t.Fatal("deleted reimbursement still appears in payroll", d.Edits)
						}
					}
					if !found {
						t.Fatal("driver statement missing after expense deletion")
					}
					_, err = pay.Save(ctx, DriverPayEdits{DriverID: driver, WeekStart: "2026-10-19", ExpenseDeductions: []ExpenseDeduction{{ExpenseID: duplicate.ID, Amount: "10.15", Apply: true}}})
					if !errors.Is(err, ErrDriverPayConflict) {
						t.Fatal("stale payroll save recreated deleted reimbursement", err)
					}
				}
			})
		}
		// Frozen snapshots protect legacy expenses even without allocation rows.
		for _, legacy := range []string{"deduction", "auto credit"} {
			driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.75) RETURNING id::text`, "Legacy deletion "+legacy)
			expense := id(`INSERT INTO expenses(category_id,category,expense_date,amount,driver_id,covered_by) SELECT id,name,'2026-10-11',10.15,$1,'Company' FROM expense_settings WHERE id=$2 RETURNING id::text`, driver, category)
			if legacy == "deduction" {
				exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19',jsonb_build_object('edits',jsonb_build_object('expenseDeductions',jsonb_build_array(jsonb_build_object('expenseId',$2::text)))))`, driver, expense)
			} else {
				exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19',jsonb_build_object('autoCharges',jsonb_build_array(jsonb_build_object('source','expense-reimburse:'||$2::text))))`, driver, expense)
			}
			if err := repo.DeleteExpense(ctx, expense); !errors.Is(err, ErrExpenseFinalized) {
				t.Fatal("legacy frozen snapshot allowed deletion", legacy, err)
			}
		}
		// A retained external receipt must roll back removal of draft allocations.
		driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Receipt deletion','receipt deletion','cpm',0.75) RETURNING id::text`)
		expense := id(`INSERT INTO expenses(category_id,category,expense_date,amount,driver_id,covered_by) SELECT id,name,'2026-10-11',20,$1,'Driver' FROM expense_settings WHERE id=$2 RETURNING id::text`, driver, category)
		exec(`INSERT INTO expense_external_settlements(id,expense_id,amount,settled_on,reference,actor_id) VALUES(gen_random_uuid(),$1,5,'2026-10-19','Receipt test',$2)`, expense, actor)
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-19',10)`, expense)
		if err := repo.DeleteExpense(ctx, expense); err == nil {
			t.Fatal("external receipt allowed deletion")
		}
		if got := id(`SELECT amount::text FROM expense_payments WHERE expense_id=$1`, expense); got != "10.00" {
			t.Fatal("failed deletion lost its draft allocation", got)
		}
	})
}
