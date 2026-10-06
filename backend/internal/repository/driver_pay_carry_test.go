package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRecurringRemainderProjection(t *testing.T) {
	s := ChargeSchedule{ID: "fee", Kind: "recurring", Name: "Fee", Direction: "charge", StartWeek: "2026-09-28", Eligibility: "calendar", Phases: []ChargePhase{{WeekStart: "2026-09-28", Amount: "100.00"}}, Occurrences: []ChargeOccurrence{{WeekStart: "2026-09-28", ScheduledAmount: "-100.00", Amount: "-60.00", Version: 1}}}
	rows, err := projectCharges(s, "2026-10-12", nil, false)
	if err != nil || len(rows) != 3 || rows[1].Amount != "-140.00" || rows[2].Amount != "-140.00" {
		t.Fatalf("reading must not collect carry: %+v %v", rows, err)
	}
	s.Occurrences[0].WaiveRemainder = true
	rows, err = projectCharges(s, "2026-10-05", nil, false)
	if err != nil || rows[1].Amount != "-100.00" {
		t.Fatalf("weekly reduction: %+v %v", rows, err)
	}
	s.Occurrences[0].WaiveRemainder = false
	s.Phases = append(s.Phases, ChargePhase{WeekStart: "2026-10-05", Paused: true, Amount: "100.00"})
	rows, err = projectCharges(s, "2026-10-05", nil, false)
	if err != nil || rows[1].Amount != "-40.00" {
		t.Fatalf("paused schedule lost debt: %+v %v", rows, err)
	}
	paid := rows[1]
	paid.Amount = "-15.00"
	paid.Version = 1
	s.Occurrences = append(s.Occurrences, paid)
	rows, err = projectCharges(s, "2026-10-12", nil, false)
	if err != nil || rows[2].Amount != "-25.00" {
		t.Fatalf("repeated partial collection: %+v %v", rows, err)
	}
}

