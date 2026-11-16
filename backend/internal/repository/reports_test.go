package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReportsClosedWeekDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		fixture, err := os.ReadFile("testdata/reports_closed_week.sql")
		if err != nil {
			t.Fatal(err)
		}
		_, err = admin.Exec(ctx, string(fixture))
		if err != nil {
			t.Fatal(err)
		}
		repo := NewReportsRepository(pool)
		read := func(view, from, to string) ReportsResult {
			t.Helper()
			period, err := ResolveReportsPeriod(from, to, "", "", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			result, err := repo.Read(ctx, ReportsQuery{View: view, Period: period})
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
		check := func(row ReportRow, want map[string]float64) {
			t.Helper()
			for key, value := range want {
				if got, ok := row.Metrics[key]; !ok || got != value {
					t.Errorf("%s %s got %v (%v), want %v; all %v", row.Name, key, got, ok, value, row.Metrics)
				}
			}
		}
		company := read(ReportCompany, "2026-10-12", "2026-10-18")
		if len(company.Current) != 2 {
			t.Fatalf("company: %+v", company.Current)
		}
		check(company.Current[0], map[string]float64{"gross": 110000, "miles": 400, "earnedPay": 30000, "maintenance": 8000, "fuel": 20000, "tolls": 4000, "scales": 1400, "equipment": 6000, "contribution": 40600, "completedLoads": 1, "provisionalGross": 20000})
		operator := read(ReportOperator, "2026-10-12", "2026-10-18")
		if len(operator.Current) != 2 {
			t.Fatalf("operator: %+v", operator.Current)
		}
		check(operator.Current[0], map[string]float64{"gross": 180000, "carrierFees": 34000, "carrierCosts": 5000, "contribution": 29000,
			"settlementEarnings": 136000, "reimbursements": 2500, "escrowWithheld": 2000, "escrowReturned": 500, "escrowApplied": 1000, "deductions": 5000, "netPayable": 132000})
		performance := read(ReportDriver, "2026-10-12", "2026-10-18")
		if len(performance.Current) != 2 {
			t.Fatalf("driver: %+v", performance.Current)
		}
		check(performance.Current[0], map[string]float64{"activeDays": 2, "pickupOnTime": 100, "pickupCoverage": 100, "deliveryOnTime": 0, "deliveryCoverage": 100})
		check(performance.Current[1], map[string]float64{"pickupCoverage": 0, "deliveryCoverage": 0})
		if _, ok := performance.Current[1].Metrics["pickupOnTime"]; ok {
			t.Error("unknown on-time rate displayed")
		}
		period := performance.Period
		filtered, err := repo.Read(ctx, ReportsQuery{View: ReportDriver, Period: period, Truck: "61000000-0000-0000-0000-000000000004", Dispatcher: "historical:Former Dispatcher"})
		if err != nil || len(filtered.Current) != 1 || filtered.Current[0].Name != "Company Driver" {
			t.Fatal("dated filters", filtered, err)
		}
		dispatch := read(ReportDispatcher, "2026-10-12", "2026-10-18")
		if len(dispatch.Current) != 2 || dispatch.Current[1].Name != "Original Dispatcher" {
			t.Fatalf("transferred dispatch: %+v", dispatch.Current)
		}
		check(dispatch.Current[1], map[string]float64{"gross": 180000, "truckWeeks": 1, "grossPerTruckWeek": 180000})
		check(operator.Current[0], map[string]float64{"dispatchBalance": 10000})
		// Repeat reads must reuse evidence without sharing mutable result objects.
		company.Current[0].Metrics["gross"] = -1
		check(read(ReportCompany, "2026-10-12", "2026-10-18").Current[0], map[string]float64{"gross": 110000})
		// Collaboration heartbeats must not evict unchanged financial snapshots.
		if _, err = admin.Exec(ctx, `UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name IN ('driver_board_presence','gross_board_presence','fleetscope_sync_state','maintenance_odometer_readings','payroll_statement_current')`); err != nil {
			t.Fatal(err)
		}
		if presence := read(ReportCompany, "2026-10-12", "2026-10-18"); presence.Revision != company.Revision {
			t.Fatal("board presence invalidated financial report")
		}
		if _, err = admin.Exec(ctx, `UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name='maintenance_configuration'`); err != nil {
			t.Fatal(err)
		}
		if classification := read(ReportCompany, "2026-10-12", "2026-10-18"); classification.Revision == company.Revision {
			t.Fatal("maintenance classification did not invalidate report")
		}
		if _, err = admin.Exec(ctx, `UPDATE loads SET total_pay=1200 WHERE id=61001`); err != nil {
			t.Fatal(err)
		}
		check(read(ReportCompany, "2026-10-12", "2026-10-18").Current[0], map[string]float64{"gross": 120000, "contribution": 50600})
		// Provider availability is intentionally outside payroll revision capture.
		if _, err = admin.Exec(ctx, `INSERT INTO load_source_availability(load_id,present) VALUES(61001,false)`); err != nil {
			t.Fatal(err)
		}
		missing := read(ReportCompany, "2026-10-12", "2026-10-18").Current[0]
		for _, key := range []string{"gross", "contribution", "miles", "costPerMile"} {
			if _, ok := missing.Metrics[key]; ok {
				t.Errorf("unavailable %s fabricated: %v", key, missing.Metrics)
			}
		}
		if _, err = admin.Exec(ctx, `UPDATE load_source_availability SET present=true WHERE load_id=61001`); err != nil {
			t.Fatal(err)
		}
		check(read(ReportCompany, "2026-10-12", "2026-10-18").Current[0], map[string]float64{"gross": 120000})
		if _, err = admin.Exec(ctx, `UPDATE payroll_settlements SET finalized=false WHERE driver_id='61000000-0000-0000-0000-000000000001'`); err != nil {
			t.Fatal(err)
		}
		unfinalized := read(ReportCompany, "2026-10-12", "2026-10-18").Current[0]
		if _, ok := unfinalized.Metrics["contribution"]; ok {
			t.Fatal("unfinalized pay counted as zero")
		}
		if _, err = admin.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,driver_id,truck_id,delivery_time,raw_payload)
 VALUES(61999,'UNPLANNED','delivered',500,500,100,'61000000-0000-0000-0000-000000000001','61000000-0000-0000-0000-000000000004','2026-10-15 15:00Z','{}')`); err != nil {
			t.Fatal(err)
		}
		unplanned := read(ReportDriver, "2026-10-12", "2026-10-18")
		if len(unplanned.Current) != 3 || unplanned.Current[2].ID != "unassigned" {
			t.Fatal("provider labels became native assignments", unplanned.Current)
		}
		check(unplanned.Current[2], map[string]float64{"gross": 50000, "completedLoads": 1})
	})
}

func TestReportsCutoff(t *testing.T) {
	now := time.Date(2026, 11, 9, 4, 59, 59, 0, time.UTC)
	period, err := ResolveReportsPeriod("", "", "", "", now)
	if err != nil || period.DateFrom != "2026-10-26" || period.DateTo != "2026-11-01" {
		t.Fatal(period, err)
	}
	period, err = ResolveReportsPeriod("", "", "", "", now.Add(time.Second))
	if err != nil || period.DateFrom != "2026-11-02" || period.CompareFrom != "2026-10-26" {
		t.Fatal(period, err)
	}
}

func TestReportsFleetScaleDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		for _, file := range []string{"reports_closed_week.sql", "reports_scale.sql"} {
			body, err := os.ReadFile("testdata/" + file)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = admin.Exec(ctx, string(body)); err != nil {
				t.Fatal(err)
			}
		}
		period, err := ResolveReportsPeriod("2026-10-12", "2026-10-18", "", "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		repo := NewReportsRepository(pool)
		for _, view := range []string{ReportCompany, ReportOperator, ReportDispatcher, ReportDriver} {
			q := ReportsQuery{View: view, Period: period, CurrentOnly: true}
			started := time.Now()
			cold, err := repo.Read(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			coldTime := time.Since(started)
			started = time.Now()
			warm, err := repo.Read(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			warmTime := time.Since(started)
			if len(cold.Current) != len(warm.Current) || len(cold.Comparison) != 0 {
				t.Fatal("cache changed row coverage")
			}
			t.Logf("%s: %d rows; cold %s; cached %s", view, len(cold.Current), coldTime, warmTime)
			if view == ReportCompany && len(cold.Current) != 122 {
				t.Fatal("scale fixture lost trucks")
			}
		}
	})
}
