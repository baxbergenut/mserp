package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func testChargeMatrix(t *testing.T, pool *pgxpool.Pool, actor string) {
	t.Helper()
	ctx := context.Background()
	repo := NewDriverChargeRepository(pool)
	pay := NewDriverPayRepository(pool)
	week := ChargeCurrentWeek()
	date, _ := chargeWeek(week)
	next := date.AddDate(0, 0, 7).Format(time.DateOnly)
	var driver string
	if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Matrix Driver','matrix driver','cpm',0.75) RETURNING id::text`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	createType := func(name, eligibility string, amounts ...string) ChargeType {
		t.Helper()
		typ, err := repo.SaveType(ctx, ChargeType{Name: name, Direction: "charge", Amounts: amounts, Eligibility: eligibility}, actor)
		if err != nil {
			t.Fatal(err)
		}
		return typ
	}
	max := createType("Maximum insurance", "loads", "300", "350")
	min := createType("Minimum insurance", "no_loads", "150", "175")
	cell := func(typ ChargeType) ChargeCell {
		t.Helper()
		data, err := repo.List(ctx, driver)
		if err != nil {
			t.Fatal(err)
		}
		c := ChargeCell{DriverID: driver, TypeID: typ.ID, WeekStart: week, Included: true, Amount: typ.Amount, TypeVersion: typ.Version}
		for _, s := range data.Schedules {
			if s.TypeID != nil && *s.TypeID == typ.ID {
				c.ScheduleID = s.ID
				c.Version = s.Version
				c.Amount = phaseAt(s, week).Amount
			}
		}
		return c
	}
	save := func(c ChargeCell) {
		t.Helper()
		if err := repo.SaveCell(ctx, c, actor); err != nil {
			t.Fatal(err)
		}
	}
	stale := cell(max)
	save(stale)
	if err := repo.SaveCell(ctx, stale, actor); !errors.Is(err, ErrChargeConflict) {
		t.Fatal("duplicate matrix write must conflict", err)
	}
	save(cell(min))
	get := func() DriverPayEdits {
		t.Helper()
		report, err := pay.Get(ctx, date)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range report.Drivers {
			if d.ID == driver {
				return d.Edits
			}
		}
		t.Fatal("missing matrix driver")
		return DriverPayEdits{}
	}
	expectAmount := func(name, amount string) {
		t.Helper()
		edits := get()
		if len(edits.GeneratedCharges) != 1 || edits.GeneratedCharges[0].Name != name || edits.GeneratedCharges[0].Amount != amount {
			t.Fatalf("eligibility: %+v", edits.GeneratedCharges)
		}
	}
	expectAmount(min.Name, "-150.00")
	if _, err := pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,$2::date,'HOME')`, driver, week); err != nil {
		t.Fatal(err)
	}
	expectAmount(min.Name, "-150.00")
	if _, err := pool.Exec(ctx, `UPDATE gross_board_entries SET day_status='',load_number='unmatched plan' WHERE driver_id=$1`, driver); err != nil {
		t.Fatal(err)
	}
	expectAmount(max.Name, "-300.00")
	c := cell(max)
	c.Amount = "350"
	save(c)
	expectAmount(max.Name, "-350.00")
	c = cell(max)
	c.Amount = "349"
	if err := repo.SaveCell(ctx, c, actor); err == nil {
		t.Fatal("unlisted amount accepted")
	}
	c = cell(max)
	c.Included = false
	save(c)
	if len(get().GeneratedCharges) != 0 {
		t.Fatal("unchecked fee still charged")
	}
	c = cell(max)
	save(c)
	expectAmount(max.Name, "-350.00")
	c = cell(max)
	c.WeekStart = next
	c.Amount = "300"
	save(c)
	expectAmount(max.Name, "-350.00")
	stalePay := get()
	staleCell := cell(max)
	max.Eligibility = "calendar"
	var err error
	max, err = repo.SaveType(ctx, max, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pay.Save(ctx, stalePay, actor); !errors.Is(err, ErrChargeConflict) {
		t.Fatal("stale type payroll write accepted", err)
	}
	if err = repo.SaveCell(ctx, staleCell, actor); !errors.Is(err, ErrChargeConflict) {
		t.Fatal("stale type matrix write accepted", err)
	}
	// Options never silently rewrite a driver's selected amount.
	max.Amounts = []string{"400"}
	max, err = repo.SaveType(ctx, max, actor)
	if err != nil {
		t.Fatal(err)
	}
	expectAmount(max.Name, "-350.00")
	// A saved skip stays explicit even after eligibility changes.
	edits := get()
	edits.GeneratedCharges[0].Amount = "0"
	if _, err = pay.Save(ctx, edits, actor); err != nil {
		t.Fatal(err)
	}
	max.Eligibility = "no_loads"
	max, err = repo.SaveType(ctx, max, actor)
	if err != nil {
		t.Fatal(err)
	}
	expectAmount(max.Name, "0.00")
	// New recurring creation cannot override the type's eligibility.
	draft, err := repo.Draft(ctx, ChargeCreate{DriverIDs: []string{driver}, TypeID: min.ID, Kind: "recurring", Amount: "150", Eligibility: "loads", StartWeek: week})
	if err != nil || len(draft) != 12 || draft[0].Amount != "-150.00" {
		t.Fatal("draft ignored type rule", err)
	}
	if _, err = repo.SaveType(ctx, ChargeType{Name: "Duplicate", Direction: "charge", Amounts: []string{"1", "1.00"}}, actor); err == nil {
		t.Fatal("duplicate options accepted")
	}
}

func TestChargeTypeEffectiveEligibility(t *testing.T) {
	s := ChargeSchedule{ID: "fee", Kind: "recurring", Name: "fee", Direction: "charge", StartWeek: "2026-09-07", Eligibility: "calendar", Version: 1, TypeVersion: 3,
		Phases:    []ChargePhase{{WeekStart: "2026-09-07", Amount: "150"}},
		TypeRules: []ChargeTypeRule{{WeekStart: "2026-09-14", Eligibility: "no_loads"}, {WeekStart: "2026-09-28", Eligibility: "loads"}}}
	rows, err := projectCharges(s, "2026-10-05", map[string]bool{"2026-09-14": true, "2026-09-28": true}, false)
	if err != nil || len(rows) != 3 {
		t.Fatalf("dated rules: %+v %v", rows, err)
	}
	for i, w := range []string{"2026-09-07", "2026-09-21", "2026-09-28"} {
		if rows[i].WeekStart != w || rows[i].TypeVersion != 3 {
			t.Fatalf("wrong rule: %+v", rows)
		}
	}
	s.Occurrences = []ChargeOccurrence{{ScheduleID: s.ID, WeekStart: "2026-09-14", Amount: "0.00", ScheduledAmount: "-150.00", Overridden: true}}
	rows, err = projectCharges(s, "2026-10-05", nil, false)
	if err != nil || len(rows) != 5 || rows[1].Amount != "0.00" || rows[4].Amount != "-150.00" {
		t.Fatalf("saved skip lost: %+v %v", rows, err)
	}
}
