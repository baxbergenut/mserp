package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func testMaintenanceManualPM(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	repo := NewMaintenanceRepository(pool)
	var truck string
	if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('MANUAL-PM') RETURNING id::text`).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProgramTrucks(ctx, OilChangeProgramID, []ProgramTruckInput{{TruckID: truck, Active: true}}, ""); err != nil {
		t.Fatal(err)
	}
	summary := func() PreventativeTruck {
		t.Helper()
		page, err := repo.Preventative(ctx, "MANUAL-PM", false, 1, 25)
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		return page.Items[0]
	}
	miles := 1000
	input := MaintenancePMInput{TruckID: truck, LastDate: "2026-10-01", LastMiles: &miles, ActorName: "Operator"}
	if err := repo.SavePMBaseline(ctx, input); err != nil {
		t.Fatal(err)
	}
	saved := summary()
	if saved.LastDate == nil || *saved.LastDate != input.LastDate || saved.LastMiles == nil || *saved.LastMiles != 1000 || saved.RecordID != nil || saved.BaselineVersion != 1 {
		t.Fatal(saved)
	}
	if err := repo.SavePMBaseline(ctx, input); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatal("stale write accepted", err)
	}
	input.BaselineVersion = 1
	input.LastDate = "2026-09-30"
	input.LastMiles = nil
	if err := repo.SavePMBaseline(ctx, input); err != nil {
		t.Fatal(err)
	}
	saved = summary()
	if saved.LastMiles != nil || *saved.LastDate != "2026-09-30" {
		t.Fatal(saved)
	}
	var record string
	if err := admin.QueryRow(ctx, `INSERT INTO maintenance_records(truck_id,status,service_date,odometer,services) VALUES($1,'completed','2026-10-02',1100,'[{"typeCode":"preventive","name":"PM Service","amount":null,"trackingCode":"pm"}]') RETURNING id::text`, truck).Scan(&record); err != nil {
		t.Fatal(err)
	}
	saved = summary()
	if *saved.LastDate != "2026-10-02" || *saved.LastMiles != 1100 {
		t.Fatal("new completed work did not supersede baseline", saved)
	}
	input.RecordID = saved.RecordID
	input.RecordVersion = saved.RecordVersion
	input.BaselineVersion = saved.BaselineVersion
	input.LastDate = "2026-10-01"
	input.LastMiles = &miles
	if err := repo.SavePMBaseline(ctx, input); err != nil {
		t.Fatal(err)
	}
	due, err := repo.Due(ctx, truck, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	if err != nil || len(due) != 1 || *due[0].LastDate != "2026-10-01" || *due[0].LastMiles != 1000 || *due[0].CycleID == record || *due[0].LastRecordID != record {
		t.Fatal("reminders did not use corrected PM cycle", due, err)
	}
	var original int
	if err := admin.QueryRow(ctx, `SELECT odometer FROM maintenance_records WHERE id=$1`, record).Scan(&original); err != nil || original != 1100 {
		t.Fatal("baseline changed historical service", original, err)
	}
	var audits int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM access_audit WHERE target_id=$1 AND action='maintenance_pm_baseline'`, truck).Scan(&audits); err != nil || audits != 3 {
		t.Fatal("missing correction audit", audits, err)
	}
	_, err = repo.SaveReading(ctx, MaintenanceReading{TruckID: truck, Date: "2026-10-10", Mileage: 1200, ActorName: "Operator"})
	if err != nil {
		t.Fatal(err)
	}
	interval := 100
	if _, err := repo.SaveSchedule(ctx, MaintenanceSchedule{TruckID: &truck, ServiceCode: "pm", IntervalMiles: &interval, WarningMiles: 10, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	if err := repo.RefreshMaintenanceTasks(ctx, today, nil); err != nil {
		t.Fatal(err)
	}
	var open int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM system_task_records WHERE maintenance_truck_id=$1 AND completed_at IS NULL AND maintenance_baseline_id=$2`, truck, due[0].CycleID).Scan(&open); err != nil || open != 1 {
		t.Fatal("manual PM reminder cycle missing", open, err)
	}
	saved = summary()
	input.BaselineVersion = saved.BaselineVersion
	miles = 1300
	if err := repo.SavePMBaseline(ctx, input); !errors.Is(err, ErrMaintenanceInvalid) {
		t.Fatal("decreasing chronology accepted", err)
	}
	input.LastDate = "2999-01-01"
	if err := repo.SavePMBaseline(ctx, input); !errors.Is(err, ErrMaintenanceInvalid) {
		t.Fatal("future date accepted", err)
	}
	input.LastDate = "2026-10-10"
	miles = 1200
	if err := repo.SavePMBaseline(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := repo.RefreshMaintenanceTasks(ctx, today, nil); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM system_task_records WHERE maintenance_truck_id=$1 AND completed_at IS NULL`, truck).Scan(&open); err != nil || open != 0 {
		t.Fatal("manual completion did not resolve reminder", open, err)
	}
	if _, err := repo.SaveReading(ctx, MaintenanceReading{TruckID: truck, Date: "2026-10-11", Mileage: 1100, ActorName: "Operator"}); !errors.Is(err, ErrMaintenanceInvalid) {
		t.Fatal("manual reading bypassed baseline chronology", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE trucks SET active=false WHERE id=$1`, truck); err != nil {
		t.Fatal(err)
	}
}