func testPayrollRemainders(t *testing.T, pool *pgxpool.Pool, actor string) {
	t.Helper()
	ctx := context.Background()
	var driver string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,is_owner_operator,pay_type,pay_rate) VALUES('Remainder Owner','remainder owner',true,'gross_percentage',88) RETURNING id::text`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	seedDriverPayCosts(t, ctx, pool, driver)
	pay := NewDriverPayRepository(pool)
	read := func(week string) DriverPayDriver {
		t.Helper()
		w, _ := chargeWeek(week)
		r, e := pay.GetDriverWeek(ctx, w, driver)
		if e != nil || len(r.Drivers) != 1 {
			t.Fatalf("read %s: %+v %v", week, r, e)
		}
		return r.Drivers[0]
	}
	save := func(d DriverPayDriver, fuel, toll string) DriverPayEdits {
		t.Helper()
		e := d.Edits
		e.FuelOverride = &fuel
		e.TollOverride = &toll
		s, err := pay.Save(ctx, e, actor)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	w1, w2, w3, w4 := "2026-09-28", "2026-10-05", "2026-10-12", "2026-10-19"
	d := read(w1)
	if d.Edits.Costs.FuelDue != "1089.30" || d.Edits.Costs.TollDue != "12.25" {
		t.Fatalf("sources %+v", d.Edits.Costs)
	}
	// Simulate a saved partial deduction from the previous release.
	if _, err := pool.Exec(ctx, "INSERT INTO driver_pay_weeks(driver_id,week_start,fuel_override,toll_override) VALUES($1,$2::date,-50,-5)", driver, w1); err != nil {
		t.Fatal(err)
	}
	d = read(w2)
	if d.Edits.Costs.FuelCarry != "1039.30" || d.Edits.Costs.TollCarry != "7.25" {
		t.Fatal("legacy overrides lost their remainders")
	}
	d = read(w2)
	if d.Edits.Costs.FuelCarry != "1039.30" || d.Edits.Costs.FuelDue != "2038.30" || d.Edits.Costs.TollCarry != "7.25" {
		t.Fatalf("carry %+v", d.Edits.Costs)
	}
	stale := read(w3)
	save(d, "0.00", "0.00")
	if _, err := pay.Save(ctx, stale.Edits, actor); !errors.Is(err, ErrDriverPayConflict) {
		t.Fatalf("stale cross-week save accepted: %v", err)
	}
	d = read(w3)
	if d.Edits.Costs.FuelDue != "2038.30" || len(d.Loads) != 0 {
		t.Fatalf("empty-week remainder lost: %+v", d)
	}
	save(d, "-500.00", "-7.25")
	d = read(w4)
	if d.Edits.Costs.FuelDue != "1538.30" || d.Edits.Costs.TollDue != "0.00" {
		t.Fatalf("partial carry %+v", d.Edits.Costs)
	}
	prior := read(w1)
	value := "-60.00"
	prior.Edits.FuelOverride = &value
	if _, err := pay.Save(ctx, prior.Edits, actor); err == nil {
		t.Fatal("historical edit changed later collections")
	}
	// A changed tariff does not make old debt disappear or charge new CPM costs.
	if _, err := pool.Exec(ctx, `UPDATE drivers SET pay_type='cpm',pay_rate=0.75 WHERE id=$1`, driver); err != nil {
		t.Fatal(err)
	}
	d = read(w4)
	if d.Edits.Costs.FuelDue != "1538.30" {
		t.Fatal("tariff change lost debt")
	}
	save(d, "-1538.30", "0.00")
	w, _ := chargeWeek("2026-10-26")
	r, err := pay.GetDriverWeek(ctx, w, driver)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Drivers {
		if d.Edits.Costs.FuelCarry != "0.00" {
			t.Fatal("collected twice")
		}
	}

	var recurringDriver string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Recurring Remainder','recurring remainder','cpm',0.75) RETURNING id::text`).Scan(&recurringDriver); err != nil {
		t.Fatal(err)
	}
	driver = recurringDriver
	charges := NewDriverChargeRepository(pool)
	typ, err := charges.SaveType(ctx, ChargeType{Name: "Remainder weekly fee", Direction: "charge", Amount: "80", Eligibility: "calendar"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	err = charges.SaveCell(ctx, ChargeCell{DriverID: driver, TypeID: typ.ID, TypeVersion: typ.Version, Included: true, Amount: "80", WeekStart: w1}, actor)
	if err != nil {
		t.Fatal(err)
	}
	d = read(w1)
	d.Edits.GeneratedCharges[0].Amount = "-30.00"
	if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
		t.Fatal(err)
	}
	d = read(w2)
	if d.Edits.GeneratedCharges[0].Amount != "-130.00" {
		t.Fatal("recurring did not carry")
	}
	d = read(w1)
	d.Edits.GeneratedCharges[0].WaiveRemainder = true
	if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
		t.Fatal(err)
	}
	d = read(w2)
	if d.Edits.GeneratedCharges[0].Amount != "-80.00" {
		t.Fatal("this-week reduction leaked")
	}
	d = read(w1)
	d.Edits.GeneratedCharges[0].WaiveRemainder = false
	if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
		t.Fatal(err)
	}
	d = read(w2)
	if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
		t.Fatal(err)
	}
	d = read(w3)
	if d.Edits.GeneratedCharges[0].Amount != "-80.00" {
		t.Fatal("saving automatic carry failed to collect it")
	}
	d = read(w1)
	d.Edits.GeneratedCharges[0].WaiveRemainder = true
	if _, err = pay.Save(ctx, d.Edits, actor); err == nil {
		t.Fatal("historical waiver changed later collection")
	}
	// Corrections remain possible by undoing the later collection first.
	d = read(w2)
	d.Edits.GeneratedCharges[0].Amount = "0.00"
	if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
		t.Fatal(err)
	}
	d = read(w1)
	d.Edits.GeneratedCharges[0].WaiveRemainder = true
	if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
		t.Fatal(err)
	}
	d = read(w2)
	d.Edits.GeneratedCharges[0].Amount = "-80.00"
	if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
		t.Fatal(err)
	}
	if read(w3).Edits.GeneratedCharges[0].Amount != "-80.00" {
		t.Fatal("correction duplicated debt")
	}
	// Finalized snapshots and both new persistence fields remain protected.
	monday, _ := chargeWeek(w1)
	full, err := pay.Get(ctx, monday)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pay.Settle(ctx, monday, driver, full.Revision, actor, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE driver_charge_occurrences SET waive_remainder=false WHERE schedule_id=(SELECT id FROM driver_charge_schedules WHERE driver_id=$1) AND week_start=$2::date`, driver, w1); err == nil {
		t.Fatal("finalized waiver was editable")
	}
	if _, err = pool.Exec(ctx, `UPDATE driver_pay_cost_collections SET fuel_base=1 WHERE driver_id=$1 AND week_start=$2::date`, driver, w1); err == nil {
		t.Fatal("finalized cost collection was editable")
	}
	if !read(w1).Edits.GeneratedCharges[0].WaiveRemainder {
		t.Fatal("finalized reduction lost")
	}
}
