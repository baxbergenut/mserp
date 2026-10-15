package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// RefreshMaintenanceTasks reconciles each service cycle once. Missing telemetry
// never silently resolves an existing reminder. Reads of the task board do not
// perform work or collect payments.
func (r *MaintenanceRepository) RefreshMaintenanceTasks(ctx context.Context, today time.Time, live map[string]MaintenanceReading) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(730101)"); err != nil {
		return err
	}
	due, err := maintenanceDue(ctx, tx, "", today)
	if err != nil {
		return err
	}
	type reminder struct {
		id, truck, service, baseline string
		miles                        *int
		date                         *string
	}
	rows, err := tx.Query(ctx, `SELECT id::text,maintenance_truck_id::text,maintenance_service,coalesce(maintenance_baseline_id::text,''),maintenance_observed_miles,maintenance_observed_date::text FROM system_task_records WHERE kind IN ('maintenance_pm','maintenance_oil') AND completed_at IS NULL FOR UPDATE`)
	if err != nil {
		return err
	}
	open := map[string]reminder{}
	for rows.Next() {
		var v reminder
		if err = rows.Scan(&v.id, &v.truck, &v.service, &v.baseline, &v.miles, &v.date); err != nil {
			rows.Close()
			return err
		}
		open[v.truck+":"+v.service] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	closeTask := func(id, outcome string) error {
		_, e := tx.Exec(ctx, `UPDATE system_task_records SET completed_at=now(),completed_by_name='System',outcome=$2 WHERE id=$1`, id, outcome)
		return e
	}
	for _, d := range due {
		key := d.TruckID + ":" + d.ServiceCode
		old, exists := open[key]
		delete(open, key)
		baseline := ""
		if d.LastRecordID != nil {
			baseline = *d.LastRecordID
		}
		// Retain the mileage that established this reminder across telemetry outages.
		// It belongs only to this completion cycle and is never a service reading.
		if exists && old.baseline == baseline && old.miles != nil && (d.CurrentMiles == nil || *old.miles > *d.CurrentMiles) {
			d.CurrentMiles = old.miles
			d.CurrentDate = old.date
			calculateMaintenanceDue(&d, today)
		}
		if reading, ok := live[d.TruckID]; ok && (d.CurrentDate == nil || reading.Date >= *d.CurrentDate) && (d.CurrentMiles == nil || reading.Mileage >= *d.CurrentMiles) {
			d.CurrentMiles = &reading.Mileage
			d.CurrentDate = &reading.Date
			calculateMaintenanceDue(&d, today)
		}
		alert := d.Status == "upcoming" || d.Status == "due" || d.Status == "overdue"
		if exists && (old.baseline != baseline || (!alert && d.Status != "missing_information")) {
			outcome := "Reminder no longer due"
			if baseline != "" && old.baseline != baseline {
				outcome = "Service recorded; previous reminder resolved"
			}
			if err = closeTask(old.id, outcome); err != nil {
				return err
			}
			exists = false
		}
		if !alert {
			continue
		}
		service := "PM service"
		if d.ServiceCode == "oil" {
			service = "Oil change"
		}
		title := fmt.Sprintf("%s %s · Truck %s", service, strings.ReplaceAll(d.Status, "_", " "), d.UnitNumber)
		notes := []string{"Record completed service in Work orders to resolve this reminder."}
		if d.NextDate != nil {
			notes = append(notes, "Due date: "+*d.NextDate)
		}
		if d.NextMiles != nil {
			notes = append(notes, fmt.Sprintf("Due odometer: %d mi", *d.NextMiles))
		}
		if d.CurrentMiles != nil {
			notes = append(notes, fmt.Sprintf("Current reading: %d mi (%s)", *d.CurrentMiles, valueOrEmpty(d.CurrentDate)))
		}
		note := strings.Join(notes, "\n")
		if exists {
			_, err = tx.Exec(ctx, `UPDATE system_task_records SET title=$2,notes=$3,maintenance_observed_miles=$4,maintenance_observed_date=$5::date WHERE id=$1 AND (title,notes,maintenance_observed_miles,maintenance_observed_date) IS DISTINCT FROM ($2,$3,$4::integer,$5::date)`, old.id, title, note, d.CurrentMiles, d.CurrentDate)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO system_task_records(id,kind,title,notes,maintenance_truck_id,maintenance_service,maintenance_baseline_id,created_at,maintenance_observed_miles,maintenance_observed_date) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,NULLIF($6,'')::uuid,now(),$7,$8::date)`, "maintenance_"+d.ServiceCode, title, note, d.TruckID, d.ServiceCode, baseline, d.CurrentMiles, d.CurrentDate)
		}
		if err != nil {
			return err
		}
	}
	for _, old := range open {
		if err = closeTask(old.id, "Truck no longer active"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return "date unknown"
	}
	return *v
}
