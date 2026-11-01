package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollRoutingFactsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES($1,'2026-10-06','2026-10-06','','','','','PROJECTION','','','2026-10-06','12:00','',40,repeat('r',64))`, projectionTruck)
		check := func(selected bool, expected int) {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			snapshot, err := payrollProjectionFence(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			read := ctx
			if selected {
				read = context.WithValue(read, payrollSelectionKey{}, &payrollSelection{drivers: []string{projectionDriver}, trucks: []string{projectionTruck}})
			}
			prepared := payrollRoutingFactsContext(context.WithValue(read, payrollNormalizedFactsKey{}, true), snapshot)
			wantLoads, err := truckPayLoadMapUncached(read, tx, "2026-10-05")
			if err != nil {
				t.Fatal(err)
			}
			gotLoads, err := truckPayLoadMap(prepared, tx, "2026-10-05")
			if err != nil || !reflect.DeepEqual(wantLoads, gotLoads) {
				t.Fatal("dated load routing differs", wantLoads, gotLoads, err)
			}
			wantCosts, err := readTruckCostAllocationsUncached(read, tx, "2026-10-05")
			if err != nil {
				t.Fatal(err)
			}
			gotCosts, err := readTruckCostAllocations(prepared, tx, "2026-10-05")
			if err != nil || !reflect.DeepEqual(wantCosts, gotCosts) {
				t.Fatal("dated cost routing differs", wantCosts, gotCosts, err)
			}
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM payroll_routing_facts`).Scan(&count); err != nil || count != expected {
				t.Fatal("wrong reusable fact count", count, expected, err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		check(true, 0) // Partial miss cannot become a full-week fact.
		check(false, 2)
		check(true, 2) // New transaction/context reuses persisted facts.
		exec(`INSERT INTO driver_pay_weeks(driver_id,week_start,notes) VALUES($1,'2026-10-05','Ledger edit')`, projectionDriver)
		check(false, 2)
		exec(`UPDATE tolls SET amount=41`)
		check(true, 2) // A stale full fact is not used or replaced by a subset.
		check(false, 3)
		exec(`UPDATE truck_driver_assignments SET unassigned_at='2026-10-04'`)
		check(false, 5)
		check(true, 5)
		if _, err := pool.Exec(ctx, `UPDATE payroll_routing_facts SET payload='{}'`); err == nil {
			t.Fatal("immutable routing facts were editable")
		}
	})
}

func TestPayrollDriverFactEligibilityDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		// Evidence routing is unnecessary for a truly empty source cohort. A
		// zero-valued transaction still needs attribution and retained inputs.
		if _, err := pool.Exec(ctx, `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES($1,'2026-10-06','2026-10-06','','','','','PROJECTION','Agency','Plaza','2026-10-06','12:00','',0,repeat('y',64))`, projectionTruck); err != nil {
			t.Fatal(err)
		}
		const driver = "18200000-0000-0000-0000-000000001200"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES($1,'Newly eligible','newly eligible','cpm',0.65,'2026-01-01')`, driver); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		var evidencePrepared bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_routing_facts WHERE kind='driver_cost_base' AND entity_id='')
 AND EXISTS(SELECT 1 FROM payroll_routing_facts WHERE kind='native_costs')`).Scan(&evidencePrepared); err != nil || !evidencePrepared {
			t.Fatal("preparation did not retain its evidence inputs", evidencePrepared, err)
		}
		check := func(present bool) {
			t.Helper()
			page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
			if err != nil || page.State != "ready" {
				t.Fatal("eligibility publication", page.State, err)
			}
			_, found := page.Statements[driver]
			if found != present {
				t.Fatal("stale driver eligibility", found, present)
			}
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			full, err := readPayrollProjectionReference(ctx, tx, week, true)
			_ = tx.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(PaginatePay(full[0], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if string(a) != string(b) {
				t.Fatalf("dynamic driver eligibility differs\n%s\n%s", a, b)
			}
			var versions int
			if err = pool.QueryRow(ctx, `SELECT count(DISTINCT input_fingerprint) FROM payroll_routing_facts WHERE kind='driver_base'`).Scan(&versions); err != nil || versions != 1 {
				t.Fatal("financial edit rebuilt static driver facts", versions, err)
			}
		}
		check(false)
		var category string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`).Scan(&category); err != nil {
			t.Fatal(err)
		}
		principal := driver
		expenses := NewExpenseRepository(pool)
		expense, err := expenses.CreateExpense(WithPayrollPublication(ctx), ExpenseInput{CategoryID: category, ExpenseDate: week, DriverID: &principal, Amount: "75.00", ExpenseType: payTestString("New eligibility"), CoveredBy: payTestString("Driver")})
		if err != nil {
			t.Fatal(err)
		}
		check(true)
		if err = expenses.DeleteExpense(WithPayrollPublication(ctx), expense.ID); err != nil {
			t.Fatal(err)
		}
		check(false)
	})
}

func TestPayrollNativeCostFactsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES($1,'2026-10-06','2026-10-06','','','','','PROJECTION','Agency','Plaza','2026-10-06','12:00','',40,repeat('z',64))`, projectionTruck); err != nil {
			t.Fatal(err)
		}
		check := func(expected int, attributed bool) {
			t.Helper()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			snapshot, err := payrollCacheSnapshot(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			prepared := payrollRoutingFactsContext(context.WithValue(ctx, payrollNormalizedFactsKey{}, true), snapshot)
			prepared = context.WithValue(prepared, payrollCostDetailKey{}, true)
			prepared = context.WithValue(prepared, payrollSelectionKey{}, &payrollSelection{drivers: []string{projectionDriver}, trucks: []string{projectionTruck}})
			want, err := readNativeCostTransactionsUncached(ctx, tx, "2026-10-05")
			if err != nil {
				t.Fatal(err)
			}
			got, err := readNativeCostTransactions(prepared, tx, "2026-10-05")
			if err != nil || !reflect.DeepEqual(want, got) || len(got) != 1 || got[0].driverAttributed != attributed || got[0].sortKey == "" || got[0].kind != "toll" {
				t.Fatal("native evidence lost source attribution", want, got, err)
			}
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM payroll_routing_facts WHERE kind='native_costs'`).Scan(&count); err != nil || count != expected {
				t.Fatal("native source revision reuse", count, expected, err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		check(1, true)
		check(1, true) // Process-local context eviction still reads the stored input.
		if _, err := pool.Exec(ctx, `UPDATE truck_driver_assignments SET unassigned_at='2026-10-04'`); err != nil {
			t.Fatal(err)
		}
		check(2, false) // Never reuse old attribution after a dated assignment edit.
	})
}
