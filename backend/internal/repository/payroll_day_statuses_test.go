package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPayrollDayStatusRouting(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	id := func(sql string, args ...any) string {
		t.Helper()
		var value string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	main := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Status Main','status main','cpm',0.75) RETURNING id`)
	team := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Status Team','status team','cpm',0.75) RETURNING id`)
	oldTruck := id(`INSERT INTO trucks(unit_number) VALUES('STATUS-OLD') RETURNING id`)
	newTruck := id(`INSERT INTO trucks(unit_number) VALUES('STATUS-NEW') RETURNING id`)
	exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,'2026-10-05')`, main, team)
	exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at) VALUES($1,$3,'2026-10-01 00:00-04','2026-10-07 00:00-04'),($2,$3,'2026-10-07 00:00-04',NULL)`, oldTruck, newTruck, main)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,day_status) VALUES($1,'2026-10-05','HOME'),($2,'2026-10-05','HOME'),($2,'2026-10-07','IN TRANSIT'),($2,'2026-10-12','RESET')`, main, team)
	exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,day_status,deleted) VALUES($1,'2026-10-05',1,'HOME',false),($1,'2026-10-08',1,'SHOP',true)`, team)
	driverReport := DriverPayWeek{WeekStart: "2026-10-05", Drivers: []DriverPayDriver{{ID: main}}}
	if err := decoratePayDayStatuses(ctx, tx, "driver", &driverReport); err != nil {
		t.Fatal(err)
	}
	want := []PayDayStatus{{"2026-10-05", "HOME"}, {"2026-10-07", "IN TRANSIT"}}
	if !reflect.DeepEqual(driverReport.Drivers[0].DayStatuses, want) {
		t.Fatalf("team statuses, duplicates, deleted rows and week boundaries: %+v", driverReport)
	}
	if summarizePayStatement(driverReport.Drivers[0]).DayStatuses != nil {
		t.Fatal("summary includes day labels")
	}
	investorReport := DriverPayWeek{WeekStart: "2026-10-05", Drivers: []DriverPayDriver{{ID: oldTruck}, {ID: newTruck}}}
	if err := decoratePayDayStatuses(ctx, tx, "investor", &investorReport); err != nil {
		t.Fatal(err)
	}
	for i, d := range investorReport.Drivers {
		if !reflect.DeepEqual(d.DayStatuses, want[i:i+1]) {
			t.Fatalf("dated truck assignment: %+v", investorReport)
		}
	}
	driverReport.Drivers[0].Settlement = &PayrollSettlement{Finalized: true}
	exec(`UPDATE gross_board_entries SET day_status='RESET' WHERE driver_id=$1 AND service_date='2026-10-07'`, team)
	if err := decoratePayDayStatuses(ctx, tx, "driver", &driverReport); err != nil || !reflect.DeepEqual(driverReport.Drivers[0].DayStatuses, want) {
		t.Fatalf("frozen day labels changed: %+v %v", driverReport, err)
	}
	// Old snapshots without metadata gain labels on read, retaining all amounts.
	driverReport.Drivers[0].DayStatuses = nil
	driverReport.Drivers[0].FuelTotal = "25.00"
	if err := decoratePayDayStatuses(ctx, tx, "driver", &driverReport); err != nil || driverReport.Drivers[0].DayStatuses[1].Status != "RESET" || driverReport.Drivers[0].FuelTotal != "25.00" {
		t.Fatalf("legacy display metadata: %+v %v", driverReport, err)
	}
}
