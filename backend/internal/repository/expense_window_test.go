package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs against both fresh and migrated schemas as the application role.
func testExpenseCollectionWindow(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	for _, route := range []string{"driver", "investor", "owner-driver"} {
		t.Run("expense-window-"+route, func(t *testing.T) {
			id := func(query string, args ...any) string {
				t.Helper()
				var value string
				if err := admin.QueryRow(ctx, query, args...).Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			week, _ := time.Parse(time.DateOnly, "2026-09-28")
			driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.75) RETURNING id`, "Window "+route)
			exec(`UPDATE driver_dispatcher_assignments SET assigned_at='2026-09-21'::timestamp AT TIME ZONE 'America/New_York' WHERE driver_id=$1`, driver)
			exec(`DELETE FROM driver_escrows WHERE driver_id=$1`, driver)
			actor := id(`INSERT INTO app_users(username,password_hash) VALUES($1,crypt('test-only',gen_salt('bf'))) RETURNING id`, "window-"+route)
			statement := driver
			var owner, truck *string
			coveredBy := "Driver"
			if route != "driver" {
				var ownerDriver *string
				if route == "owner-driver" {
					ownerDriver = &driver
				}
				o := id(`INSERT INTO investors(full_name,driver_id) VALUES($1,$2) RETURNING id`, "Window "+route, ownerDriver)
				tr := id(`INSERT INTO trucks(unit_number,owner_id) VALUES($1,$2) RETURNING id`, "WINDOW-"+route, o)
				owner, truck, coveredBy = &o, &tr, "Truck Owner"
				exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-09-21',88)`, tr, o)
				if route == "owner-driver" {
					exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-21'::timestamp AT TIME ZONE 'America/New_York')`, tr, driver)
				} else {
					statement = tr
				}
			}
			pay := NewDriverPayRepository(pool)
			read := func(w time.Time) DriverPayWeek {
				t.Helper()
				var report DriverPayWeek
				var err error
				if route == "investor" {
					report, err = pay.InvestorPay(ctx, w)
				} else {
					report, err = pay.Get(ctx, w)
				}
				if err != nil {
					t.Fatal(err)
				}
				return report
			}
			card := func(report DriverPayWeek) DriverPayDriver {
				t.Helper()
				for _, d := range report.Drivers {
					if d.ID == statement {
						return d
					}
				}
				t.Fatal("expense-only statement missing", route, report.WeekStart)
				return DriverPayDriver{}
			}
			save := func(edits DriverPayEdits) (DriverPayEdits, error) {
				if route == "investor" {
					return pay.SaveInvestorPay(ctx, edits, actor)
				}
				return pay.Save(ctx, edits)
			}
			category := id(`SELECT id FROM expense_settings WHERE kind='category' AND name='Penalties'`)
			expenses := NewExpenseRepository(pool)
			create := func(offset int) Expense {
				t.Helper()
				e, err := expenses.CreateExpense(ctx, ExpenseInput{CategoryID: category,
					ExpenseType: payTestString(fmt.Sprintf("Window day %d", offset)), ExpenseDate: week.AddDate(0, 0, offset),
					DriverID: &driver, OwnerID: owner, TruckID: truck, CoveredBy: &coveredBy, Amount: "100.25"})
				if err != nil {
					t.Fatal(err)
				}
				return e
			}
			for _, offset := range []int{7, 8, 9} {
				create(offset)
			}
			thursday := create(10)
			initial := card(read(week))
			if len(initial.Edits.ExpenseDeductions) != 3 {
				t.Fatalf("Monday–Wednesday must appear without loads: %+v", initial.Edits)
			}
			if route != "investor" {
				history, err := pay.History(ctx, driver, Pagination{PageSize: 100})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, row := range history.Items {
					found = found || row.WeekStart == week.Format(time.DateOnly) && len(row.Driver.Edits.ExpenseDeductions) == 3
				}
				if !found {
					t.Fatal("history omitted the earlier eligible statement")
				}
			}
			for _, offset := range []int{-1, 0, 6} {
				create(offset)
			}
			first := card(read(week)).Edits
			if len(first.ExpenseDeductions) != 6 {
				t.Fatalf("older balances and Monday–Wednesday window: %+v", first)
			}
			var payments int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM expense_payments p JOIN expenses e ON e.id=p.expense_id WHERE e.driver_id=$1`, driver).Scan(&payments); err != nil || payments != 0 {
				t.Fatal("reads collected payments", payments, err)
			}
			forged := first
			forged.ExpenseDeductions = []ExpenseDeduction{{ExpenseID: thursday.ID, Version: 1, Amount: "100.25", Apply: true}}
			if _, err := save(forged); err == nil {
				t.Fatal("Thursday collection accepted before its eligible week")
			}
			stale := card(read(week.AddDate(0, 0, 7))).Edits
			for i := range first.ExpenseDeductions {
				x := &first.ExpenseDeductions[i]
				x.Apply = true
				if x.ExpenseDate == "2026-10-06" {
					x.Amount = "30.10"
				} else if x.ExpenseDate == "2026-10-07" {
					x.Amount = "0"
				}
			}
			if _, err := save(first); err != nil {
				t.Fatal(err)
			}
			for i := range stale.ExpenseDeductions {
				stale.ExpenseDeductions[i].Apply = true
			}
			if _, err := save(stale); !errors.Is(err, ErrDriverPayConflict) {
				t.Fatal("stale overlapping collection was not rejected", err)
			}
			next := card(read(week.AddDate(0, 0, 7))).Edits
			if len(next.ExpenseDeductions) != 3 {
				t.Fatalf("paid expenses repeated: %+v", next)
			}
			for i, want := range []string{"70.15", "100.25", "100.25"} {
				if next.ExpenseDeductions[i].Amount != want {
					t.Fatalf("partial/zero/new balance: %+v", next)
				}
			}
			settle := pay.Settle
			if route == "investor" {
				settle = pay.SettleInvestor
			}
			report := read(week)
			frozen, err := settle(ctx, week, statement, report.Revision, actor, "", false)
			if err != nil {
				t.Fatal(err)
			}
			if len(card(frozen).Edits.ExpenseDeductions) != 6 {
				t.Fatal("finalization lost the extended window")
			}
			create(9) // Late source changes must not rewrite a finalized statement.
			if len(card(read(week)).Edits.ExpenseDeductions) != 6 {
				t.Fatal("finalized snapshot changed")
			}
			reopened, err := settle(ctx, week, statement, read(week).Revision, actor, "Review late expense", true)
			if err != nil {
				t.Fatal(err)
			}
			if len(card(reopened).Edits.ExpenseDeductions) != 7 || len(card(read(week.AddDate(0, 0, 7))).Edits.ExpenseDeductions) != 4 {
				t.Fatal("reopening did not preserve payments and expose the new expense")
			}
		})
	}
}
