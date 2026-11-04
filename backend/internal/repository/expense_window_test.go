package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs against both fresh and migrated schemas as the application role.
func testExpenseCollectionWindow(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool, prepared ...bool) {
	preparedSettlement := len(prepared) > 0 && prepared[0]
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
			create(10)
			initial := card(read(week))
			if len(initial.Edits.ExpenseDeductions) != 4 {
				t.Fatalf("All unpaid expenses must appear without loads or a date cutoff: %+v", initial.Edits)
			}
			if route != "investor" {
				history, err := pay.History(ctx, driver, Pagination{PageSize: 100})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, row := range history.Items {
					found = found || row.WeekStart == week.Format(time.DateOnly) && len(row.Driver.Edits.ExpenseDeductions) == 4
				}
				if !found {
					t.Fatal("history omitted the earlier eligible statement")
				}
			}
			for _, offset := range []int{-1, 0, 6} {
				create(offset)
			}
			first := card(read(week)).Edits
			if len(first.ExpenseDeductions) != 7 {
				t.Fatalf("all older and future unpaid balances: %+v", first)
			}
			var payments int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM expense_payments p JOIN expenses e ON e.id=p.expense_id WHERE e.driver_id=$1`, driver).Scan(&payments); err != nil || payments != 0 {
				t.Fatal("reads collected payments", payments, err)
			}
			forged := first
			forged.ExpenseDeductions = []ExpenseDeduction{{ExpenseID: "19800000-0000-0000-0000-000000000099", Version: 1, Amount: "100.25", Apply: true}}
			if _, err := save(forged); err == nil {
				t.Fatal("unknown expense choice accepted")
			}
			stale := card(read(week.AddDate(0, 0, 7))).Edits
			for i := range first.ExpenseDeductions {
				x := &first.ExpenseDeductions[i]
				x.Apply, x.Mode = true, "fixed"
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

			if _, err := save(stale); err != nil {
				t.Fatal("independent draft choice was blocked", err)
			}
			if len(card(read(week.AddDate(0, 0, 7))).Edits.ExpenseDeductions) != 7 {
				t.Fatal("draft choices reserved another statement's balance")
			}
			kind := "driver"
			if route == "investor" {
				kind = "investor"
			}
			report := read(week)
			frozen, err := settlePayrollFixture(t, ctx, pay, kind, week, statement, report.Revision, actor, "", false, false, preparedSettlement)
			if err != nil {
				t.Fatal(err)
			}
			if len(card(frozen).Edits.ExpenseDeductions) != 7 {
				t.Fatal("finalization lost unpaid expense rows")
			}
			next := card(read(week.AddDate(0, 0, 7))).Edits
			if len(next.ExpenseDeductions) != 2 {
				t.Fatalf("finalization did not collect balances: %+v", next)
			}
			for i, want := range []string{"70.15", "100.25"} {
				if next.ExpenseDeductions[i].Available != want {
					t.Fatalf("partial/zero balance: %+v", next)
				}
			}
			create(9) // Late source changes must not rewrite a finalized statement.
			if len(card(read(week)).Edits.ExpenseDeductions) != 7 {
				t.Fatal("finalized snapshot changed")
			}
			reopened, err := settlePayrollFixture(t, ctx, pay, kind, week, statement, read(week).Revision, actor, "Review late expense", true, false, preparedSettlement)
			if err != nil {
				t.Fatal(err)
			}
			if len(card(reopened).Edits.ExpenseDeductions) != 8 || len(card(read(week.AddDate(0, 0, 7))).Edits.ExpenseDeductions) != 8 {
				t.Fatal("reopening did not reverse payments and expose the new expense")
			}
			t.Run("efs", func(t *testing.T) {
				feeWeek := week.AddDate(0, 0, 14)
				otherCategory := id(`SELECT id FROM expense_settings WHERE kind='category' AND name='Other'`)
				createFee := func(amount, method string, base *string, fee string, penalty bool) Expense {
					t.Helper()
					categoryID := otherCategory
					if penalty {
						categoryID = category
					}
					v, err := expenses.CreateExpense(ctx, ExpenseInput{CategoryID: categoryID, ExpenseType: payTestString("EFS collection"), ExpenseDate: feeWeek,
						DriverID: &driver, OwnerID: owner, TruckID: truck, CoveredBy: &coveredBy, Amount: amount, PaymentType: &method, BaseAmount: base, PaymentFee: fee})
					if err != nil {
						t.Fatal(err)
					}
					return v
				}
				cases := []struct {
					amount, method, fee, want string
					base                      *string
					penalty                   bool
				}{
					{amount: "0", method: "EFS", want: "0.00"},
					{amount: "0.01", method: "EFS", want: "3.01"},
					{amount: "500", method: "EFS", want: "503.00"},
					{amount: "500.01", method: " eFs ", want: "506.01"},
					{amount: "1000", method: "EFS", want: "1006.00"},
					{amount: "640", method: "EFS", base: payTestString("634"), fee: "6", want: "640.00"},
					{amount: "634", method: "Cash", want: "634.00"},
					{amount: "100", method: "EFS", penalty: true, want: "100.00"},
				}
				var target Expense
				for _, tc := range cases {
					v := createFee(tc.amount, tc.method, tc.base, tc.fee, tc.penalty)
					x, err := expenses.GetExpense(ctx, v.ID)
					if err != nil || x.RemainingAmount == nil || *x.RemainingAmount != tc.want {
						t.Fatalf("expense balance %+v: %+v %v", tc, x, err)
					}
					found := false
					for _, row := range card(read(feeWeek)).Edits.ExpenseDeductions {
						if row.ExpenseID != v.ID {
							continue
						}
						found = true
						if row.Total != tc.want || row.Amount != tc.want || row.Available != tc.want {
							t.Fatalf("fee missing or doubled: %+v %+v", tc, row)
						}
					}
					if !found && tc.amount != "0" {
						t.Fatal("EFS expense missing", tc)
					}
					if tc.amount == "500.01" {
						target = v
					}
				}
				selectTarget := func(w time.Time, amount string) DriverPayEdits {
					t.Helper()
					edits := card(read(w)).Edits
					for _, row := range edits.ExpenseDeductions {
						if row.ExpenseID == target.ID {
							row.Amount, row.Apply, row.Mode = amount, true, "fixed"
							edits.ExpenseDeductions = []ExpenseDeduction{row}
							return edits
						}
					}
					t.Fatal("EFS remainder missing")
					return edits
				}
				if _, err := save(selectTarget(feeWeek, "506.02")); err == nil {
					t.Fatal("collection above principal plus EFS fee accepted")
				}
				if _, err := save(selectTarget(feeWeek, "500.01")); err != nil {
					t.Fatal("partial EFS collection", err)
				}
				if _, err := settlePayrollFixture(t, ctx, pay, kind, feeWeek, statement, read(feeWeek).Revision, actor, "", false, false, preparedSettlement); err != nil {
					t.Fatal("finalize partial EFS collection", err)
				}
				// Paying the original principal must leave the fee available next
				// week. The fee is charged once, not again on each partial payment.
				nextWeek := feeWeek.AddDate(0, 0, 7)
				nextEdits := selectTarget(nextWeek, "6.00")
				if nextEdits.ExpenseDeductions[0].Available != "6.00" {
					t.Fatalf("fee carry: %+v", nextEdits)
				}
				if _, err := save(nextEdits); err != nil {
					t.Fatal("fee collection", err)
				}
				if _, err := settlePayrollFixture(t, ctx, pay, kind, nextWeek, statement, read(nextWeek).Revision, actor, "", false, false, preparedSettlement); err != nil {
					t.Fatal("finalize EFS remainder", err)
				}
				x, err := expenses.GetExpense(ctx, target.ID)
				if err != nil || *x.Amount != "500.01" || x.BaseAmount != nil || x.PaymentFee != "0.00" || *x.PaidAmount != "506.01" || *x.RemainingAmount != "0.00" {
					t.Fatalf("source changed or fee not collected once: %+v %v", x, err)
				}
				for _, later := range read(nextWeek.AddDate(0, 0, 7)).Drivers {
					for _, row := range later.Edits.ExpenseDeductions {
						if row.ExpenseID == target.ID {
							t.Fatal("fully paid EFS fee repeated")
						}
					}
				}
				if _, err := pool.Exec(ctx, `UPDATE expenses SET payment_type='Cash' WHERE id=$1`, target.ID); err == nil {
					t.Fatal("payment metadata changed the collected balance")
				}
				if _, err := pool.Exec(ctx, `UPDATE expenses SET description='Reviewed' WHERE id=$1`, target.ID); err != nil {
					t.Fatal("unrelated expense edit blocked", err)
				}
			})
		})
	}
}
