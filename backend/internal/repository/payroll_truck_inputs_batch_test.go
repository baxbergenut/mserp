package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollTruckInputsBatchDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,truck_id,owner_id,covered_by)
 SELECT v.id::uuid,s.id,s.name,v.name,v.day::date,v.amount,t.id,t.owner_id,'Truck owner'
 FROM expense_settings s CROSS JOIN trucks t CROSS JOIN (VALUES
 ('18200000-0000-0000-0000-000000009801','First invoice','2026-10-07',200),
 ('18200000-0000-0000-0000-000000009802','Second invoice','2026-10-07',80),
 ('18200000-0000-0000-0000-000000009803','Thursday boundary','2026-10-08',45)) v(id,name,day,amount)
 WHERE s.kind='category' AND s.name='Other';
 INSERT INTO expense_payments(expense_id,week_start,amount,investor_truck_id) VALUES
 ('18200000-0000-0000-0000-000000009801','2026-10-05',0,'18200000-0000-0000-0000-000000000002'),
 ('18200000-0000-0000-0000-000000009801','2026-10-19',30,'18200000-0000-0000-0000-000000000002');
 INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits) SELECT id,'2026-10-05',owner_id,'{}' FROM trucks;`); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		publication := payrollPublicationReadContext(ctx, tx)
		scopes := []payrollTruckInputScope{}
		for _, week := range []string{"2026-09-21", "2026-09-28", "2026-10-05", "2026-10-12", "2026-10-19", "2026-11-02"} {
			scopes = append(scopes, payrollTruckInputScope{week, []string{projectionTruck}})
		}
		if err = preparePayrollTruckInputs(publication, tx, scopes); err != nil {
			t.Fatal(err)
		}
		for _, scope := range scopes {
			for _, costsOnly := range []bool{false, true} {
				selected := context.WithValue(publication, payrollSelectionKey{}, &payrollSelection{trucks: scope.Trucks})
				selected = context.WithValue(selected, payrollCostsOnlyKey{}, costsOnly)
				measured, metrics := db.WithQueryMetrics(selected)
				got, err := readTruckPayInputs(measured, tx, scope.Week)
				if err != nil || metrics.Queries.Load() != 0 {
					t.Fatal("batch not reused", err, metrics.Queries.Load())
				}
				want, err := readTruckPayInputsUncached(selected, tx, scope.Week)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("batch differs %s costs=%t: %v\ngot %#v\nwant %#v", scope.Week, costsOnly, err, got, want)
				}
				delete(got.metadata, projectionTruck)
				if len(got.people) > 0 {
					got.people[0].person.Name = "Caller mutation"
				}
				for id, rows := range got.expenses {
					if len(rows) > 0 {
						rows[0].deduction.Amount = "999.00"
						got.expenses[id] = rows
					}
				}
				again, err := readTruckPayInputs(selected, tx, scope.Week)
				if err != nil || !reflect.DeepEqual(again, want) {
					t.Fatal("batch changed by caller", err)
				}
			}
		}
	})
}
