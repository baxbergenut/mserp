package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPriorWeekInstallmentDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		repo := NewDriverChargeRepository(pool)
		pay := NewDriverPayRepository(pool)
		current, _ := chargeWeek(ChargeCurrentWeek())
		prior := current.AddDate(0, 0, -7)
		var driver string
		if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate)
 VALUES('Installment Driver','installment driver','cpm',0.75) RETURNING id::text`).Scan(&driver); err != nil {
			t.Fatal(err)
		}
		input := ChargeCreate{DriverIDs: []string{driver}, Kind: "installment", Name: "Advance",
			Total: "200", Amount: "100", StartWeek: prior.Format(time.DateOnly), Eligibility: "loads"}
		rows, err := repo.Draft(ctx, input)
		if err != nil || len(rows) != 2 || rows[0].WeekStart != current.Format(time.DateOnly) {
			t.Fatalf("load-only preview should skip a completed week without loads: %+v, %v", rows, err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number)
 VALUES($1,$2::date,'INSTALLMENT-LOAD')`, driver, input.StartWeek); err != nil {
			t.Fatal(err)
		}
		rows, err = repo.Draft(ctx, input)
		if err != nil || len(rows) != 2 || rows[0].WeekStart != input.StartWeek || rows[0].Amount != "-100.00" {
			t.Fatalf("load-only preview should include the prior week with a load: %+v, %v", rows, err)
		}
		ids, err := repo.Create(ctx, input, actor)
		if err != nil {
			t.Fatal(err)
		}
		report, err := pay.GetDriverWeek(ctx, prior, driver)
		if err != nil || len(report.Drivers) != 1 {
			t.Fatalf("prior-week Driver Pay: %+v, %v", report, err)
		}
		charges := report.Drivers[0].Edits.GeneratedCharges
		if len(charges) != 1 || charges[0].ScheduleID != ids[0] || charges[0].Amount != "-100.00" || charges[0].ConfirmedAt != nil {
			t.Fatalf("expected one unconfirmed prior-week installment: %+v", charges)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report)
 VALUES($1,$2::date,'{}')`, driver, input.StartWeek); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Draft(ctx, input); err == nil || !strings.Contains(err.Error(), "Reopen the Driver Pay statement") {
			t.Fatalf("expected finalized week to block preview, got %v", err)
		}
		if _, err = repo.Create(ctx, input, actor); err == nil || !strings.Contains(err.Error(), "Reopen the Driver Pay statement") {
			t.Fatalf("expected finalized week to block creation, got %v", err)
		}
		input.StartWeek = prior.AddDate(0, 0, -7).Format(time.DateOnly)
		if _, err = repo.Create(ctx, input, actor); err == nil || !strings.Contains(err.Error(), "Choose last week") {
			t.Fatalf("expected older start to be rejected, got %v", err)
		}
	})
}

func TestDriverChargesDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		repo := NewDriverChargeRepository(pool)
		pay := NewDriverPayRepository(pool)
		week := ChargeCurrentWeek()
		monday, _ := chargeWeek(week)
		next := monday.AddDate(0, 0, 7).Format(time.DateOnly)
		var driver, other string
		for i, p := range []*string{&driver, &other} {
			if err := pool.QueryRow(ctx, "INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.75) RETURNING id::text", fmt.Sprintf("Charge Driver %d", i)).Scan(p); err != nil {
				t.Fatal(err)
			}
		}
		var report DriverPayWeek
		var edits, saved DriverPayEdits
		var data ChargeData
		// An installment is not collected by viewing or saving payroll.
		plans, err := repo.Create(ctx, ChargeCreate{DriverIDs: []string{driver}, Kind: "installment", Name: "Advance", Total: "650", Amount: "100", StartWeek: week, Eligibility: "calendar"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		projection, err := repo.Preview(ctx, plans[0])
		if err != nil || len(projection) != 7 || projection[6].Amount != "-50.00" {
			t.Fatal("installment preview", err)
		}
		selected := projection[0]
		selected.Amount = "-60.00"
		report, err = pay.Get(ctx, monday)
		if err != nil {
			t.Fatal(err)
		}
		edits = report.Drivers[0].Edits
		for i := range edits.GeneratedCharges {
			if edits.GeneratedCharges[i].ScheduleID == plans[0] {
				edits.GeneratedCharges[i].Amount = "-60.00"
			}
		}
		saved, err = pay.Save(ctx, edits, actor)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range saved.GeneratedCharges {
			if o.ScheduleID == plans[0] {
				selected = o
			}
		}
		confirmation := ChargeConfirm{DriverID: driver, WeekStart: week, Rows: []ChargeOccurrence{selected}}
		var wg sync.WaitGroup
		outcomes := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); outcomes <- repo.Confirm(ctx, confirmation, actor, false) }()
		}
		wg.Wait()
		close(outcomes)
		for e := range outcomes {
			if e != nil {
				t.Fatal("concurrent confirmation", e)
			}
		}
		if err = repo.Confirm(ctx, confirmation, actor, false); err != nil {
			t.Fatal("confirmation retry", err)
		}
		data, err = repo.List(ctx, driver)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range data.Schedules {
			if s.ID == plans[0] {
				if s.Confirmed != "60.00" || s.Remaining != "590.00" {
					t.Fatalf("balance %+v", s)
				}
				selected = s.Occurrences[0]
			}
		}
		if err = repo.Confirm(ctx, ChargeConfirm{DriverID: other, WeekStart: week, Rows: []ChargeOccurrence{selected}}, actor, false); err == nil {
			t.Fatal("cross-driver confirmation accepted")
		}
		report, err = pay.Get(ctx, monday)
		if err != nil {
			t.Fatal(err)
		}
		edits = report.Drivers[0].Edits
		for i := range edits.GeneratedCharges {
			if edits.GeneratedCharges[i].ScheduleID == plans[0] {
				edits.GeneratedCharges[i].Amount = "-80.00"
			}
		}
		if _, err = pay.Save(ctx, edits, actor); err == nil {
			t.Fatal("confirmed amount editable")
		}
		confirmation.Rows = []ChargeOccurrence{selected}
		confirmation.Reason = "Correction"
		if err = repo.Confirm(ctx, confirmation, actor, true); err != nil {
			t.Fatal("reopen", err)
		}
		projection, err = repo.Preview(ctx, plans[0])
		if err != nil || len(projection) != 7 || projection[6].Amount != "-50.00" || projection[1].Amount != "-140.00" {
			t.Fatal("reopen projection", err)
		}
		data, err = repo.List(ctx, driver)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range data.Schedules {
			if s.ID == plans[0] && s.Confirmed != "0.00" {
				t.Fatal("reopen did not restore balance")
			}
		}
		// A rejected oversized weekly edit must roll back both the ledger and payroll version.
		report, err = pay.Get(ctx, monday)
		if err != nil {
			t.Fatal(err)
		}
		edits = report.Drivers[0].Edits
		for i := range edits.GeneratedCharges {
			if edits.GeneratedCharges[i].ScheduleID == plans[0] {
				edits.GeneratedCharges[i].Amount = "-700.00"
			}
		}
		if _, err = pay.Save(ctx, edits, actor); err == nil {
			t.Fatal("overallocated edit accepted")
		}
		report, err = pay.Get(ctx, monday)
		if err != nil {
			t.Fatal(err)
		}
		if report.Drivers[0].Edits.Version != edits.Version {
			t.Fatal("failed edit advanced payroll version")
		}
		for _, o := range report.Drivers[0].Edits.GeneratedCharges {
			if o.ScheduleID == plans[0] && o.Amount != "-60.00" {
				t.Fatal("failed edit changed amount")
			}
		}
		// Deletion restriction and atomic effective deactivation.
		fleet := NewFleetRepository(pool)
		if err = fleet.DeleteDriver(ctx, driver); err == nil {
			t.Fatal("deleted financial history")
		}
		input := DriverInput{FullName: "Charge Driver 0", PayType: "cpm", PayRate: 0.75, Active: false, ChargePauseWeek: next, ChargeActor: actor}
		if _, err = fleet.UpdateDriver(ctx, driver, input); err != nil {
			t.Fatal("deactivation", err)
		}
		projection, err = repo.Preview(ctx, plans[0])
		if err != nil || len(projection) != 2 || projection[1].Amount != "-40.00" {
			t.Fatal("pause did not stop installments", err, len(projection))
		}
		input.Active = true
		if _, err = fleet.UpdateDriver(ctx, driver, input); err != nil {
			t.Fatal(err)
		}
		projection, _ = repo.Preview(ctx, plans[0])
		if len(projection) != 2 || projection[1].Amount != "-40.00" {
			t.Fatal("reactivation resumed charges")
		}

		testChargeTypeDeletion(t, pool, actor, driver)
		testPayrollRemainders(t, pool, actor)
	})
}
