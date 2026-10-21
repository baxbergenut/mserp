package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/samsara"
	"testing"
	"time"
)

func testOdometerSelection(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	m := NewMaintenanceRepository(pool)
	now := time.Now().UTC().Add(-time.Minute)
	var truck string
	const vin = "1M8GDM9AXKP042794"
	if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,vin,active) VALUES('SOURCE-CHOICE',$1,true) RETURNING id::text`, vin).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO maintenance_records(truck_id,status,service_date,odometer,services,program_ids) VALUES($1,'completed',(now() AT TIME ZONE 'America/New_York')::date-1,900000,'[{"typeCode":"preventive","name":"Oil change","amount":null,"trackingCode":"pm"}]',ARRAY[$2::uuid])`, truck, OilChangeProgramID)
	if err := m.SaveProgramTrucks(ctx, OilChangeProgramID, []ProgramTruckInput{{TruckID: truck, Active: true}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.StoreMaintenanceTelemetry(ctx, MaintenanceTelemetryTruck{truck, vin}, &MaintenanceTelemetry{Mileage: 900120, ReportedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := NewSamsaraRepository(pool).Store(ctx, samsara.Snapshot{Fetched: 1, Ambiguous: []string{}, Vehicles: []samsara.Vehicle{{ID: "choice", VIN: vin, Name: "Fixture", Odometer: &samsara.Odometer{Miles: 900100, Time: now.Add(-5 * time.Minute)}}}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	data, err := m.OdometerSources(ctx, truck)
	if err != nil || data.Source != "auto" || data.Current == nil || data.Current.Source != "samsara" || len(data.Readings) != 3 {
		t.Fatal("comparison", data, err)
	}
	selectSource := func(source string, version int) {
		t.Helper()
		if err := m.SaveOdometerSource(ctx, truck, OdometerSourceInput{Source: source, Version: version, ActorName: "Fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	selectSource("five_eld", 0)
	if err := m.SaveOdometerSource(ctx, truck, OdometerSourceInput{Source: "auto", Version: 0}); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatal("stale preference accepted", err)
	}
	members, err := m.ProgramTrucks(ctx, OilChangeProgramID, truck)
	if err != nil || len(members) != 1 || members[0].CurrentSource != "five_eld" || *members[0].CurrentMiles != 900120 {
		t.Fatal("program selection", members, err)
	}
	reading, err := m.CachedMaintenanceOdometer(ctx, truck)
	if err != nil || reading == nil || reading.Source != "five_eld" {
		t.Fatal("work order selection", reading, err)
	}
	exec(`UPDATE maintenance_telemetry SET mileage=NULL,reported_at=NULL WHERE truck_id=$1`, truck)
	data, err = m.OdometerSources(ctx, truck)
	if err != nil || data.Current != nil {
		t.Fatal("pinned source fell back", data, err)
	}
	if reading, err = m.CachedMaintenanceOdometer(ctx, truck); err != nil || reading != nil {
		t.Fatal("autofill fell back", reading, err)
	}
	exec(`UPDATE maintenance_telemetry SET mileage=900120,reported_at=$2 WHERE truck_id=$1`, truck, now)
	if err = m.StoreMaintenanceTelemetry(ctx, MaintenanceTelemetryTruck{truck, vin}, &MaintenanceTelemetry{Mileage: 140159, ReportedAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	data, err = m.OdometerSources(ctx, truck)
	if err != nil || data.Readings[1].Mileage == nil || *data.Readings[1].Mileage != 140159 || data.Readings[1].Valid {
		t.Fatal("rejected raw reading hidden/usable", data, err)
	}
	if err = m.SaveOdometerSource(ctx, truck, OdometerSourceInput{Source: "five_eld", Version: 1}); !errors.Is(err, ErrMaintenanceInvalid) {
		t.Fatal("bad source selectable", err)
	}
	selectSource("auto", 1)
	manual, err := m.SaveReading(ctx, MaintenanceReading{TruckID: truck, Date: maintenanceToday().Format(time.DateOnly), Mileage: 900125, ObservedAt: &now, Note: "Dashboard observation", ActorName: "Fixture"})
	if err != nil {
		t.Fatal("manual observation", err)
	}
	selectSource("manual", 2)
	reading, err = m.CachedMaintenanceOdometer(ctx, truck)
	if err != nil || reading == nil || reading.Source != "manual" || reading.Mileage != 900125 {
		t.Fatal("manual autofill", reading, err)
	}
	manual.Mileage = 1900000
	if _, err = m.SaveReading(ctx, manual); !errors.Is(err, ErrMaintenanceInvalid) {
		t.Fatal("manual typo accepted", err)
	}
	var audits int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM access_audit WHERE action='maintenance_odometer_source' AND target_id=$1`, truck).Scan(&audits); err != nil || audits != 3 {
		t.Fatal("missing audit", audits, err)
	}
	// An explicit valid provider choice may settle a provider-only disagreement.
	const otherVIN = "1M8GDM9AXKP042795"
	var other string
	if err = admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,vin,active) VALUES('SOURCE-DISAGREEMENT',$1,true) RETURNING id::text`, otherVIN).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if err = NewSamsaraRepository(pool).Store(ctx, samsara.Snapshot{Fetched: 1, Ambiguous: []string{}, Vehicles: []samsara.Vehicle{{ID: "disagreement", VIN: otherVIN, Name: "Fixture", Odometer: &samsara.Odometer{Miles: 500000, Time: now}}}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = m.StoreMaintenanceTelemetry(ctx, MaintenanceTelemetryTruck{other, otherVIN}, &MaintenanceTelemetry{Mileage: 500100, ReportedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err = m.SaveOdometerSource(ctx, other, OdometerSourceInput{Source: "five_eld", ActorName: "Fixture"}); err != nil {
		t.Fatal("provider disagreement cannot be resolved", err)
	}
	reading, err = m.CachedMaintenanceOdometer(ctx, other)
	if err != nil || reading == nil || reading.Mileage != 500100 {
		t.Fatal("explicit provider was ignored", reading, err)
	}
}
