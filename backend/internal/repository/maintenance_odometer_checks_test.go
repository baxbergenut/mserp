package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/samsara"
)

func testOdometerSourceChecks(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	m, sam := NewMaintenanceRepository(pool), NewSamsaraRepository(pool)
	now := time.Now().UTC().Add(-time.Minute)
	newTruck := func(t *testing.T, unit, vin string, baseline int, days int) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,vin,active) VALUES($1,$2,true) RETURNING id::text`, unit, vin).Scan(&id); err != nil {
			t.Fatal(err)
		}
		exec(t, `INSERT INTO maintenance_records(truck_id,status,service_date,odometer,services,program_ids) VALUES($1,'completed',(now() AT TIME ZONE 'America/New_York')::date-$3::int,$2,'[{"typeCode":"preventive","name":"Oil change","amount":null,"trackingCode":"pm"}]',ARRAY[$4::uuid])`, id, baseline, days, OilChangeProgramID)
		if err := m.SaveProgramTrucks(ctx, OilChangeProgramID, []ProgramTruckInput{{TruckID: id, Active: true}}, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}
	storeSam := func(t *testing.T, vin string, mileage int, at time.Time) {
		t.Helper()
		if err := sam.Store(ctx, samsara.Snapshot{Fetched: 1, Ambiguous: []string{}, Vehicles: []samsara.Vehicle{{ID: vin, VIN: vin, Name: "Fixture", GPS: &samsara.GPS{Time: now, Latitude: 40, Longitude: -75}, Odometer: &samsara.Odometer{Time: at, Miles: mileage}}}}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	check := func(t *testing.T, truck, source string, mileage int, prefill bool) {
		t.Helper()
		members, err := m.ProgramTrucks(ctx, OilChangeProgramID, truck)
		if err != nil || len(members) != 1 {
			t.Fatal(members, err)
		}
		v := members[0]
		if v.CurrentSource != source || v.CurrentMiles == nil || *v.CurrentMiles != mileage || (v.MilesSinceService != nil && *v.MilesSinceService < 0) {
			t.Fatal("untrusted program reading", v)
		}
		cached, err := m.CachedMaintenanceOdometer(ctx, truck)
		if err != nil || (cached != nil) != prefill {
			t.Fatal("unsafe autofill", cached, err)
		}
		if prefill && (cached.Source != source || cached.Mileage != mileage) {
			t.Fatal("autofill differs from program", cached, v)
		}
	}
	t.Run("003 fresh but wrong Five ELD", func(t *testing.T) {
		vin := "1FUJGLDR0JLJK4059"
		truck := newTruck(t, "CHECK-003", vin, 906991, 19)
		exec(t, `INSERT INTO maintenance_odometer_readings(truck_id,reading_date,mileage,source,actor_name) VALUES($1,(now() AT TIME ZONE 'America/New_York')::date,915999,'manual','Fixture')`, truck)
		storeSam(t, vin, 916056, now.Add(-4*time.Minute))
		if err := m.StoreMaintenanceTelemetry(ctx, MaintenanceTelemetryTruck{TruckID: truck, VIN: vin}, &MaintenanceTelemetry{Mileage: 140159, ReportedAt: now}); err != nil {
			t.Fatal(err)
		}
		check(t, truck, "samsara", 916056, true)
		// An old binary may already have cached the invalid value. Read checks
		// must exclude it even before another background refresh occurs.
		exec(t, `UPDATE maintenance_telemetry SET mileage=140159,reported_at=$2 WHERE truck_id=$1`, truck, now)
		exec(t, `UPDATE samsara_telemetry SET available=false WHERE vin=$1`, vin)
		check(t, truck, "manual", 915999, false)
	})
	t.Run("041 stale Samsara behind last service", func(t *testing.T) {
		vin := "4V4NC9EH8NN283178"
		truck := newTruck(t, "CHECK-041", vin, 904185, 14)
		// Seed legacy data directly: new validation must screen existing rows.
		exec(t, `INSERT INTO samsara_telemetry(vin,vehicle_id,mileage,odometer_at,fetched_at) VALUES($1,$1,892063,$2,now())`, vin, now.Add(-42*24*time.Hour))
		if err := m.StoreMaintenanceTelemetry(ctx, MaintenanceTelemetryTruck{TruckID: truck, VIN: vin}, &MaintenanceTelemetry{Mileage: 910751, ReportedAt: now}); err != nil {
			t.Fatal(err)
		}
		check(t, truck, "five_eld", 910751, true)
	})
	t.Run("preserve accepted mileage across jumps and rewinds", func(t *testing.T) {
		vin := "1M8GDM9AXKP042791"
		truck := newTruck(t, "CHECK-JUMPS", vin, 900000, 1)
		candidate := MaintenanceTelemetryTruck{TruckID: truck, VIN: vin}
		if err := m.StoreMaintenanceTelemetry(ctx, candidate, &MaintenanceTelemetry{Mileage: 900100, ReportedAt: now.Add(-5 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []int{899999, 900050, 1900000} {
			if err := m.StoreMaintenanceTelemetry(ctx, candidate, &MaintenanceTelemetry{Mileage: bad, ReportedAt: now}); err != nil {
				t.Fatal(err)
			}
			check(t, truck, "five_eld", 900100, true)
		}
		storeSam(t, vin, 900110, now)
		storeSam(t, vin, 1900000, now.Add(time.Second))
		check(t, truck, "samsara", 900110, true)
		storeSam(t, vin, 900105, now.Add(time.Second))
		check(t, truck, "samsara", 900110, true)
		// Revalidating after a reviewed history correction excludes caches that
		// were accepted under the former baseline.
		exec(t, `INSERT INTO maintenance_odometer_readings(truck_id,reading_date,mileage,source,actor_name) VALUES($1,(now() AT TIME ZONE 'America/New_York')::date,900200,'manual','Fixture')`, truck)
		check(t, truck, "manual", 900200, false)
	})
	t.Run("cross provider conflict without a saved baseline", func(t *testing.T) {
		vin := "1M8GDM9AXKP042792"
		var truck string
		if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,vin,active) VALUES('CHECK-NO-BASELINE',$1,true) RETURNING id::text`, vin).Scan(&truck); err != nil {
			t.Fatal(err)
		}
		if err := m.SaveProgramTrucks(ctx, OilChangeProgramID, []ProgramTruckInput{{TruckID: truck, Active: true}}, ""); err != nil {
			t.Fatal(err)
		}
		storeSam(t, vin, 916056, now.Add(-2*time.Hour))
		if err := m.StoreMaintenanceTelemetry(ctx, MaintenanceTelemetryTruck{TruckID: truck, VIN: vin}, &MaintenanceTelemetry{Mileage: 140159, ReportedAt: now}); err != nil {
			t.Fatal(err)
		}
		check(t, truck, "samsara", 916056, false)
	})
	t.Run("reviewed baseline supersedes original service mileage", func(t *testing.T) {
		vin := "1M8GDM9AXKP042793"
		truck := newTruck(t, "CHECK-CORRECTION", vin, 900000, 1)
		exec(t, `INSERT INTO maintenance_pm_baselines(truck_id,service_date,odometer,source_record_id,source_version,actor_name)
 SELECT truck_id,service_date,890000,id,version,'Fixture' FROM maintenance_records WHERE truck_id=$1`, truck)
		exec(t, `INSERT INTO maintenance_odometer_readings(truck_id,record_id,reading_date,mileage,source,actor_name)
 SELECT truck_id,id,service_date,odometer,'service','Fixture' FROM maintenance_records WHERE truck_id=$1`, truck)
		storeSam(t, vin, 890100, now)
		check(t, truck, "samsara", 890100, true)
		exec(t, `UPDATE samsara_telemetry SET available=false WHERE vin=$1`, vin)
		check(t, truck, "service", 890000, false)
	})
}
