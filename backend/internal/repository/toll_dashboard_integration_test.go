package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Uses a connection-local temporary table, leaving any existing data untouched.
func TestTollDashboardDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_TOLL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_TOLL_TEST_DATABASE_URL to run PostgreSQL aggregation checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid database configuration")
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("could not connect to test database")
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE tolls (
		posting_date date, amount numeric(12,2), equipment_unit text,
		agency text, prepass_environment text
	);
	INSERT INTO tolls VALUES
		('2025-12-31', 999, 'OLD', 'Outside', 'production'),
		('2026-01-01', 10.10, '101', 'Agency A', 'production'),
		('2026-01-04', 20.20, '101', 'Agency A', NULL),
		('2026-01-05', -5.05, 'UNMATCHED', 'Agency B', 'production'),
		('2026-03-01', 0.10, '102', 'Agency A', 'production'),
		('2026-03-01', 500, 'TEST', 'Test', 'nonproduction'),
		('2026-03-02', 999, 'FUTURE', 'Outside', 'production');`)
	if err != nil {
		t.Fatal(err)
	}
	date := func(value string) time.Time {
		parsed, err := time.Parse(time.DateOnly, value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	repo := NewTollRepository(pool)
	dashboard, err := repo.GetDashboard(ctx, date("2026-01-01"), date("2026-03-01"))
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Totals.Spend != 25.35 || dashboard.Totals.TransactionCount != 4 || dashboard.Totals.TruckCount != 3 {
		t.Fatalf("unexpected totals: %+v", dashboard.Totals)
	}
	if len(dashboard.Monthly) != 3 || dashboard.Monthly[0].Spend != 25.25 || dashboard.Monthly[1].Spend != 0 || dashboard.Monthly[2].Spend != 0.10 {
		t.Fatalf("unexpected months: %+v", dashboard.Monthly)
	}
	if len(dashboard.Weekly) != 9 || dashboard.Weekly[0].Label != "2025-12-29" || dashboard.Weekly[0].Spend != 30.30 || dashboard.Weekly[1].Spend != -5.05 {
		t.Fatalf("unexpected weeks: %+v", dashboard.Weekly)
	}
	if len(dashboard.Agencies) != 2 || dashboard.Agencies[0].Spend != 30.40 || len(dashboard.Trucks) != 3 {
		t.Fatalf("unexpected breakdowns: %+v / %+v", dashboard.Agencies, dashboard.Trucks)
	}
	empty, err := repo.GetDashboard(ctx, date("2027-01-01"), date("2027-01-01"))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Totals != (TollDashboardTotals{}) || len(empty.Monthly) != 1 || len(empty.Weekly) != 1 || len(empty.Agencies) != 0 || empty.Trucks == nil {
		t.Fatalf("unexpected empty dashboard: %+v", empty)
	}
	// Attribute every transaction, including credits and historical imports,
	// using normalized agency codes. Ambiguous agencies must stay unmapped.
	_, err = pool.Exec(ctx, `UPDATE pg_temp.tolls SET agency = CASE equipment_unit
		WHEN '101' THEN ' ptc ' WHEN '102' THEN 'MdTA'
		WHEN 'UNMATCHED' THEN 'RIVE' ELSE agency END`)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := repo.GetDashboard(ctx, date("2026-01-01"), date("2026-03-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped.States) != 2 || mapped.States[0].Label != "MD" || mapped.States[0].Spend != 0.10 || mapped.States[1].Label != "PA" || mapped.States[1].Spend != 30.30 || mapped.States[1].TransactionCount != 2 {
		t.Fatalf("unexpected state totals: %+v", mapped.States)
	}
	if len(mapped.Unmapped) != 1 || mapped.Unmapped[0].Label != "RIVE" || mapped.Unmapped[0].Spend != -5.05 {
		t.Fatalf("ambiguous agency was lost or mislocated: %+v", mapped.Unmapped)
	}
	if empty.States == nil || empty.Unmapped == nil {
		t.Fatal("empty geography must serialize as arrays")
	}
	_, err = pool.Exec(ctx, `TRUNCATE pg_temp.tolls;
		INSERT INTO pg_temp.tolls
		SELECT '2026-01-01', 1, n::text, code, 'production'
		FROM unnest(ARRAY['PTC','WVPEDTA','ILTOLL','ITRCC','OTC','MDTA','NYSTA','NJTP','NTTA','CFX','NCTA','VDOT']) WITH ORDINALITY AS codes(code,n);
		INSERT INTO pg_temp.tolls VALUES ('2026-01-01', 2, '99', 'NEW-AGENCY', 'production');`)
	if err != nil {
		t.Fatal(err)
	}
	all, err := repo.GetDashboard(ctx, date("2026-01-01"), date("2026-01-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Agencies) != 10 || len(all.States) != 12 || len(all.Unmapped) != 1 || all.Unmapped[0].Spend != 2 {
		t.Fatalf("map must include agencies outside the top ten: %+v", all)
	}
}
