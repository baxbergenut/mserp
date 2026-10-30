package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBrokerExpenseMigration(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_CHARGES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local test database required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.Database, "_test") || (cfg.Host != "localhost" && cfg.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	schema := pgx.Identifier{fmt.Sprintf("broker_migration_%d", time.Now().UnixNano())}.Sanitize()
	exec := func(q string) {
		t.Helper()
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema + ",public")
	defer conn.Exec(ctx, "SET search_path TO public; DROP SCHEMA "+schema+" CASCADE")
	exec(`CREATE TABLE expenses(id int,covered_by text,accounting_verified boolean DEFAULT false); INSERT INTO expenses(id,covered_by) VALUES(1,'Broker'),(2,'Company')`)
	migration, err := os.ReadFile("../../sql/177_broker_expenses.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	exec(`UPDATE expenses SET accounting_verified=true WHERE id=1`)
	for _, q := range []string{`INSERT INTO expenses(id,covered_by) VALUES(3,'Broker')`, `UPDATE expenses SET covered_by='Broker' WHERE id=2`} {
		if _, err = conn.Exec(ctx, q); err == nil {
			t.Fatal("missing load accepted", q)
		}
	}
	exec(`UPDATE expenses SET broker_load_number='LEGACY-1' WHERE id=1`)
	if _, err = conn.Exec(ctx, `UPDATE expenses SET broker_load_number=NULL WHERE id=1`); err == nil {
		t.Fatal("reviewed load erased")
	}
}

func TestBrokerExpensesDatabase(t *testing.T) {
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
		driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date,roster_start_week) VALUES('Broker driver','broker driver','cpm',0.75,'2026-09-01','2026-09-07') RETURNING id::text`)
		owner := id(`INSERT INTO investors(full_name) VALUES('Broker owner') RETURNING id::text`)
		truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('BROKER-01',$1) RETURNING id::text`, owner)
		exec(`UPDATE truck_ownership_history SET assigned_at='2026-09-01',start_known=true WHERE truck_id=$1`, truck)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-01')`, truck, driver)
		exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-09-07',88)`, truck, owner)
		exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,raw_payload) VALUES(9811,'00123-A','delivered',1000,1000,100,'2026-09-28 12:00+00','{"trip":{"mile":90,"empty_mile":10},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"A","state":"TX"}},{"stop_type":"delivery","ordering":2,"location":{"city":"B","state":"OH"}}]}')`)
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,'2026-09-28','00123-A',9811,1000)`, driver)
		category := id(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`)
		repo := NewExpenseRepository(pool)
		pay := NewDriverPayRepository(pool)
		week, _ := time.Parse(time.DateOnly, "2026-09-28")
		read := func(kind string) DriverPayWeek {
			t.Helper()
			r, err := pay.PayrollPage(ctx, kind, week, PayPageQuery{Pagination: Pagination{PageSize: 100}})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		beforeDriver, beforeInvestor := read("driver"), read("investor")
		input := ExpenseInput{CategoryID: category, ExpenseDate: week, TruckID: &truck, DriverID: &driver, Amount: "125.50", ExpenseType: payTestString("Lumper"), CoveredBy: payTestString("Broker"), BrokerLoadNumber: payTestString("00123-A")}
		bad := input
		bad.BrokerLoadNumber = nil
		if _, err := repo.CreateExpense(ctx, bad); err == nil {
			t.Fatal("database allowed broker without load")
		}
		expense, err := repo.CreateExpense(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if expense.BrokerLoadNumber == nil || *expense.BrokerLoadNumber != "00123-A" || expense.ChargeDriverID != nil || expense.OwnerID != nil {
			t.Fatal("broker became payroll debt", expense)
		}
		check := func(kind string, before DriverPayWeek) DriverPayDriver {
			t.Helper()
			r := read(kind)
			if r.Pagination.Payable != before.Pagination.Payable {
				t.Fatal("broker changed payable", kind, r.Pagination, before.Pagination)
			}
			for _, d := range r.Drivers {
				if d.ID == driver || d.ID == truck {
					if len(d.BrokerReimbursements) != 1 || d.BrokerReimbursements[0].ExpenseID != expense.ID || d.BrokerReimbursements[0].Amount != "125.50" {
						t.Fatal("missing reminder", kind, d)
					}
					if len(d.Edits.ExpenseDeductions) != 0 {
						t.Fatal("broker created deduction", d)
					}
					return d
				}
			}
			t.Fatal("missing statement", kind)
			return DriverPayDriver{}
		}
		d := check("driver", beforeDriver)
		check("investor", beforeInvestor)
		compact, err := pay.PayrollPage(ctx, "driver", week, PayPageQuery{Summary: true})
		if err != nil || len(compact.Drivers) == 0 || len(compact.Drivers[0].BrokerReimbursements) != 1 {
			t.Fatal("compact reminder missing", compact, err)
		}
		// Existing financial snapshots stay byte-for-byte unchanged; collection notes refresh.
		frozen, _ := json.Marshal(d)
		exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,$2,$3::jsonb)`, driver, week, string(frozen))
		input.BrokerLoadNumber = payTestString("Other-load")
		input.UpdatedAt = &expense.UpdatedAt
		expense, err = repo.UpdateExpense(ctx, expense.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"driver", "investor"} {
			for _, row := range read(kind).Drivers {
				if len(row.BrokerReimbursements) != 0 {
					t.Fatal("stale reminder", kind, row)
				}
			}
		}
		var retained DriverPayDriver
		if err = pool.QueryRow(ctx, `SELECT report FROM payroll_settlements WHERE driver_id=$1`, driver).Scan(&retained); err != nil {
			t.Fatal(err)
		}
		if len(retained.BrokerReimbursements) != 1 || retained.Loads[0].Fee != d.Loads[0].Fee {
			t.Fatal("frozen snapshot rewritten")
		}
		input.BrokerLoadNumber = payTestString("00123-a")
		input.UpdatedAt = &expense.UpdatedAt
		expense, err = repo.UpdateExpense(ctx, expense.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		check("driver", beforeDriver)
		check("investor", beforeInvestor)
		page, err := repo.ListExpensesPage(ctx, ExpensePageQuery{Search: "00123-a", VisibleCategoryIDs: []string{category}, AccessibleCategoryIDs: []string{category}})
		if err != nil || page.Total != 1 {
			t.Fatal("load search", page, err)
		}
		if err = repo.DeleteExpense(ctx, expense.ID); err != nil {
			t.Fatal(err)
		}
		for _, row := range read("driver").Drivers {
			if len(row.BrokerReimbursements) != 0 {
				t.Fatal("deleted reminder remains")
			}
		}
	})
}
