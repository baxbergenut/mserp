package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestTruckChargePublicationScopeDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const typ = "18200000-0000-0000-0000-000000001601"
		const other = "18200000-0000-0000-0000-000000001602"
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO driver_charge_types(id,name,direction,amount,amounts)
 VALUES('18200000-0000-0000-0000-000000001601','Window fee','charge',20,ARRAY[20,25,30]::numeric[]),
 ('18200000-0000-0000-0000-000000001602','Other window fee','charge',20,ARRAY[20,25,30]::numeric[]);
 INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount)
 VALUES('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000001601','2026-10-05',20),
 ('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000001601','2026-10-19',30);
 INSERT INTO investors(id,full_name)
 SELECT md5('recurring-owner-'||n)::uuid,'Fixture Owner '||n FROM generate_series(1,146) n;
 INSERT INTO trucks(id,unit_number,owner_id)
 SELECT md5('recurring-truck-'||n)::uuid,'FEE-FIXTURE-'||n,md5('recurring-owner-'||n)::uuid FROM generate_series(1,146) n;
 UPDATE truck_ownership_history SET start_known=false;
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000000001','2026-10-12','WINDOW-2',1000,1200,400),
 ('18200000-0000-0000-0000-000000000001','2026-10-19','WINDOW-3',1000,1200,400);
 INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included)
 SELECT t.id,c.id,date '2026-09-14'+7*n,20,n<3 FROM trucks t CROSS JOIN driver_charge_types c
 CROSS JOIN generate_series(0,3) n WHERE t.unit_number LIKE 'FEE-FIXTURE-%';`)
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.SwitchPayrollProtocol(ctx, "resources", 1); err != nil {
			t.Fatal(err)
		}
		dates := []string{"2026-10-05", "2026-10-12", "2026-10-19"}
		for _, date := range dates {
			w, _ := chargeWeek(date)
			if err := repo.RequestPayrollPreparation(ctx, w, false); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
		}
		chargeRepo := NewDriverChargeRepository(pool)
		v := TruckChargePhase{TruckID: projectionTruck, TypeID: typ, WeekStart: dates[1], Amount: "25", Included: true, TypeVersion: 1}
		// Replay the old all-weeks publication with the same dated scope, then
		// roll it back so the optimized command starts from identical inputs.
		baselineCtx, baseline := db.WithMetrics(WithPayrollPublication(ctx))
		tx, err := beginPayrollMutation(baselineCtx, pool)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = lockTruckAccounting(baselineCtx, tx); err != nil {
			t.Fatal(err)
		}
		if err = saveTruckCharge(baselineCtx, tx, v, ""); err != nil {
			t.Fatal(err)
		}
		var changed bool
		if err = tx.QueryRow(baselineCtx, `SELECT EXISTS(SELECT 1 FROM payroll_projection_changes WHERE source_xid=pg_current_xact_id_if_assigned() AND dispatched_at IS NULL)`).Scan(&changed); err != nil || !changed {
			t.Fatal(changed, err)
		}
		if handled, e := publishLiveDriverExpenses(baselineCtx, tx); e != nil || handled {
			t.Fatal(handled, e)
		}
		impact, err := payrollMutationImpact(baselineCtx, tx, &payrollCommand{name: "native", beforeSnapshot: tx.(*payrollMutationTransaction).beforeSnapshot}, "", "")
		if err != nil || impact == nil || !impact.chargeScoped || len(impact.weeks) != 1 || !impact.weeks[dates[1]] {
			t.Fatalf("expected matched audit and one affected week: %+v %v", impact, err)
		}
		impact.weeks, impact.chargeScoped = nil, false
		if _, err = publishPayrollCommand(baselineCtx, tx, "", impact); err != nil {
			t.Fatal(err)
		}
		if err = tx.Rollback(baselineCtx); err != nil {
			t.Fatal(err)
		}
		write, metrics := db.WithMetrics(WithPayrollPublication(ctx))
		if err = chargeRepo.SaveTruckCharge(write, v, ""); err != nil {
			t.Fatal(err)
		}
		t.Logf("matched edit: all-weeks=%d scoped=%d queries, phases=%v; baseline=%v", baseline.Queries.Load(), metrics.Queries.Load(), metrics.Events(), baseline.Events())
		if events := metrics.Events(); events["truck_charge_calculated_weeks"] != 1 || events["truck_charge_payroll_"+dates[1]+"_queries"] == 0 {
			t.Fatalf("expected only the effective week to calculate: %v", events)
		}
		if metrics.Queries.Load()*2 > baseline.Queries.Load() {
			t.Fatalf("expected at least 50%% fewer queries: %d vs %d", metrics.Queries.Load(), baseline.Queries.Load())
		}
		compare := func() {
			t.Helper()
			for _, date := range dates {
				w, _ := chargeWeek(date)
				read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				if err != nil {
					t.Fatal(err)
				}
				full, err := readPayrollProjectionReference(ctx, read, w, true)
				_ = read.Rollback(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for i, kind := range []string{"driver", "investor"} {
					page, err := repo.ReadPreparedPayrollPage(ctx, kind, w, PayPageQuery{})
					a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
					b, _ := json.Marshal(page.Report)
					if err != nil || page.State != "ready" || string(a) != string(b) {
						t.Fatalf("%s %s differs from full calculator: %v\n%s\n%s", date, kind, err, a, b)
					}
				}
			}
		}
		compare()

		inherited := v
		inherited.WeekStart = "2026-11-02"
		inherited.Amount = "30.00"
		v.Version, v.Amount = 1, "25.00"
		noop, noops := db.WithMetrics(WithPayrollPublication(ctx))
		for _, phase := range []TruckChargePhase{v, inherited} {
			if err = chargeRepo.SaveTruckCharge(noop, phase, ""); err != nil {
				t.Fatal(err)
			}
		}
		var audits, version, inheritedRows int
		if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM truck_settlement_events WHERE action='charge'),
 (SELECT version FROM truck_charge_phases WHERE truck_id=$1 AND type_id=$2 AND week_start=$3::date),
 (SELECT count(*) FROM truck_charge_phases WHERE truck_id=$1 AND type_id=$2 AND week_start=$4::date)`,
			v.TruckID, v.TypeID, v.WeekStart, inherited.WeekStart).Scan(&audits, &version, &inheritedRows); err != nil {
			t.Fatal(err)
		}
		if audits != 1 || version != 1 || inheritedRows != 0 || noops.Events()["truck_charge_noop"] != 2 || noops.Events()["payroll_calculation"] != 0 {
			t.Fatalf("identical saves wrote history or recalculated: %d %d %d %v", audits, version, inheritedRows, noops.Events())
		}
		stale := v
		stale.Version = 0
		if err = chargeRepo.SaveTruckCharge(noop, stale, ""); !errors.Is(err, ErrChargeConflict) {
			t.Fatalf("stale exact version passed no-op: %v", err)
		}

		// A real future edit and an invalid stale cell still allow the remaining
		// unchanged cell to succeed, without any completed-week calculation.
		future := inherited
		current, _ := chargeWeek(ChargeCurrentWeek())
		future.WeekStart, future.TypeID, future.Amount = current.AddDate(0, 0, 7).Format(time.DateOnly), other, "25"
		stale.WeekStart, stale.Version = dates[2], 77
		bulk, bulkMetrics := db.WithMetrics(WithPayrollPublication(ctx))
		results, err := chargeRepo.SaveTruckCharges(bulk, []TruckChargePhase{v, stale, future}, "")
		if err != nil || len(results) != 3 || results[0].Error != "" || results[1].Error == "" || results[2].Error != "" || bulkMetrics.Events()["payroll_calculation"] != 0 {
			t.Fatalf("future bulk changed completed payroll: %+v %v %v", results, bulkMetrics.Events(), err)
		}
		compare()

		// A capture consumed by the worker must reach the same date proof,
		// even before the dispatcher has processed it.
		future.Amount, future.Version = "30", 1
		if err = chargeRepo.SaveTruckCharge(ctx, future, ""); err != nil {
			t.Fatal(err)
		}
		read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer read.Rollback(ctx)
		snapshot, err := payrollCacheSnapshot(ctx, read)
		if err != nil {
			t.Fatal(err)
		}
		for _, date := range dates {
			w, _ := chargeWeek(date)
			proof, err := payrollPendingWeekImpact(ctx, read, w, snapshot)
			if err != nil || proof == nil || !proof.chargeScoped || proof.weeks[date] {
				t.Fatalf("worker cannot prove %s unaffected: %+v %v", date, proof, err)
			}
		}
		if err = read.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.DispatchPayrollChanges(ctx); err != nil {
			t.Fatal(err)
		}
		compare()

		// A saved deferral blocks the upper-bound optimization; keep the
		// conservative later scope while still excluding earlier weeks.
		check, err := beginPayrollMutation(write, pool)
		if err != nil {
			t.Fatal(err)
		}
		defer check.Rollback(ctx)
		if _, err = check.Exec(ctx, `INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement)
 VALUES('truck_charge:'||$1::uuid::text||':'||$2::uuid::text,'18200000-0000-0000-0000-000000000003',$3::date,'investor',$1,$1,'Window fee',-25,0,'{}')`,
			v.TruckID, v.TypeID, v.WeekStart); err != nil {
			t.Fatal(err)
		}
		if err = check.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		check, err = beginPayrollMutation(write, pool)
		if err != nil {
			t.Fatal(err)
		}
		defer check.Rollback(ctx)
		v.Amount = "20"
		if err = saveTruckCharge(write, check, v, ""); err != nil {
			t.Fatal(err)
		}
		impact, err = payrollMutationImpact(write, check, &payrollCommand{name: "native", beforeSnapshot: check.(*payrollMutationTransaction).beforeSnapshot}, "", "")
		if err != nil || impact == nil || !impact.chargeScoped || impact.weeks[dates[0]] || !impact.weeks[dates[1]] || !impact.weeks[dates[2]] {
			t.Fatalf("saved deferral did not retain later scope: %+v %v", impact, err)
		}
		if err = truckAudit(write, check, v.TruckID, v.WeekStart, "terms", "", nil); err != nil {
			t.Fatal(err)
		}
		impact, err = payrollMutationImpact(write, check, &payrollCommand{name: "native", beforeSnapshot: check.(*payrollMutationTransaction).beforeSnapshot}, "", "")
		if err != nil || impact == nil || impact.chargeScoped || impact.weeks != nil {
			t.Fatalf("unmatched audit must retain the conservative path: %+v %v", impact, err)
		}
		if err = check.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var savedAmount string
		if err = pool.QueryRow(ctx, `SELECT amount::text FROM truck_charge_phases WHERE truck_id=$1 AND type_id=$2 AND week_start=$3::date`,
			v.TruckID, v.TypeID, v.WeekStart).Scan(&savedAmount); err != nil || savedAmount != "25.00" {
			t.Fatalf("rolled back correction changed the phase: %s %v", savedAmount, err)
		}
	})
}
