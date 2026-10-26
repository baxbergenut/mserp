package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs against both fresh and upgraded schemas via TestTruckStatusesDatabase.
func verifyTruckGPSAlerts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, err := pool.Exec(ctx, sql, args...); must(err) }
	ids := make([]string, 12)
	vins := make([]string, 12)
	for n := range ids {
		vins[n] = fmt.Sprintf("1GPS%013d", n)
		must(pool.QueryRow(ctx, `INSERT INTO trucks(unit_number,vin) VALUES($1,$2) RETURNING id`, fmt.Sprintf("GPS-%02d", n), vins[n]).Scan(&ids[n]))
		if n < 10 {
			var driver string
			must(pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.7) RETURNING id`, fmt.Sprintf("GPS Operator %d", n)).Scan(&driver))
			exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id) VALUES($1,$2)`, ids[n], driver)
		}
	}
	five := func(n int, age string) {
		t.Helper()
		exec(`INSERT INTO five_eld_locations(vin,latitude,longitude,reported_at,fetched_at) VALUES($1,40,-80,now()-$2::interval,now()) ON CONFLICT(vin) DO UPDATE SET reported_at=excluded.reported_at`, vins[n], age)
	}
	samsara := func(n int, age string) {
		t.Helper()
		exec(`INSERT INTO samsara_telemetry(vin,vehicle_id,latitude,longitude,gps_at,fetched_at) VALUES($1,$1,40,-80,now()-$2::interval,now())`, vins[n], age)
	}
	five(0, "8 days") // Fresh fetch must not hide old observations.
	five(2, "1 hour")
	five(3, "9 days")
	samsara(3, "2 hours")
	five(4, "-1 day") // Future dates cannot establish freshness.
	five(5, "1 hour")
	exec(`INSERT INTO five_eld_sync_state(singleton,ambiguous_vins) VALUES(true,ARRAY[$1]) ON CONFLICT(singleton) DO UPDATE SET ambiguous_vins=ARRAY[$1]`, vins[5])
	five(6, "1 hour")
	exec(`INSERT INTO trucks(unit_number,vin) VALUES('GPS-DUPLICATE',lower($1))`, vins[6])
	samsara(7, "9 days")
	five(8, "167 hours 59 minutes")
	five(9, "168 hours 1 minute")
	five(10, "20 days")
	five(11, "20 days")
	exec(`UPDATE trucks SET status='maintenance',status_reason='Test shop' WHERE id=$1`, ids[11])
	fleet := NewFleetRepository(pool)
	want := []string{"overdue", "missing", "", "", "missing", "missing", "missing", "overdue", "", "overdue", "", ""}
	var before int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM fleet_status_history`).Scan(&before))
	for n, id := range ids {
		truck, err := fleet.GetTruck(ctx, id)
		must(err)
		if truck.GPSAlert != want[n] {
			t.Fatalf("GPS-%02d alert %q; want %q", n, truck.GPSAlert, want[n])
		}
		if n < 10 && truck.OperatingStatus != "in_use" {
			t.Fatal("GPS changed truck status", truck)
		}
		if n == 3 && (truck.GPSSource == nil || *truck.GPSSource != "samsara") {
			t.Fatal("freshest provider not selected", truck)
		}
	}
	page, err := fleet.ListTrucksPage(ctx, Pagination{Page: 2, PageSize: 1}, "GPS-", TruckFilters{GPSAlert: "overdue"})
	must(err)
	if page.Total != 3 || len(page.Items) != 1 || page.Items[0].UnitNumber != "GPS-07" || page.GPSAlertCounts["overdue"] != 3 || page.GPSAlertCounts["missing"] != 4 || page.StatusCounts["in_use"] != 10 {
		t.Fatalf("GPS pagination/counts: %+v", page)
	}
	empty, err := fleet.ListTrucksPage(ctx, Pagination{Page: 1, PageSize: 25}, "GPS-", TruckFilters{Status: "shop", GPSAlert: "overdue"})
	must(err)
	if empty.Total != 0 {
		t.Fatal("restricted truck flagged", empty)
	}
	searched, err := fleet.ListTrucksPage(ctx, Pagination{Page: 1, PageSize: 25}, "GPS-00")
	must(err)
	if searched.GPSAlertCounts["overdue"] != 1 || searched.GPSAlertCounts["missing"] != 0 {
		t.Fatal("search counts", searched)
	}
	five(0, "1 minute")
	updated, err := fleet.GetTruck(ctx, ids[0])
	must(err)
	if updated.GPSAlert != "" || updated.OperatingStatus != "in_use" {
		t.Fatal("fresh report did not clear alert", updated)
	}
	// Unavailable/ambiguous Samsara cannot mask an old trusted Five ELD report.
	exec(`UPDATE samsara_telemetry SET available=false WHERE vin=$1`, vins[3])
	updated, err = fleet.GetTruck(ctx, ids[3])
	must(err)
	if updated.GPSAlert != "overdue" || updated.GPSSource == nil || *updated.GPSSource != "five_eld" {
		t.Fatal("unavailable provider used", updated)
	}
	var after int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM fleet_status_history`).Scan(&after))
	if before != after {
		t.Fatal("GPS reads/updates wrote lifecycle history", before, after)
	}
}
