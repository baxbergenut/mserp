package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"testing"
)

func applyLaterTestMigrations(t *testing.T, ctx context.Context, db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, after string) {
	t.Helper()
	files, err := filepath.Glob("../../sql/[0-9][0-9][0-9]_*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Base(f)[:3] <= after {
			continue
		}
		sql, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, string(sql)); e != nil {
			t.Fatalf("%s: %v", f, e)
		}
	}
}

func checkInvestorChargeHandoff(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	id := func(sql string, args ...any) string {
		t.Helper()
		var v string
		if e := pool.QueryRow(ctx, sql, args...).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	owner := id(`INSERT INTO investors(full_name) VALUES('Handoff owner') RETURNING id`)
	truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('HANDOFF',$1) RETURNING id`, owner)
	if _, e := pool.Exec(ctx, `UPDATE truck_ownership_history SET assigned_at='2026-01-01' WHERE truck_id=$1`, truck); e != nil {
		t.Fatal(e)
	}
	first := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('First hired','first hired','cpm',0.5) RETURNING id`)
	next := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Next hired','next hired','cpm',0.5) RETURNING id`)
	cr := NewDriverChargeRepository(pool)
	typ, e := cr.SaveType(ctx, ChargeType{Name: "Handoff fee", Direction: "charge", Amount: "75.00", Amounts: []string{"75.00", "125.00"}, Eligibility: "calendar"}, actor)
	if e != nil {
		t.Fatal(e)
	}
	for _, driver := range []string{first, next} {
		amount := "75.00"
		if driver == next {
			amount = "125.00"
		}
		if e = cr.SaveCell(ctx, ChargeCell{DriverID: driver, TypeID: typ.ID, WeekStart: "2026-01-05", Included: true, Amount: amount, TypeVersion: typ.Version}, actor); e != nil {
			t.Fatal(e)
		}
	}
	loan := id(`INSERT INTO driver_charge_schedules(driver_id,kind,name,direction,start_week,eligibility,total) VALUES($1,'installment','Personal advance','charge','2026-01-05','calendar',500) RETURNING id`, first)
	if _, e = pool.Exec(ctx, `INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES($1,'2026-01-05',25)`, loan); e != nil {
		t.Fatal(e)
	}
	// Seed an already-existing assignment to exercise deployment backfill behavior.
	if _, e = pool.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-30 14:00+00')`, truck, first); e != nil {
		t.Fatal(e)
	}
	run := func(driver string) {
		t.Helper()
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		if e = handoffInvestorDriverCharges(ctx, tx, driver, actor); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
	run(first)
	data, e := cr.List(ctx, first)
	if e != nil {
		t.Fatal(e)
	}
	var s ChargeSchedule
	for _, candidate := range data.Schedules {
		if candidate.Kind == "recurring" {
			s = candidate
		} else if phaseAt(candidate, "2026-09-28").Paused {
			t.Fatal("personal installment was paused")
		}
	}
	if phaseAt(s, "2026-09-21").Paused || !phaseAt(s, "2026-09-28").Paused {
		t.Fatal("assignment Monday did not preserve prior weeks and stop following weeks")
	}
	var amount string
	if e = pool.QueryRow(ctx, `SELECT amount::text FROM truck_charge_phases WHERE truck_id=$1 AND type_id=$2 AND week_start='2026-09-28'`, truck, typ.ID).Scan(&amount); e != nil || amount != "75.00" {
		t.Fatalf("truck fee: %s %v", amount, e)
	}
	run(first)
	again, e := cr.List(ctx, first)
	if e != nil {
		t.Fatal(e)
	}
	for _, candidate := range again.Schedules {
		if candidate.ID == s.ID && candidate.Version != s.Version {
			t.Fatal("handoff was not idempotent")
		}
	}
	// Real fleet assignment path: replacement driver's amount must not override truck terms.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = assignTruck(ctx, tx, truck, next); e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	replacement, e := cr.List(ctx, next)
	if e != nil || !phaseAt(replacement.Schedules[0], ChargeCurrentWeek()).Paused {
		t.Fatal("replacement driver's charges did not stop")
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM truck_charge_phases WHERE truck_id=$1 AND type_id=$2`, truck, typ.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("replacement duplicated truck fee")
	}
	// Re-enabling a current driver fee cannot double-charge; it is paused again.
	rs := replacement.Schedules[0]
	if e = cr.SaveCell(ctx, ChargeCell{DriverID: next, TypeID: typ.ID, WeekStart: ChargeCurrentWeek(), Included: true, Amount: "125.00", TypeVersion: typ.Version, ScheduleID: rs.ID, Version: rs.Version}, actor); e != nil {
		t.Fatal(e)
	}
	replacement, e = cr.List(ctx, next)
	if e != nil || !phaseAt(replacement.Schedules[0], ChargeCurrentWeek()).Paused {
		t.Fatal("recurring write bypassed investor handoff")
	}
}
