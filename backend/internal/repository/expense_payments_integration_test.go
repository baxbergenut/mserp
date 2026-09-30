package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpensePaymentsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_PAY_TEST_DATABASE_URL to a disposable _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "_test") {
		t.Fatal("only disposable _test databases allowed")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("expense_pay_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`) }()
			source, err := os.ReadFile("../../sql/init.sql")
			if err != nil {
				t.Fatal(err)
			}
			migration, err := os.ReadFile("../../sql/036_driver_expense_balances.sql")
			if err != nil {
				t.Fatal(err)
			}
			ownerMigration, err := os.ReadFile("../../sql/037_expense_responsibility.sql")
			if err != nil {
				t.Fatal(err)
			}
			settlementMigration, err := os.ReadFile("../../sql/038_payroll_settlements.sql")
			if err != nil {
				t.Fatal(err)
			}
			sql := strings.ReplaceAll(string(source), "\r\n", "\n")
			if mode == "migration" {
				sql = strings.Replace(sql, strings.ReplaceAll(string(settlementMigration), "\r\n", "\n"), "", 1)
				sql = strings.Replace(sql, strings.ReplaceAll(string(ownerMigration), "\r\n", "\n"), "", 1)
				sql = strings.Replace(sql, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
			}
			exec(sql)
			var driver, other string
			for i, id := range []*string{&driver, &other} {
				if err := admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.75) RETURNING id`, fmt.Sprintf("Expense driver %d", i)).Scan(id); err != nil {
					t.Fatal(err)
				}
			}
			var legacy string
			if mode == "migration" {
				if err := admin.QueryRow(ctx, `INSERT INTO expenses(company,category,driver_id,covered_by,expense_date,amount) VALUES('MS Express','Other',$1,'Driver','2026-09-28',120) RETURNING id`, driver).Scan(&legacy); err != nil {
					t.Fatal(err)
				}
				exec(string(migration))
				exec(string(ownerMigration))
				exec(string(settlementMigration))
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app;GRANT SELECT ON app_users TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,dispatchers,trucks,truck_driver_assignments,loads,gross_board_entries,fuel_transactions,fuel_transaction_items,tolls TO mserp_app`)
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			expenses := NewExpenseRepository(pool)
			pay := NewDriverPayRepository(pool)
			week, _ := time.Parse(time.DateOnly, "2026-09-28")
			input := ExpenseInput{Company: "MS Express", Category: "Penalties", ExpenseDate: week, DriverID: &driver, Amount: "100.25", ExpenseType: payTestString("Parking violation"), Description: payTestString("Long explanation that must not appear in Driver Pay"), CoveredBy: payTestString("Driver")}
			e, err := expenses.CreateExpense(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			input.CoveredBy = payTestString("Company")
			if _, err = expenses.CreateExpense(ctx, input); err != nil {
				t.Fatal(err)
			}
			balance := func(paid, remaining string) {
				t.Helper()
				x, err := expenses.GetExpense(ctx, e.ID)
				if err != nil || x.PaidAmount == nil || *x.PaidAmount != paid || *x.RemainingAmount != remaining {
					t.Fatalf("balance %+v: %v", x, err)
				}
			}
			read := func(w time.Time) DriverPayEdits {
				t.Helper()
				report, err := pay.Get(ctx, w)
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range report.Drivers {
					if d.ID == driver {
						return d.Edits
					}
				}
				return DriverPayEdits{}
			}
			first := read(week)
			if len(first.ExpenseDeductions) != 1 || first.ExpenseDeductions[0].Amount != "100.25" {
				t.Fatalf("initial suggestions: %+v", first)
			}
			if first.ExpenseDeductions[0].Name != "Parking violation" || first.ExpenseDeductions[0].Category != "Penalties" {
				t.Fatalf("payroll must use only the expense name: %+v", first.ExpenseDeductions[0])
			}
			// Legacy records without a name use their category, never the description.
			if _, err = pool.Exec(ctx, `UPDATE expenses SET expense_type=NULL WHERE id=$1`, e.ID); err != nil {
				t.Fatal(err)
			}
			if got := read(week).ExpenseDeductions[0].Name; got != "Penalties" {
				t.Fatalf("legacy name fallback = %q", got)
			}
			if _, err = pool.Exec(ctx, `UPDATE expenses SET expense_type='Parking violation' WHERE id=$1`, e.ID); err != nil {
				t.Fatal(err)
			}
			first = read(week)
			balance("0", "100.25")
			// Notes and simple reads do not collect the suggested full amount.
			first.Notes = "Notes only"
			first, err = pay.Save(ctx, first)
			if err != nil {
				t.Fatal(err)
			}
			balance("0", "100.25")
			stale := read(week.AddDate(0, 0, 7))
			first.ExpenseDeductions[0].Amount = "30.10"
			first.ExpenseDeductions[0].Apply = true
			first, err = pay.Save(ctx, first)
			if err != nil {
				t.Fatal(err)
			}
			balance("30.10", "70.15")
			stale.ExpenseDeductions[0].Apply = true
			if _, err = pay.Save(ctx, stale); !errors.Is(err, ErrDriverPayConflict) {
				t.Fatalf("cross-week stale deduction accepted: %v", err)
			}
			next := read(week.AddDate(0, 0, 7))
			if next.ExpenseDeductions[0].Amount != "70.15" {
				t.Fatal(next)
			}
			next.ExpenseDeductions[0].Amount = "80"
			next.ExpenseDeductions[0].Apply = true
			if _, err = pay.Save(ctx, next); err == nil {
				t.Fatal("overpayment accepted")
			}
			balance("30.10", "70.15")
			next.ExpenseDeductions[0].Amount = "0"
			next, err = pay.Save(ctx, next)
			if err != nil {
				t.Fatal(err)
			}
			balance("30.10", "70.15")
			final := read(week.AddDate(0, 0, 14))
			if final.ExpenseDeductions[0].Amount != "70.15" {
				t.Fatal(final)
			}
			final.ExpenseDeductions[0].Apply = true
			final, err = pay.Save(ctx, final)
			if err != nil {
				t.Fatal(err)
			}
			balance("100.25", "0.00")
			if len(read(week.AddDate(0, 0, 21)).ExpenseDeductions) != 0 {
				t.Fatal("paid expense carried forward")
			}
			if read(week).ExpenseDeductions[0].Amount != "30.10" || read(week).ExpenseDeductions[0].Remaining != "70.15" {
				t.Fatal("historical amount changed")
			}
			// Cross-driver submissions and edits to paid source amounts are rejected.
			forged := final
			forged.DriverID = other
			forged.Version = 0
			forged.ExpenseDeductions[0].Apply = true
			if _, err = pay.Save(ctx, forged); err == nil {
				t.Fatal("cross-driver deduction accepted")
			}
			input.CoveredBy = payTestString("Driver")
			input.Amount = "200"
			if _, err = expenses.UpdateExpense(ctx, e.ID, input); err == nil {
				t.Fatal("payroll principal mutated")
			}
			if err = expenses.DeleteExpense(ctx, e.ID); err == nil {
				t.Fatal("payroll expense deleted")
			}
			// Stale weekly version rolls back all payment writes too.
			old := read(week)
			old.ExpenseDeductions[0].Amount = "0"
			old.ExpenseDeductions[0].Apply = true
			old.Version = 0
			if _, err = pay.Save(ctx, old); !errors.Is(err, ErrDriverPayConflict) {
				t.Fatal(err)
			}
			balance("100.25", "0.00")
			if legacy != "" {
				x, err := expenses.GetExpense(ctx, legacy)
				if err != nil || !x.DriverSettled || *x.PaidAmount != "120.00" || *x.RemainingAmount != "0" {
					t.Fatalf("legacy settlement %+v %v", x, err)
				}
			}
			testOwnerSettlements(t, ctx, admin, pool, driver, other)
		})
	}
}

func payTestString(s string) *string { return &s }
