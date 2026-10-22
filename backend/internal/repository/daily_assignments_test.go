package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs under mserp_app against both the full schema and incremental migrations.
func verifyDailyAssignments(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	fleet := NewFleetRepository(pool)
	first, err := fleet.CreateDispatcher(ctx, DispatcherInput{FullName: "Daily First", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := fleet.CreateDispatcher(ctx, DispatcherInput{FullName: "Daily Second", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	truck, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "DAILY", Active: true, Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	input := DriverInput{FullName: "Daily Driver", PayType: "cpm", PayRate: .75, Active: true, DispatcherID: &first.ID, TruckID: &truck.ID, AssignmentWeek: "2026-09-21", EscrowAmount: "0"}
	driver, err := fleet.CreateDriver(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.DispatcherID = &second.ID
	input.AssignmentWeek = "2026-09-30"
	if _, err = fleet.UpdateDriver(ctx, driver.ID, input); err != nil {
		t.Fatal("Wednesday transfer", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,miles) VALUES($1,'2026-09-28','DAILY-A',1000,900,100),($1,'2026-09-30','DAILY-B',2000,1800,200),($1,'2026-10-04','DAILY-C',3000,2700,300)`, driver.ID); err != nil {
		t.Fatal(err)
	}
	week, _ := time.Parse(time.DateOnly, "2026-09-28")
	check := func(firstDays int) {
		t.Helper()
		board, err := NewGrossBoardRepository(pool).Get(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range board.Drivers {
			if d.ID != driver.ID {
				continue
			}
			found = true
			if len(d.Assignments) != 7 {
				t.Fatalf("daily assignment count: %+v", d)
			}
			for i, a := range d.Assignments {
				want := second.ID
				if i < firstDays {
					want = first.ID
				}
				if a.DispatcherID != want || a.TruckID != truck.ID {
					t.Fatalf("day %d: %+v", i, a)
				}
			}
		}
		if !found {
			t.Fatal("driver missing")
		}
		pay, err := NewDriverPayRepository(pool).GetDriverWeek(ctx, week, driver.ID)
		if err != nil || len(pay.Drivers) != 1 {
			t.Fatalf("pay: %+v %v", pay, err)
		}
		d := pay.Drivers[0]
		if len(d.Loads) != 3 || len(d.Dispatchers) != 2 || d.DispatcherName != "Daily First / Daily Second" {
			t.Fatalf("pay lost loads or dispatcher metadata: %+v", d)
		}
		for _, id := range []string{first.ID, second.ID} {
			filtered := PaginatePay(pay, PayPageQuery{DispatcherID: id})
			if len(filtered.Drivers) != 1 || len(filtered.Drivers[0].Loads) != 3 {
				t.Fatalf("dispatcher filter lost full statement: %+v", filtered)
			}
		}
	}
	check(2)
	// Same-date corrections leave zero-length history, without extra board groups.
	input.DispatcherID = &first.ID
	if _, err = fleet.UpdateDriver(ctx, driver.ID, input); err != nil {
		t.Fatal(err)
	}
	input.DispatcherID = &second.ID
	if _, err = fleet.UpdateDriver(ctx, driver.ID, input); err != nil {
		t.Fatal(err)
	}
	check(2)
	// The containing Monday's finalization protects a Wednesday mutation.
	if _, err = pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-09-28','{}')`, driver.ID); err != nil {
		t.Fatal(err)
	}
	input.DispatcherID = &first.ID
	input.AssignmentWeek = "2026-10-01"
	if _, err = fleet.UpdateDriver(ctx, driver.ID, input); err == nil {
		t.Fatal("midweek change bypassed finalized driver payroll")
	}
	if _, err = pool.Exec(ctx, `UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, driver.ID); err != nil {
		t.Fatal(err)
	}
	// A Sunday transfer changes that day only; prior loads remain visible.
	input.DispatcherID = &first.ID
	input.AssignmentWeek = "2026-09-30"
	if _, err = fleet.UpdateDriver(ctx, driver.ID, input); err != nil {
		t.Fatal(err)
	}
	input.DispatcherID = &second.ID
	input.AssignmentWeek = "2026-10-04"
	if _, err = fleet.UpdateDriver(ctx, driver.ID, input); err != nil {
		t.Fatal(err)
	}
	check(6)
	// Truck-side displacement must protect investor payroll in the same week.
	if _, err = pool.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report) SELECT id,'2026-09-28',owner_id,'{}',true,'{}' FROM trucks WHERE id=$1`, truck.ID); err != nil {
		t.Fatal(err)
	}
	other, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Daily Replacement", PayType: "cpm", Active: true, AssignmentWeek: "2026-09-21", EscrowAmount: "0"})
	if err != nil {
		t.Fatal(err)
	}
	truckInput := TruckInput{UnitNumber: truck.UnitNumber, Status: "assigned", Active: true, DriverID: &other.ID, AssignmentWeek: "2026-10-01"}
	if _, err = fleet.UpdateTruck(ctx, truck.ID, truckInput); err == nil {
		t.Fatal("midweek displacement bypassed finalized investor payroll")
	}
	if _, err = pool.Exec(ctx, `UPDATE investor_pay_weeks SET finalized=false WHERE truck_id=$1`, truck.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = fleet.UpdateTruck(ctx, truck.ID, truckInput); err != nil {
		t.Fatal("Thursday truck assignment", err)
	}
	board, err := NewGrossBoardRepository(pool).Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range board.Drivers {
		if d.ID == driver.ID {
			for i, a := range d.Assignments {
				want := truck.ID
				if i >= 3 {
					want = ""
				}
				if a.TruckID != want {
					t.Fatalf("displaced daily truck: %+v", d)
				}
			}
		}
	}
	// A new roster may start on any day too.
	_, err = fleet.CreateDriver(ctx, DriverInput{FullName: "Daily New Driver", PayType: "cpm", Active: true, AssignmentWeek: "2026-10-03", EscrowAmount: "0"})
	if err != nil {
		t.Fatal("Saturday roster", err)
	}
}
