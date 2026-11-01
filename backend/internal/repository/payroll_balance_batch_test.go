package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollBalanceBatchDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,payment_type)
 SELECT v.id::uuid,s.id,s.name,v.name,v.day::date,v.amount,
 '18200000-0000-0000-0000-000000000001','18200000-0000-0000-0000-000000000001',v.method
 FROM expense_settings s CROSS JOIN (VALUES
 ('18200000-0000-0000-0000-000000009601','Invoice','2026-10-07',200,''),
 ('18200000-0000-0000-0000-000000009602','Payout','2026-10-07',80,'Reimburse'),
 ('18200000-0000-0000-0000-000000009603','Thursday boundary','2026-10-08',45,'')) v(id,name,day,amount,method)
 WHERE s.kind='category' AND s.name='Other';
 INSERT INTO expense_payments(expense_id,week_start,amount) VALUES
 ('18200000-0000-0000-0000-000000009601','2026-10-05',0),
 ('18200000-0000-0000-0000-000000009601','2026-10-19',30);
 INSERT INTO expense_reimbursement_payments(expense_id,week_start,amount)
 VALUES('18200000-0000-0000-0000-000000009602','2026-10-19',12);
 INSERT INTO driver_escrows(id,driver_id,driver_name,start_date,amount,opening_paid)
 VALUES('18200000-0000-0000-0000-000000009604','18200000-0000-0000-0000-000000000001','Projection Driver','2026-09-28',300,100);
 INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) VALUES
 ('18200000-0000-0000-0000-000000009604','2026-10-05',50),
 ('18200000-0000-0000-0000-000000009604','2026-10-19',25);`); err != nil {
			t.Fatal(err)
		}
		// Historical fixture receipt only. The read comparison below retains all
		// normal capacity rules, including payment-before-release in one week.
		if _, err := admin.Exec(ctx, `ALTER TABLE driver_escrow_releases DISABLE TRIGGER escrow_release_guard;
 INSERT INTO driver_escrow_releases(escrow_id,week_start,amount)
 VALUES('18200000-0000-0000-0000-000000009604','2026-10-05',40);
 ALTER TABLE driver_escrow_releases ENABLE TRIGGER escrow_release_guard;`); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		scopes := []payrollBalanceScope{}
		for _, week := range []string{"2026-09-21", "2026-09-28", "2026-10-05", "2026-10-12", "2026-10-19", "2026-11-02"} {
			scopes = append(scopes, payrollBalanceScope{Week: week, Drivers: []string{projectionDriver}})
		}
		batched, err := payrollBalanceBatchContext(ctx, tx, scopes)
		if err != nil {
			t.Fatal(err)
		}
		publication := payrollPublicationReadContext(batched, tx)
		if err := preparePayrollDriverEligibility(publication, tx, scopes); err != nil {
			t.Fatal(err)
		}
		for _, scope := range scopes {
			week, _ := chargeWeek(scope.Week)
			selected := context.WithValue(publication, payrollSelectionKey{}, &payrollSelection{drivers: scope.Drivers})
			measured, metrics := db.WithQueryMetrics(selected)
			got, err := readPayrollDriverEligibility(measured, tx, week, "", scope.Week)
			if err != nil || metrics.Queries.Load() != 0 {
				t.Fatal("batched eligibility was not reused", err, metrics.Queries.Load())
			}
			authority := context.WithValue(selected, payrollAuthoritativeKey{}, true)
			want, err := readPayrollDriverEligibility(authority, tx, week, "", scope.Week)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("eligibility differs for %s: got %#v want %#v: %v", scope.Week, got, want, err)
			}
			for id, value := range got {
				value.edits.Notes = "caller edit"
				got[id] = value
			}
			again, err := readPayrollDriverEligibility(selected, tx, week, "", scope.Week)
			if err != nil || !reflect.DeepEqual(again, want) {
				t.Fatal("caller changed retained eligibility", err)
			}
		}
		for _, scope := range scopes {
			for _, read := range []struct {
				name string
				fn   func(context.Context, pgx.Tx, []string, string) (map[string][]ExpenseDeduction, error)
			}{{"all", expenseDeductionsBulk}, {"escrow", escrowDeductionsBulk}, {"reimburse", expenseReimbursementDeductionsBulk}} {
				want, err := read.fn(ctx, tx, scope.Drivers, scope.Week)
				if err != nil {
					t.Fatal(err)
				}
				got, err := read.fn(batched, tx, scope.Drivers, scope.Week)
				if err != nil {
					t.Fatal(err)
				}
				a, _ := json.Marshal(want)
				b, _ := json.Marshal(got)
				if string(a) != string(b) {
					t.Fatalf("%s %s: want %s got %s", read.name, scope.Week, a, b)
				}
				for _, entries := range got {
					if len(entries) > 0 {
						entries[0].Amount = "999.00"
					}
				}
				again, _ := read.fn(batched, tx, scope.Drivers, scope.Week)
				b, _ = json.Marshal(again)
				if string(a) != string(b) {
					t.Fatal("caller mutated the shared balance input")
				}
			}
		}
		for _, scope := range []payrollBalanceScope{{Week: "2026-11-09", Drivers: []string{projectionDriver}}, {Week: "2026-10-05", Drivers: []string{projectionTruck}}} {
			if _, ok := payrollBatchedDeductions(batched, tx, scope.Drivers, scope.Week, "all"); ok {
				t.Fatal("incomplete scope served as complete")
			}
		}
		authority := context.WithValue(batched, payrollAuthoritativeKey{}, true)
		if _, ok := payrollBatchedDeductions(authority, tx, scopes[0].Drivers, scopes[0].Week, "all"); ok {
			t.Fatal("independent reference used prepared balance input")
		}
		other, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer other.Rollback(ctx)
		if _, ok := payrollBatchedDeductions(batched, other, scopes[0].Drivers, scopes[0].Week, "all"); ok {
			t.Fatal("balance input crossed transaction snapshots")
		}
	})
}
