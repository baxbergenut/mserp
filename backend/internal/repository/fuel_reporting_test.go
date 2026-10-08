package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFuelReportingMigrationDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("test database required")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("isolated database required")
	}
	ctx := context.Background()
	conn, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close(ctx)
	schema := pgx.Identifier{fmt.Sprintf("fuel_reporting_%d", time.Now().UnixNano())}.Sanitize()
	exec := func(sql string) {
		t.Helper()
		if _, e := conn.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE SCHEMA ` + schema + `;SET search_path TO ` + schema + `,public`)
	defer func() { _, _ = conn.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+schema+` CASCADE`) }()
	exec(`CREATE TABLE fuel_transactions(id serial PRIMARY KEY,purchased_at timestamptz,timezone text,prompts jsonb,driver_id uuid);
 CREATE TABLE truck_driver_assignments(truck_id uuid,assigned_at timestamptz);
 CREATE TABLE loads(id int,pickup_time timestamptz,driver_id uuid,truck_unit text,team_driver_name text);
 INSERT INTO fuel_transactions(purchased_at,timezone,prompts)
 SELECT d::timestamptz,z,'[{"label":" Truck # ","value":" ab1 "},{"label":"truck #","value":"AB1"},{"label":"Truck #","value":"ZZ2"}]'::jsonb
 FROM unnest(ARRAY['2026-03-08T06:59:00Z','2026-03-08T07:01:00Z','2026-11-01T05:59:00Z','2026-11-01T06:01:00Z','2026-10-05T03:30:00Z'])d
 CROSS JOIN unnest(ARRAY['America/New_York','US/Eastern','US/Arizona','US/Central','Pacific/Kiritimati','Pacific/Pago_Pago','invalid','',NULL])z;`)
	migration, e := os.ReadFile("../../sql/068_reporting_performance.sql")
	if e != nil {
		t.Fatal(e)
	}
	exec(string(migration))
	check := func() {
		t.Helper()
		var bad int
		e = conn.QueryRow(ctx, `SELECT count(*) FROM fuel_transactions f WHERE purchased_on IS DISTINCT FROM (purchased_at AT TIME ZONE `+fuelTimezoneExpression("f.timezone")+`)::date OR reporting_timezone_valid IS DISTINCT FROM EXISTS(SELECT 1 FROM pg_timezone_names WHERE name=f.timezone) OR reported_truck_units<>ARRAY['AB1','ZZ2']`).Scan(&bad)
		if e != nil || bad != 0 {
			t.Fatalf("reporting semantics changed: %d %v", bad, e)
		}
	}
	check()
	exec(`UPDATE fuel_transactions SET timezone='America/Los_Angeles',purchased_at=purchased_at+interval '3 hours'`)
	check()
	exec(`UPDATE fuel_transactions SET purchased_on='2000-01-01'`)
	check() // Derived evidence cannot be independently corrupted.
	exec(`INSERT INTO fuel_transactions(purchased_at,timezone,prompts) VALUES('2026-10-05T03:30:00Z','US/Eastern','[]')`)
	var date string
	var units []string
	if e = conn.QueryRow(ctx, `SELECT purchased_on::text,reported_truck_units FROM fuel_transactions ORDER BY id DESC LIMIT 1`).Scan(&date, &units); e != nil || date != "2026-10-04" || len(units) != 0 {
		t.Fatalf("new import: %s %v %v", date, units, e)
	}
}
