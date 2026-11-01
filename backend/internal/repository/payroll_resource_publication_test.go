package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollPublicationResourcesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const independent = "18200000-0000-0000-0000-000000009601"
		if _, err := pool.Exec(ctx, `
 INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000009601','Resource publication','resource publication','cpm',0.65,'2026-01-01');
 UPDATE truck_driver_assignments SET assigned_at='2026-09-01';
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at)
 VALUES('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000009601','2026-01-01','2026-02-01');
 INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'Resource publication','2026-10-05',100,'18200000-0000-0000-0000-000000000001','Driver' FROM expense_settings WHERE kind='category' AND name='Other';
 INSERT INTO expense_payments(expense_id,week_start,amount,investor_truck_id)
 SELECT id,'2027-01-04',20,'18200000-0000-0000-0000-000000000002' FROM expenses WHERE expense_type='Resource publication';
 INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount,opening_paid)
 VALUES('18200000-0000-0000-0000-000000000001','Projection Driver','2026-10-05',300,50);
 INSERT INTO driver_charge_schedules(driver_id,kind,name,direction,start_week,eligibility,total)
 VALUES('18200000-0000-0000-0000-000000009601','installment','Resource advance','charge','2026-10-05','calendar',125);
 INSERT INTO driver_pay_weeks(driver_id,week_start,fuel_override)
 VALUES('18200000-0000-0000-0000-000000000001','2026-09-28',-10);`); err != nil {
			t.Fatal(err)
		}
		for _, week := range []string{"2026-01-05", "2026-10-05", "2027-01-04"} {
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			reports := [2]DriverPayWeek{
				{Drivers: []DriverPayDriver{{ID: projectionDriver}, {ID: independent}}},
				{Drivers: []DriverPayDriver{{ID: projectionTruck}}},
			}
			read, err := payrollSelectionBatchContext(ctx, tx, []string{"2026-01-05", "2026-10-05", "2027-01-04"})
			if err != nil {
				t.Fatal(err)
			}
			read = payrollPublicationReadContext(read, tx)
			keys, err := payrollPublicationResources(read, tx, week, reports)
			if err != nil {
				t.Fatal(err)
			}
			cached, metrics := db.WithQueryMetrics(read)
			repeated, err := payrollPublicationResources(cached, tx, week, reports)
			if err != nil || metrics.Queries.Load() != 0 {
				t.Fatal("same-phase resource inputs were not reused", err, metrics.Queries.Load())
			}
			wantKeys, _ := json.Marshal(keys)
			gotKeys, _ := json.Marshal(repeated)
			if string(wantKeys) != string(gotKeys) {
				t.Fatal("reused resource membership changed")
			}
			for i, kind := range []string{"driver", "investor"} {
				for _, row := range reports[i].Drivers {
					want, err := payrollStatementResourceSnapshot(ctx, tx, kind, row.ID, week, map[string][]string{})
					if err != nil {
						t.Fatal(err)
					}
					var got json.RawMessage
					if err := tx.QueryRow(ctx, `SELECT payroll_resource_snapshot($1::text[])`, keys[kind+":"+row.ID]).Scan(&got); err != nil {
						t.Fatal(err)
					}
					if string(got) != string(want) {
						t.Fatalf("%s %s %s resource dependencies differ\ngot %s\nwant %s", week, kind, row.ID, got, want)
					}
				}
			}
			_ = tx.Rollback(ctx)
		}
	})
}
