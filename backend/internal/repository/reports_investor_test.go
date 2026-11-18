package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestReportsInvestorDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		exec := func(sql string) {
			t.Helper()
			if _, err := admin.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		fixture, err := os.ReadFile("testdata/reports_investor.sql")
		if err != nil {
			t.Fatal(err)
		}
		exec(string(fixture))
		period, _ := ResolveReportsPeriod("2026-10-12", "2026-10-18", "", "", time.Now())
		r := NewReportsRepository(pool)
		check := func(fee, rc, profit float64, estimated bool) {
			t.Helper()
			result, err := r.Read(ctx, ReportsQuery{View: ReportInvestor, Period: period, CurrentOnly: true})
			if err != nil || len(result.Current) != 1 {
				t.Fatal(result, err)
			}
			row := result.Current[0]
			if row.Metrics["gross"] != 120000 || row.Metrics["miles"] != 300 || row.Metrics["carrierFees"] != fee || row.Metrics["recurringCharges"] != rc || row.Metrics["contribution"] != profit || row.Estimated["contribution"] != estimated {
				t.Fatal(row)
			}
		}
		check(15000, 10000, 22000, true)
		exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits,finalized,report) VALUES(
 '64000000-0000-0000-0000-000000000003','64000000-0000-0000-0000-000000000001','2026-10-12','{}',true,
 '{"loads":[{"sourceDriverId":"64000000-0000-0000-0000-000000000002","sourceDate":"2026-10-14","sourceSlot":0,"driverGross":"1000","fee":"880"}],"autoCharges":[{"source":"truck_charge:64000000-0000-0000-0000-000000000003:64000000-0000-0000-0000-000000000004","amount":"-100"}],"edits":{"sourceCharges":[{"source":"truck_charge:64000000-0000-0000-0000-000000000003:64000000-0000-0000-0000-000000000004","amount":"-45"}]}}')`)
		check(12000, 4500, 13500, false)
		exec(`UPDATE investor_pay_weeks SET finalized=false`)
		check(15000, 10000, 22000, true)
		exec(`DELETE FROM truck_settlement_terms`)
		result, err := r.Read(ctx, ReportsQuery{View: ReportInvestor, Period: period, CurrentOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := result.Current[0].Metrics["contribution"]; ok {
			t.Fatal("missing agreement produced profit")
		}
	})
}

func TestReportsMilesOnIdleTruck(t *testing.T) {
	for _, view := range []string{ReportCompany, ReportOperator, ReportInvestor} {
		groups := reportGroups{}
		row := groups.add("idle", "Idle", reportIdentity{}, ReportEvidence{Kind: "Maintenance"})
		row.finish(view)
		if miles, ok := row.Metrics["miles"]; !ok || miles != 0 {
			t.Fatalf("%s idle mileage: %v", view, row.Metrics)
		}
		incomplete := groups.add("active", "Active", reportIdentity{}, ReportEvidence{Kind: "Gross Board"})
		incomplete.loads["manual"] = true
		incomplete.missingMiles = true
		incomplete.finish(view)
		if _, ok := incomplete.Metrics["miles"]; ok {
			t.Fatal("missing mileage converted to zero")
		}
	}
}
