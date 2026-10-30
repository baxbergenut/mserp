package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPayrollAssignmentLabels(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) string {
		t.Helper()
		var value string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,roster_start_week) VALUES('Final Week','final week','cpm',0.75,'2026-10-05') RETURNING id`)
	truck := id(`INSERT INTO trucks(unit_number) VALUES('FINAL-3785') RETURNING id`)
	exec(`UPDATE truck_ownership_history SET assigned_at='2026-10-01 00:00-04',start_known=true WHERE truck_id=$1`, truck)
	dispatcher := id(`INSERT INTO dispatchers(full_name,normalized_name) VALUES('Historical Dispatch','historical dispatch') RETURNING id`)
	exec(`DELETE FROM driver_dispatcher_assignments WHERE driver_id=$1`, driver)
	exec(`INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_id,dispatcher_name,assigned_at,unassigned_at,start_known) VALUES
 ($1,$2,'Historical Dispatch','2026-10-05 00:00-04','2026-10-21 00:00-04',true),
 ($1,NULL,'Unassigned','2026-10-21 00:00-04',NULL,true)`, driver, dispatcher)
	exec(`INSERT INTO truck_driver_assignments(driver_id,truck_id,assigned_at,unassigned_at) VALUES($1,$2,'2026-10-05 00:00-04','2026-10-23 00:00-04')`, driver, truck)
	exec(`UPDATE drivers SET active=false,status='terminated',termination_date='2026-10-21' WHERE id=$1`, driver)
	load := DriverPayLoad{Date: "2026-10-20", SourceDate: "2026-10-20", LoadNumber: "Final load", Fee: "75.00"}
	report := DriverPayWeek{WeekStart: "2026-10-19", Drivers: []DriverPayDriver{{ID: driver, EmploymentType: "unassigned", Loads: []DriverPayLoad{load}, FuelTotal: "25.00", TollTotal: "10.00"}}}
	check := func(kind string, report *DriverPayWeek) {
		t.Helper()
		if err := decoratePayAssignments(ctx, tx, kind, report); err != nil {
			t.Fatal(err)
		}
		got := report.Drivers[0]
		if got.TruckID != truck || got.TruckUnit != "FINAL-3785" || got.DispatcherID != dispatcher || got.DispatcherName != "Historical Dispatch" {
			t.Fatalf("%s final-week labels: %+v", kind, got)
		}
	}
	check("driver", &report)
	if report.Drivers[0].EmploymentType != "unassigned" || len(report.Drivers[0].DisplayEmploymentTypes) != 1 || report.Drivers[0].DisplayEmploymentTypes[0] != "company" {
		t.Fatal("dated display classification must not change financial classification", report.Drivers[0].DisplayEmploymentTypes)
	}
	if report.Drivers[0].FuelTotal != "25.00" || report.Drivers[0].TollTotal != "10.00" || report.Drivers[0].Loads[0].Fee != "75.00" {
		t.Fatal("labels changed financial values")
	}
	compact := summarizePayStatement(report.Drivers[0])
	if compact.TruckUnit != "FINAL-3785" || compact.DispatcherID != dispatcher {
		t.Fatal("compact labels lost")
	}
	load.SourceDriverID = driver
	investor := DriverPayWeek{WeekStart: "2026-10-19", Drivers: []DriverPayDriver{{ID: truck, TruckID: truck, TruckUnit: "FINAL-3785", Loads: []DriverPayLoad{load}, OperatingDrivers: []PayPerson{{ID: driver}}}}}
	check("investor", &investor)
	// No-load final statements retain the assignments held earlier in the week.
	report.Drivers[0].Loads = nil
	investor.Drivers[0].Loads = nil
	check("driver", &report)
	check("investor", &investor)
	// A later-week carried statement does not resurrect an ended assignment.
	report.WeekStart = "2026-10-26"
	if err := decoratePayAssignments(ctx, tx, "driver", &report); err != nil {
		t.Fatal(err)
	}
	if report.Drivers[0].TruckID != "" || report.Drivers[0].DispatcherID != "" {
		t.Fatal("historical assignment leaked into later week")
	}
	// Transferred work uses its original source day and reviewed dispatcher credit.
	load.Date = "2026-10-27"
	load.TransferVersion = 1
	load.CreditedDispatcherID = dispatcher
	load.CreditedDispatcherName = "Reviewed credit"
	report.Drivers[0].Loads = []DriverPayLoad{load}
	if err := decoratePayAssignments(ctx, tx, "driver", &report); err != nil {
		t.Fatal(err)
	}
	if report.Drivers[0].TruckID != truck || report.Drivers[0].DispatcherName != "Reviewed credit" {
		t.Fatal("transfer ignored source date or saved credit")
	}
	// Genuine unassigned work stays visible instead of borrowing a previous truck.
	load.SourceDate = "2026-10-24"
	load.TransferVersion = 0
	report.Drivers[0].Loads = []DriverPayLoad{load}
	if err := decoratePayAssignments(ctx, tx, "driver", &report); err != nil {
		t.Fatal(err)
	}
	if report.Drivers[0].TruckID != "" || report.Drivers[0].DispatcherID != "" {
		t.Fatal("unassigned load borrowed historical labels")
	}
	report.Drivers[0].TruckUnit = "Frozen truck"
	report.Drivers[0].DispatcherName = "Frozen dispatcher"
	report.Drivers[0].Settlement = &PayrollSettlement{Finalized: true}
	if err := decoratePayAssignments(ctx, tx, "driver", &report); err != nil {
		t.Fatal(err)
	}
	if report.Drivers[0].TruckUnit != "Frozen truck" || report.Drivers[0].DispatcherName != "Frozen dispatcher" {
		t.Fatal("frozen labels changed")
	}
}
