package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func testBackdatedChargeMatrix(t *testing.T, pool *pgxpool.Pool, actor string) {
	t.Helper()
	ctx := context.Background()
	repo := NewDriverChargeRepository(pool)
	pay := NewDriverPayRepository(pool)
	current := ChargeCurrentWeek()
	date, _ := chargeWeek(current)
	past := date.AddDate(0, 0, -14).Format(time.DateOnly)
	middle := date.AddDate(0, 0, -7).Format(time.DateOnly)
	var driver string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Backdated Driver','backdated driver','cpm',0.75) RETURNING id::text`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	typ, err := repo.SaveType(ctx, ChargeType{Name: "Backdated idle fee", Direction: "charge", Amounts: []string{"150", "175"}, Eligibility: "no_loads"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	cell := func(week string) ChargeCell {
		t.Helper()
		data, e := repo.List(ctx, driver)
		if e != nil {
			t.Fatal(e)
		}
		c := ChargeCell{DriverID: driver, TypeID: typ.ID, TypeVersion: typ.Version, WeekStart: week, Included: true, Amount: typ.Amount}
		for _, s := range data.Schedules {
			if s.StartWeek <= week && (s.EndWeek == nil || *s.EndWeek >= week) {
				c.ScheduleID = s.ID
				c.Version = s.Version
				c.Amount = phaseAt(s, week).Amount
			}
		}
		return c
	}
	save := func(c ChargeCell) {
		t.Helper()
		if e := repo.SaveCell(ctx, c, actor); e != nil {
			t.Fatal(e)
		}
	}
	get := func(week string) DriverPayEdits {
		t.Helper()
		d, _ := chargeWeek(week)
		report, e := pay.Get(ctx, d)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range report.Drivers {
			if v.ID == driver {
				return v.Edits
			}
		}
		return DriverPayEdits{}
	}
	amount := func(week, want string) {
		t.Helper()
		edits := get(week)
		if len(edits.GeneratedCharges) != 1 || edits.GeneratedCharges[0].Amount != want {
			t.Fatalf("week %s: %+v, want %s", week, edits.GeneratedCharges, want)
		}
	}
	save(cell(current))
	original := cell(current)
	edits := get(current)
	edits.GeneratedCharges[0].Amount = "-140"
	if _, err = pay.Save(ctx, edits, actor); err != nil {
		t.Fatal(err)
	}
	save(cell(past)) // Backfill before an existing assignment with an override.
	amount(past, "-150.00")
	amount(middle, "-150.00")
	amount(current, "-140.00")
	if cell(current).ScheduleID != original.ScheduleID {
		t.Fatal("backfill replaced existing assignment")
	}
	data, err := repo.List(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Schedules) != 2 {
		t.Fatal("backfill did not create a separate historical period")
	}
	for _, s := range data.Schedules {
		if s.StartWeek == past && (s.EndWeek == nil || *s.EndWeek != middle || s.Eligibility != "no_loads") {
			t.Fatal("wrong backfill interval or eligibility", s)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number) VALUES($1,$2::date,'unmatched')`, driver, past); err != nil {
		t.Fatal(err)
	}
	if len(get(past).GeneratedCharges) != 0 {
		t.Fatal("backdated no-load fee charged a load week")
	}
	if _, err = pool.Exec(ctx, `UPDATE gross_board_entries SET load_number='' WHERE driver_id=$1`, driver); err != nil {
		t.Fatal(err)
	}
	c := cell(middle)
	c.Amount = "175"
	save(c)
	amount(past, "-150.00")
	amount(middle, "-175.00")
	edits = get(middle)
	edits.GeneratedCharges[0].Amount = "-160"
	if _, err = pay.Save(ctx, edits, actor); err != nil {
		t.Fatal(err)
	}
	c = cell(past)
	c.Included = false
	save(c) // A later phase's override is outside this change.
	if len(get(past).GeneratedCharges) != 0 {
		t.Fatal("backdated pause failed")
	}
	amount(middle, "-160.00")
	amount(current, "-140.00")
	c = cell(middle)
	c.Amount = "150"
	if err = repo.SaveCell(ctx, c, actor); err == nil {
		t.Fatal("overwrote a protected weekly override")
	}
	c = cell(past)
	save(c)
	amount(past, "-150.00")
	c = cell(past)
	c.WeekStart = date.AddDate(0, 0, -13).Format(time.DateOnly)
	if err = repo.SaveCell(ctx, c, actor); err == nil {
		t.Fatal("non-Monday accepted")
	}
	// Every read order sees the same historical and current amounts.
	amount(current, "-140.00")
	amount(past, "-150.00")
	amount(middle, "-160.00")
}
