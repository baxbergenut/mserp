package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func testMaintenanceBackgroundTelemetry(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var truck string
	if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,vin,active) VALUES('TELEMETRY-TEST','1M8GDM9AXKP042788',true) RETURNING id::text`).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	repo := NewMaintenanceRepository(pool)
	candidates, err := repo.MaintenanceTelemetryCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var candidate MaintenanceTelemetryTruck
	for _, v := range candidates {
		if v.TruckID == truck {
			candidate = v
		}
	}
	if candidate.TruckID == "" {
		t.Fatal("trucks without schedules must still sync")
	}
	zone, _ := time.LoadLocation("America/New_York")
	date := time.Now().In(zone).AddDate(0, 0, -1).Format(time.DateOnly)
	exec(`INSERT INTO maintenance_records(truck_id,status,service_date,odometer,services) VALUES($1,'completed',$2,1000,'[{"typeCode":"preventive","name":"PM Service","amount":null,"trackingCode":"pm"},{"typeCode":"oil","name":"Oil change","amount":null,"trackingCode":"oil"}]')`, truck, date)
	for code, interval := range map[string]int{"pm": 100, "oil": 200} {
		if _, err := repo.SaveSchedule(ctx, MaintenanceSchedule{TruckID: &truck, ServiceCode: code, IntervalMiles: &interval, Enabled: true, WarningMiles: 10}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Add(-time.Minute)
	if err := repo.StoreMaintenanceTelemetry(ctx, candidate, &MaintenanceTelemetry{Mileage: 1095, ReportedAt: now}); err != nil {
		t.Fatal(err)
	}
	check := func(code, status string, next, remaining int) {
		t.Helper()
		page, err := repo.Preventative(ctx, code, "TELEMETRY-TEST", false, 1, 25)
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		v := page.Items[0]
		if v.NextMiles == nil || *v.NextMiles != next || v.RemainingMiles == nil || *v.RemainingMiles != remaining || v.Status != status {
			t.Fatal("incorrect service interval", v)
		}
	}
	check("pm", "upcoming", 1100, 5)
	check("oil", "current", 1200, 105)
	if err := repo.StoreMaintenanceTelemetry(ctx, candidate, nil); err != nil {
		t.Fatal(err)
	}
	cached, err := repo.CachedMaintenanceOdometer(ctx, truck)
	if err != nil || cached == nil || cached.Mileage != 1095 {
		t.Fatal("outage erased known reading", cached, err)
	}
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM maintenance_odometer_readings WHERE truck_id=$1`, truck).Scan(&count); err != nil || count != 0 {
		t.Fatal("telemetry invented a service reading", count, err)
	}
	if err := repo.StoreMaintenanceTelemetry(ctx, candidate, &MaintenanceTelemetry{Mileage: 1101, ReportedAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	check("pm", "overdue", 1100, -1)
	exec(`UPDATE maintenance_telemetry SET reported_at=now()-interval '2 hours' WHERE truck_id=$1`, truck)
	cached, err = repo.CachedMaintenanceOdometer(ctx, truck)
	if err != nil || cached != nil {
		t.Fatal("stale reading filled new service", cached, err)
	}
	exec(`UPDATE trucks SET vin='1M8GDM9AXKP042789' WHERE id=$1`, truck)
	cached, err = repo.CachedMaintenanceOdometer(ctx, truck)
	if err != nil || cached != nil {
		t.Fatal("changed VIN reused old cache", cached, err)
	}
	exec(`INSERT INTO trucks(unit_number,vin,active) VALUES('TELEMETRY-DUPLICATE','1m8gdm9axkp042789',false)`)
	candidates, err = repo.MaintenanceTelemetryCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range candidates {
		if v.TruckID == truck {
			t.Fatal("ambiguous VIN was polled")
		}
	}
	exec(`UPDATE trucks SET active=false WHERE id=$1`, truck)
}
