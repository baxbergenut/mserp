package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Legacy fixtures intentionally omit older migrations; defer this dependent
// read view until their extra-slot and team tables have been installed.
func withoutClustieLoadAssignments(t *testing.T, source string) string {
	t.Helper()
	b, err := os.ReadFile("../../sql/167_clustie_load_assignments.sql")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Replace(source, strings.ReplaceAll(string(b), "\r\n", "\n"), "", 1)
}

func applyClustieLoadAssignments(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	b, err := os.ReadFile("../../sql/167_clustie_load_assignments.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, string(b)); err != nil {
		t.Fatal(err)
	}
}

func TestClustieLoadAssignmentsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		id := func(q string, args ...any) string {
			t.Helper()
			var v string
			if err := pool.QueryRow(ctx, q, args...).Scan(&v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES('Local Mark','local mark','gross_percentage',25,'2026-10-01') RETURNING id::text`)
		dispatcher := id(`INSERT INTO dispatchers(full_name,normalized_name) VALUES('Local Dispatcher','local dispatcher') RETURNING id::text`)
		exec(`UPDATE drivers SET dispatcher_id=$2 WHERE id=$1`, driver, dispatcher)
		exec(`UPDATE driver_dispatcher_assignments SET assigned_at='2026-10-01 00:00-04' WHERE driver_id=$1 AND unassigned_at IS NULL`, driver)
		investor := id(`INSERT INTO investors(full_name) VALUES('Local Investor') RETURNING id::text`)
		oldTruck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('LOCAL-065',$1) RETURNING id::text`, investor)
		companyTruck := id(`INSERT INTO trucks(unit_number) VALUES('LOCAL-028') RETURNING id::text`)
		exec(`UPDATE truck_ownership_history SET assigned_at='2026-10-01',start_known=true WHERE truck_id=ANY($1::uuid[])`, []string{oldTruck, companyTruck})
		exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-10-19',90)`, oldTruck, investor)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at) VALUES($1,$2,'2026-10-12 00:00-04','2026-10-24 00:00-04')`, oldTruck, driver)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-10-24 00:00-04')`, companyTruck, driver)
		// All provider assignment fields are deliberately wrong, including Sunday's
		// still-linked investor truck and a completely unrecognizable unit label.
		exec(`INSERT INTO loads(id,load_id,status,total_pay,load_pay,total_miles,truck_unit,driver_name,dispatcher_name,raw_payload) VALUES
  (96601,'LOCAL-MON','delivered',2799,2799,800,'LOCAL-065','Wrong source driver','Wrong source dispatcher','{}'),
  (96602,'LOCAL-TUE','delivered',1313,1313,515,'01 PACIFIQUE','Wrong source driver','Wrong source dispatcher','{}'),
  (96603,'LOCAL-SUN','delivered',1353,1353,562,'LOCAL-065','Wrong source driver','Wrong source dispatcher','{}')`)
		for _, row := range []struct {
			date, number string
			load         int
		}{{"2026-10-19", "LOCAL-MON", 96601}, {"2026-10-20", "LOCAL-TUE", 96602}, {"2026-10-25", "LOCAL-SUN", 96603}} {
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) SELECT $1,$2::date,$3,$4,total_pay FROM loads WHERE id=$4`, driver, row.date, row.number, row.load)
		}
		week, _ := time.Parse(time.DateOnly, "2026-10-19")
		pay := NewDriverPayRepository(pool)
		report, err := pay.InvestorPay(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Drivers) != 1 || report.Drivers[0].TruckID != oldTruck || len(report.Drivers[0].Loads) != 2 || len(report.Drivers[0].Issues) != 0 {
			t.Fatalf("native attribution: %+v", report)
		}
		for _, l := range report.Drivers[0].Loads {
			if l.LoadNumber == "LOCAL-SUN" {
				t.Fatal("company-truck Sunday paid to investor")
			}
		}
		scoped, err := pay.InvestorPay(ctx, week, oldTruck)
		if err != nil || len(scoped.Drivers) != 1 || len(scoped.Drivers[0].Loads) != 2 {
			t.Fatalf("scoped/full parity: %+v %v", scoped, err)
		}
		driverPay, err := pay.Get(ctx, week)
		if err != nil || len(driverPay.Drivers) != 1 || len(driverPay.Drivers[0].Loads) != 3 {
			t.Fatalf("driver lost company work: %+v %v", driverPay, err)
		}
		loads, err := NewLoadRepository(pool).GetLoadsPage(ctx, LoadPageQuery{Pagination: Pagination{Page: 1, PageSize: 100}, Driver: "Local Mark", Dispatcher: "Local Dispatcher"})
		if err != nil || loads.Total != 3 {
			t.Fatalf("load filters used source assignments: %+v %v", loads, err)
		}
		exec(`UPDATE loads SET raw_payload='{"stops":[{"stop_type":"delivery","ordering":2,"location":{"address1":"200 Delivery Rd"}},{"stop_type":"pickup","ordering":1,"location":{"address1":"100 Pickup Rd","zip_code":"00123"}}]}' WHERE id=96601`)
		stopPage, err := NewLoadRepository(pool).GetLoadsPage(ctx, LoadPageQuery{Pagination: Pagination{Page: 1, PageSize: 100}})
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range stopPage.Items {
			if l.ID == 96601 && (len(l.Stops) != 2 || l.Stops[0].Address != "100 Pickup Rd" || l.Stops[0].ZipCode != "00123" || l.Stops[1].Address != "200 Delivery Rd" || l.RawPayload != nil) {
				t.Fatalf("load stop projection = %+v", l)
			}
		}
		for _, l := range loads.Items {
			if l.DriverID == nil || *l.DriverID != driver || l.TruckUnit == nil || l.DispatcherName == nil || *l.DispatcherName != "Local Dispatcher" {
				t.Fatalf("load surface leaked provider assignment: %+v", l)
			}
			want := "LOCAL-065"
			if l.ID == 96603 {
				want = "LOCAL-028"
			}
			if *l.TruckUnit != want {
				t.Fatalf("load %d: truck %s, want %s", l.ID, *l.TruckUnit, want)
			}
		}
		period, _ := ResolveDashboardPeriod("week", "2026-10-19", "", time.Now())
		review, err := NewDashboardRepository(pool).DashboardCostReview(ctx, period, "ownership", Pagination{})
		if err != nil || review.Total != 0 {
			t.Fatalf("stale source truck raised dashboard warning: %+v %v", review, err)
		}
		// Source allocation review is separate from native assignment authority:
		// an unresolved split stays on its real truck and keeps its financial hold.
		exec(`UPDATE loads SET split_expected=true WHERE id=96602`)
		held, err := pay.InvestorPay(ctx, week, oldTruck)
		if err != nil || len(held.Drivers) != 1 || len(held.Drivers[0].Loads) != 2 {
			t.Fatalf("split review hid a native truck load: %+v %v", held, err)
		}
		foundReview := false
		for _, l := range held.Drivers[0].Loads {
			if l.LoadNumber == "LOCAL-TUE" {
				foundReview = l.TripReview && l.DriverFee == "" && l.Fee == ""
			}
		}
		if !foundReview {
			t.Fatal("native attribution bypassed the split financial review")
		}
		for _, issue := range held.Drivers[0].Issues {
			if strings.Contains(issue, "Review truck assignment") {
				t.Fatal("split review was mislabeled as an assignment problem")
			}
		}
		exec(`UPDATE loads SET split_expected=false WHERE id=96602`)
		// Missing native assignment cannot silently recover the provider's truck.
		exec(`UPDATE truck_driver_assignments SET unassigned_at='2026-10-25 00:00-04' WHERE truck_id=$1`, companyTruck)
		var linked bool
		if err = pool.QueryRow(ctx, `SELECT truck_id IS NOT NULL FROM clustie_loads WHERE id=96603`).Scan(&linked); err != nil || linked {
			t.Fatal("missing local assignment used provider fallback", err)
		}
	})
}
