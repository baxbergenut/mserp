package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type MaintenanceReading struct {
	ID        string    `json:"id"`
	TruckID   string    `json:"truckId"`
	RecordID  *string   `json:"recordId"`
	Date      string    `json:"date"`
	Mileage   int       `json:"mileage"`
	Source    string    `json:"source"`
	Version   int       `json:"version"`
	ActorName string    `json:"actorName"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type MaintenanceSchedule struct {
	ID            string  `json:"id"`
	TruckID       *string `json:"truckId"`
	ServiceCode   string  `json:"serviceCode"`
	IntervalMiles *int    `json:"intervalMiles"`
	IntervalDays  *int    `json:"intervalDays"`
	WarningMiles  int     `json:"warningMiles"`
	WarningDays   int     `json:"warningDays"`
	Enabled       bool    `json:"enabled"`
	Version       int     `json:"version"`
}
type MaintenanceDue struct {
	LastRecordID *string              `json:"lastRecordId"`
	TruckID      string               `json:"truckId"`
	UnitNumber   string               `json:"unitNumber"`
	ServiceCode  string               `json:"serviceCode"`
	Status       string               `json:"status"`
	Reason       string               `json:"reason"`
	LastDate     *string              `json:"lastDate"`
	LastMiles    *int                 `json:"lastMiles"`
	CurrentDate  *string              `json:"currentDate"`
	CurrentMiles *int                 `json:"currentMiles"`
	NextDate     *string              `json:"nextDate"`
	NextMiles    *int                 `json:"nextMiles"`
	Schedule     *MaintenanceSchedule `json:"schedule"`
}

func checkMaintenanceReading(ctx context.Context, tx pgx.Tx, truck string, date time.Time, miles int, record, id string) error {
	var conflict bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM maintenance_odometer_readings WHERE truck_id=$1 AND active AND ($4='' OR record_id IS DISTINCT FROM NULLIF($4,'')::uuid) AND id IS DISTINCT FROM NULLIF($5,'')::uuid AND ((reading_date<=$2 AND mileage>$3) OR (reading_date>=$2 AND mileage<$3)))`, truck, date, miles, record, id).Scan(&conflict)
	if err != nil {
		return err
	}
	if conflict {
		return fmt.Errorf("%w: odometer conflicts with a dated reading; review or correct the existing reading first", ErrMaintenanceInvalid)
	}
	return nil
}
func (r *MaintenanceRepository) Readings(ctx context.Context, truck string) ([]MaintenanceReading, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,truck_id,record_id,reading_date::text,mileage,source,version,actor_name,updated_at FROM maintenance_odometer_readings WHERE active AND ($1='' OR truck_id=NULLIF($1,'')::uuid) ORDER BY reading_date DESC,updated_at DESC LIMIT 500`, truck)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaintenanceReading{}
	for rows.Next() {
		var v MaintenanceReading
		if err := rows.Scan(&v.ID, &v.TruckID, &v.RecordID, &v.Date, &v.Mileage, &v.Source, &v.Version, &v.ActorName, &v.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *MaintenanceRepository) SaveReading(ctx context.Context, in MaintenanceReading) (MaintenanceReading, error) {
	date, err := time.Parse(time.DateOnly, in.Date)
	if err != nil || in.Mileage < 0 {
		return in, ErrMaintenanceInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer tx.Rollback(ctx)
	var truck string
	if err := tx.QueryRow(ctx, "SELECT id FROM trucks WHERE id=$1 FOR UPDATE", in.TruckID).Scan(&truck); err != nil {
		return in, mapNotFound(err)
	}
	var before []byte
	if in.ID != "" {
		if err := tx.QueryRow(ctx, "SELECT to_jsonb(r) FROM maintenance_odometer_readings r WHERE id=$1 AND truck_id=$2 AND version=$3 AND source='manual' FOR UPDATE", in.ID, in.TruckID, in.Version).Scan(&before); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return in, ErrMaintenanceConflict
			}
			return in, err
		}
	}
	if err := checkMaintenanceReading(ctx, tx, truck, date, in.Mileage, "", in.ID); err != nil {
		return in, err
	}
	var row pgx.Row
	if in.ID == "" {
		row = tx.QueryRow(ctx, `INSERT INTO maintenance_odometer_readings(truck_id,reading_date,mileage,source,actor_name) VALUES($1,$2,$3,'manual',$4) RETURNING id,truck_id,record_id,reading_date::text,mileage,source,version,actor_name,updated_at`, truck, date, in.Mileage, in.ActorName)
	} else {
		row = tx.QueryRow(ctx, `UPDATE maintenance_odometer_readings SET reading_date=$2,mileage=$3,actor_name=$4,version=version+1,updated_at=now() WHERE id=$1 RETURNING id,truck_id,record_id,reading_date::text,mileage,source,version,actor_name,updated_at`, in.ID, date, in.Mileage, in.ActorName)
	}
	var v MaintenanceReading
	if err := row.Scan(&v.ID, &v.TruckID, &v.RecordID, &v.Date, &v.Mileage, &v.Source, &v.Version, &v.ActorName, &v.UpdatedAt); err != nil {
		return v, err
	}
	after, _ := json.Marshal(v)
	if _, err := tx.Exec(ctx, "INSERT INTO maintenance_reading_changes(reading_id,actor_name,before_value,after_value) VALUES($1,$2,$3,$4)", v.ID, in.ActorName, before, after); err != nil {
		return v, err
	}
	if err := tx.Commit(ctx); err != nil {
		return v, err
	}
	return v, nil
}
func (r *MaintenanceRepository) Schedules(ctx context.Context) ([]MaintenanceSchedule, error) {
	return maintenanceSchedules(ctx, r.pool)
}
func maintenanceSchedules(ctx context.Context, q chargeQuery) ([]MaintenanceSchedule, error) {
	rows, err := q.Query(ctx, `SELECT id,truck_id,service_code,interval_miles,interval_days,warning_miles,warning_days,enabled,version FROM maintenance_schedules ORDER BY truck_id NULLS FIRST,service_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaintenanceSchedule{}
	for rows.Next() {
		var v MaintenanceSchedule
		if err := rows.Scan(&v.ID, &v.TruckID, &v.ServiceCode, &v.IntervalMiles, &v.IntervalDays, &v.WarningMiles, &v.WarningDays, &v.Enabled, &v.Version); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *MaintenanceRepository) SaveSchedule(ctx context.Context, in MaintenanceSchedule) (MaintenanceSchedule, error) {
	var row pgx.Row
	if in.ID == "" {
		row = r.pool.QueryRow(ctx, `INSERT INTO maintenance_schedules(truck_id,service_code,interval_miles,interval_days,warning_miles,warning_days,enabled) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,truck_id,service_code,interval_miles,interval_days,warning_miles,warning_days,enabled,version`, in.TruckID, in.ServiceCode, in.IntervalMiles, in.IntervalDays, in.WarningMiles, in.WarningDays, in.Enabled)
	} else {
		row = r.pool.QueryRow(ctx, `UPDATE maintenance_schedules SET interval_miles=$2,interval_days=$3,warning_miles=$4,warning_days=$5,enabled=$6,version=version+1 WHERE id=$1 AND version=$7 AND truck_id IS NOT DISTINCT FROM $8::uuid AND service_code=$9 RETURNING id,truck_id,service_code,interval_miles,interval_days,warning_miles,warning_days,enabled,version`, in.ID, in.IntervalMiles, in.IntervalDays, in.WarningMiles, in.WarningDays, in.Enabled, in.Version, in.TruckID, in.ServiceCode)
	}
	var v MaintenanceSchedule
	err := row.Scan(&v.ID, &v.TruckID, &v.ServiceCode, &v.IntervalMiles, &v.IntervalDays, &v.WarningMiles, &v.WarningDays, &v.Enabled, &v.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrMaintenanceConflict
	}
	return v, err
}
func (r *MaintenanceRepository) Due(ctx context.Context, truck string, today time.Time) ([]MaintenanceDue, error) {
	return maintenanceDue(ctx, r.pool, truck, today)
}
func maintenanceDue(ctx context.Context, q chargeQuery, truck string, today time.Time) ([]MaintenanceDue, error) {
	schedules, err := maintenanceSchedules(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT t.id,t.unit_number,x.code,
 (SELECT id::text FROM maintenance_records m WHERE m.truck_id=t.id AND m.status='completed' AND EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) s WHERE s->>'trackingCode'=x.code) ORDER BY service_date DESC,created_at DESC,m.id DESC LIMIT 1),
 (SELECT service_date::text FROM maintenance_records m WHERE m.truck_id=t.id AND m.status='completed' AND EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) s WHERE s->>'trackingCode'=x.code) ORDER BY service_date DESC,created_at DESC,m.id DESC LIMIT 1),
 (SELECT odometer FROM maintenance_records m WHERE m.truck_id=t.id AND m.status='completed' AND EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) s WHERE s->>'trackingCode'=x.code) ORDER BY service_date DESC,created_at DESC,m.id DESC LIMIT 1),
 (SELECT reading_date::text FROM maintenance_odometer_readings r WHERE r.truck_id=t.id AND r.active ORDER BY reading_date DESC,updated_at DESC LIMIT 1),
 (SELECT mileage FROM maintenance_odometer_readings r WHERE r.truck_id=t.id AND r.active ORDER BY reading_date DESC,updated_at DESC LIMIT 1)
 FROM trucks t CROSS JOIN (VALUES('pm'),('oil')) x(code) WHERE ($1='' AND t.active OR t.id=NULLIF($1,'')::uuid) ORDER BY t.unit_number,x.code`, truck)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaintenanceDue{}
	for rows.Next() {
		var v MaintenanceDue
		if err := rows.Scan(&v.TruckID, &v.UnitNumber, &v.ServiceCode, &v.LastRecordID, &v.LastDate, &v.LastMiles, &v.CurrentDate, &v.CurrentMiles); err != nil {
			return nil, err
		}
		for _, s := range schedules {
			if s.ServiceCode == v.ServiceCode && (s.TruckID == nil || *s.TruckID == v.TruckID) {
				if v.Schedule == nil || s.TruckID != nil {
					copy := s
					v.Schedule = &copy
				}
			}
		}
		calculateMaintenanceDue(&v, today)
		items = append(items, v)
	}
	return items, rows.Err()
}
func calculateMaintenanceDue(v *MaintenanceDue, today time.Time) {
	v.Status = "missing_information"
	v.Reason = ""
	v.NextDate = nil
	v.NextMiles = nil
	if v.Schedule == nil {
		v.Reason = "Configure a service interval"
		return
	}
	s := v.Schedule
	if !s.Enabled {
		v.Status = "disabled"
		v.Reason = "Schedule disabled"
		return
	}
	if v.LastDate == nil {
		v.Reason = "No confirmed completion or baseline"
		return
	}
	last, err := time.Parse(time.DateOnly, *v.LastDate)
	if err != nil {
		v.Reason = "Invalid completion date"
		return
	}
	missing := false
	overdue := false
	due := false
	upcoming := false
	if s.IntervalDays != nil {
		date := last.AddDate(0, 0, *s.IntervalDays).Format(time.DateOnly)
		v.NextDate = &date
		delta := int(last.AddDate(0, 0, *s.IntervalDays).Sub(today).Hours() / 24)
		overdue = delta < 0
		due = delta == 0
		upcoming = delta <= s.WarningDays
	}
	if s.IntervalMiles != nil {
		if v.LastMiles == nil || v.CurrentMiles == nil {
			missing = true
		} else {
			miles := *v.LastMiles + *s.IntervalMiles
			v.NextMiles = &miles
			delta := miles - *v.CurrentMiles
			overdue = overdue || delta < 0
			due = due || delta == 0
			upcoming = upcoming || delta <= s.WarningMiles
		}
	}
	// A stale reading cannot establish that mileage-based maintenance is still current.
	if s.IntervalMiles != nil && v.CurrentDate != nil {
		date, _ := time.Parse(time.DateOnly, *v.CurrentDate)
		if today.Sub(date) > 7*24*time.Hour {
			missing = true
		}
	}
	switch {
	case overdue:
		v.Status = "overdue"
	case due:
		v.Status = "due"
	case missing:
		v.Reason = "A recent dated odometer reading or completion mileage is missing"
	case upcoming:
		v.Status = "upcoming"
	default:
		v.Status = "current"
	}
}
