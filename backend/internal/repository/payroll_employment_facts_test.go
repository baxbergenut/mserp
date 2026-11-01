package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollEmploymentFactsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		const week = "2026-10-05"
		check := func(away, stopped bool, expected int) {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			snapshot, err := payrollCacheSnapshot(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			prepared := payrollRoutingFactsContext(context.WithValue(ctx, payrollNormalizedFactsKey{}, true), snapshot)
			prepared = context.WithValue(prepared, payrollSelectionKey{}, &payrollSelection{drivers: []string{projectionDriver}, trucks: []string{projectionTruck}})
			want, err := readVacationStateUncached(ctx, tx, week, true)
			if err != nil {
				t.Fatal(err)
			}
			got, err := readVacationWeek(prepared, tx, week)
			if err != nil || !reflect.DeepEqual(want, got) || got.trucks[projectionTruck] != away {
				t.Fatal("vacation source reuse differs", want, got, away, err)
			}
			wantStopped, err := readStoppedTruckChargesUncached(ctx, tx, week)
			if err != nil {
				t.Fatal(err)
			}
			gotStopped, err := readPreparedStoppedTruckCharges(prepared, tx, week)
			if err != nil || !reflect.DeepEqual(wantStopped, gotStopped) || gotStopped[projectionTruck] != stopped {
				t.Fatal("employment source reuse differs", wantStopped, gotStopped, stopped, err)
			}
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM payroll_routing_facts WHERE kind IN ('vacations','stopped_charges')`).Scan(&count); err != nil || count != expected {
				t.Fatal("wrong employment source revision reuse", count, expected, err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		check(false, false, 2)
		check(false, false, 2) // Complete facts survive context/transaction eviction.
		exec(`INSERT INTO driver_pay_weeks(driver_id,week_start,notes) VALUES($1,$2,'Financial edit')`, projectionDriver, week)
		check(false, false, 2) // Allocations and edits cannot invalidate source facts.
		exec(`INSERT INTO fleet_status_history(entity_kind,entity_id,entity_name,track,previous_status,status,effective_at,actor_name,source)
 VALUES('driver',$1,'Projection Driver','availability','active','vacation','2026-10-05 00:00-04','Test','profile')`, projectionDriver)
		check(true, false, 3)
		exec(`INSERT INTO fleet_status_history(entity_kind,entity_id,entity_name,track,previous_status,status,effective_at,actor_name,source)
 VALUES('driver',$1,'Projection Driver','availability','vacation','active','2026-10-07 00:00-04','Test','profile')`, projectionDriver)
		check(false, false, 4) // Partial vacation retains recurring fees.
		exec(`UPDATE investors SET driver_id=$1 WHERE id=$2`, projectionDriver, "18200000-0000-0000-0000-000000000003")
		exec(`UPDATE drivers SET status='terminated',termination_date='2026-10-04' WHERE id=$1`, projectionDriver)
		check(false, true, 6)
		const relief = "18200000-0000-0000-0000-000000009900"
		exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Continuing crew','continuing crew','cpm',0.7)`, relief)
		exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,$3)`, projectionDriver, relief, week)
		check(false, false, 8) // Employment cutoff cannot suppress a working team.
		exec(`UPDATE driver_teams SET end_week=$2 WHERE team_driver_id=$1`, relief, week)
		check(false, true, 10)
		// A fact prepared under a different current-week meaning must not satisfy
		// today's legacy-availability lookup, even with identical table revisions.
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		snapshot, err := payrollCacheSnapshot(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		old := payrollRoutingFactsContext(context.WithValue(ctx, payrollNormalizedFactsKey{}, true), snapshot)
		old.Value(payrollRoutingFactsKey{}).(*payrollRoutingFactsState).calendar = "2026-01-05"
		if _, err = readVacationWeek(old, tx, week); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM payroll_routing_facts WHERE kind='vacations'`).Scan(&count); err != nil || count != 7 {
			t.Fatal("calendar rollover reused a legacy-availability fact", count, err)
		}
	})
}
