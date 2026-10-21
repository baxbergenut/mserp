package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/samsara"
)

func TestSamsaraDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_SAMSARA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_SAMSARA_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/131_samsara_telemetry.sql")
	if err != nil {
		t.Fatal(err)
	}
	init := strings.ReplaceAll(string(source), "\r\n", "\n")
	upgrade := strings.ReplaceAll(string(migration), "\r\n", "\n")
	if !strings.Contains(init, upgrade) {
		t.Fatal("init/migration mismatch")
	}
	for _, mode := range []string{"fresh", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("samsara_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec("CREATE SCHEMA " + quoted + "; SET search_path TO " + quoted + ",public; GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app")
			defer admin.Exec(context.Background(), "SET search_path TO public; DROP SCHEMA "+quoted+" CASCADE")
			if mode == "upgrade" {
				exec(strings.Replace(init, upgrade, "", 1))
				exec(upgrade)
			} else {
				exec(init)
			}
			exec("GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO mserp_app")
			for _, table := range []string{"samsara_telemetry", "samsara_tracking", "samsara_sync_state"} {
				var owner string
				if e := admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename=$2`, schema, table).Scan(&owner); e != nil || owner != "mserp_app" {
					t.Fatal("table owner", table, owner, e)
				}
			}
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			var truck string
			vin := "1M8GDM9AXKP042788"
			now := time.Now().UTC().Add(-time.Minute)
			old := now.Add(-20 * time.Minute)
			if e = admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,vin,active) VALUES('SAMSARA-TEST',$1,true) RETURNING id::text`, vin).Scan(&truck); e != nil {
				t.Fatal(e)
			}
			exec(`INSERT INTO maintenance_odometer_readings(truck_id,reading_date,mileage,source,actor_name) VALUES($1,(now() AT TIME ZONE 'America/New_York')::date,1950,'manual','Fixture')`, truck)
			m := NewMaintenanceRepository(pool)
			if e = m.StoreMaintenanceTelemetry(ctx, MaintenanceTelemetryTruck{TruckID: truck, VIN: vin}, &MaintenanceTelemetry{Mileage: 2050, ReportedAt: now}); e != nil {
				t.Fatal(e)
			}
			exec(`INSERT INTO five_eld_locations(vin,latitude,longitude,reported_at,fetched_at,provider_truck_number) VALUES($1,41,-76,$2,$2,'ELD')`, vin, now)
			repo := NewSamsaraRepository(pool)
			gps := &samsara.GPS{Time: old, Latitude: 40, Longitude: -75}
			odo := &samsara.Odometer{Time: old, Miles: 2000}
			snapshot := samsara.Snapshot{Vehicles: []samsara.Vehicle{{ID: "one", VIN: vin, Name: "Samsara unit", GPS: gps, Odometer: odo}}, Ambiguous: []string{}, Fetched: 1}
			if e = repo.Store(ctx, snapshot, now); e != nil {
				t.Fatal(e)
			}
			location, e := NewFleetRepository(pool).TruckLocation(ctx, truck)
			if e != nil || location.Location == nil || location.Location.Source != "samsara" || location.Location.Latitude != 40 {
				t.Fatal("Samsara GPS did not win over newer Five ELD", location, e)
			}
			reading, e := m.CachedMaintenanceOdometer(ctx, truck)
			if e != nil || reading == nil || reading.Source != "samsara" || reading.Mileage != 2000 {
				t.Fatal("Samsara odometer did not win", reading, e)
			}
			if e = m.SaveProgramTrucks(ctx, OilChangeProgramID, []ProgramTruckInput{{TruckID: truck, Active: true}}, ""); e != nil {
				t.Fatal(e)
			}
			members, e := m.ProgramTrucks(ctx, OilChangeProgramID, truck)
			if e != nil || len(members) != 1 || members[0].CurrentSource != "samsara" || members[0].CurrentMiles == nil || *members[0].CurrentMiles != 2000 {
				t.Fatal("service programs chose another source", members, e)
			}
			// Outages retain last-known data, and an older sample never rewinds it.
			if e = repo.RecordFailure(ctx, now, fmt.Errorf("fixture outage")); e != nil {
				t.Fatal(e)
			}
			snapshot.Vehicles[0].Odometer = &samsara.Odometer{Time: old.Add(-time.Minute), Miles: 1000}
			if e = repo.Store(ctx, snapshot, now); e != nil {
				t.Fatal(e)
			}
			reading, e = m.CachedMaintenanceOdometer(ctx, truck)
			if e != nil || reading == nil || reading.Mileage != 2000 {
				t.Fatal("sample rewind", reading, e)
			}
			// Local ambiguity disables Samsara instead of choosing a truck by name.
			exec(`INSERT INTO trucks(unit_number,vin) VALUES('DUPLICATE-SAMSARA',$1)`, strings.ToLower(vin))
			location, e = NewFleetRepository(pool).TruckLocation(ctx, truck)
			if e != nil || location.Location == nil || location.Location.Source != "five_eld" {
				t.Fatal("ambiguous local VIN selected Samsara", location, e)
			}
			exec(`DELETE FROM trucks WHERE unit_number='DUPLICATE-SAMSARA'`)
			// A complete roster removes eligibility; fallback becomes visible.
			if e = repo.Store(ctx, samsara.Snapshot{Vehicles: []samsara.Vehicle{}, Ambiguous: []string{vin}}, now); e != nil {
				t.Fatal(e)
			}
			location, e = NewFleetRepository(pool).TruckLocation(ctx, truck)
			if e != nil || location.Location == nil || location.Location.Source != "five_eld" {
				t.Fatal("GPS fallback missing", location, e)
			}
			reading, e = m.CachedMaintenanceOdometer(ctx, truck)
			if e != nil || reading == nil || reading.Source != "five_eld" || reading.Mileage != 2050 {
				t.Fatal("odometer fallback missing", reading, e)
			}
			// Provider changes invalidate arrival evidence even if Samsara is older.
			lat, lng := gps.Latitude, gps.Longitude
			evidence := evaluateArrival(ArrivalEvidence{Context: "test", Source: "five_eld", LastAt: now, Arrived: false}, "test", &DriverBoardLocation{Source: "samsara", Latitude: lat, Longitude: lng, ReportedAt: old}, BoardStop{Latitude: &lat, Longitude: &lng}, now)
			if !evidence.Arrived || evidence.Source != "samsara" || !evidence.LastAt.Equal(old) {
				t.Fatal("old provider evidence retained", evidence)
			}
			testOdometerSourceChecks(t, ctx, admin, pool)
		})
	}
}
