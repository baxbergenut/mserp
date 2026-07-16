package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/datatruck"
)

type LoadRepository struct {
	pool *pgxpool.Pool
}

func NewLoadRepository(pool *pgxpool.Pool) *LoadRepository {
	return &LoadRepository{pool: pool}
}

type LoadRecord struct {
	ID                      int
	LoadID                  string
	ShipmentID              *string
	Status                  string
	LoadPay                 string
	TotalOtherPay           string
	TotalPay                string
	TotalMiles              *string
	PerMileRevenue          *string
	DispatcherName          *string
	DriverName              *string
	TeamDriverName          *string
	TruckUnit               *string
	CustomerName            *string
	PickupTime              *time.Time
	DeliveryTime            *time.Time
	PickupAppointmentTime   *time.Time
	DeliveryAppointmentTime *time.Time
	CreatedDatetime         *time.Time
	SyncedAt                time.Time
	RawPayload              []byte
}

func (r *LoadRepository) UpsertLoads(ctx context.Context, records []LoadRecord) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, record := range records {
		batch.Queue(upsertLoadSQL,
			record.ID,
			record.LoadID,
			nullableString(record.ShipmentID),
			record.Status,
			record.LoadPay,
			record.TotalOtherPay,
			record.TotalPay,
			nullableString(record.TotalMiles),
			nullableString(record.PerMileRevenue),
			nullableString(record.DispatcherName),
			nullableString(record.DriverName),
			nullableString(record.TeamDriverName),
			nullableString(record.TruckUnit),
			nullableString(record.CustomerName),
			record.PickupTime,
			record.DeliveryTime,
			record.PickupAppointmentTime,
			record.DeliveryAppointmentTime,
			record.CreatedDatetime,
			record.SyncedAt,
			record.RawPayload,
		)
	}

	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()

	for range records {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}

	return nil
}

func (r *LoadRepository) HealthCheck(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func LoadToRecord(load datatruck.Load, payload []byte, syncedAt time.Time) (LoadRecord, error) {
	if load.LoadID == nil || strings.TrimSpace(*load.LoadID) == "" {
		return LoadRecord{}, ErrMissingLoadID
	}

	record := LoadRecord{
		ID:                      load.ID,
		LoadID:                  strings.TrimSpace(*load.LoadID),
		Status:                  load.Status,
		LoadPay:                 flexibleStringOrDefault(load.LoadPay, "0"),
		TotalOtherPay:           flexibleStringOrDefault(load.TotalOtherPay, "0"),
		TotalPay:                flexibleStringOrDefault(load.TotalPay, "0"),
		TotalMiles:              flexibleStringPtr(load.TotalMiles),
		PerMileRevenue:          flexibleStringPtr(load.PerMileRevenue),
		DispatcherName:          load.DispatcherFullName,
		CustomerName:            load.CustomerCompanyName,
		PickupTime:              load.PickupTime,
		DeliveryTime:            load.DeliveryTime,
		PickupAppointmentTime:   load.PickupAppointmentTime,
		DeliveryAppointmentTime: load.DeliveryAppointmentTime,
		CreatedDatetime:         load.CreatedDatetime,
		SyncedAt:                syncedAt.UTC(),
		RawPayload:              payload,
	}

	if load.ShipmentID != nil {
		record.ShipmentID = load.ShipmentID
	}
	if load.Trip != nil {
		record.DriverName = load.Trip.DriverFullName
		record.TeamDriverName = load.Trip.TeamDriverFullName
		record.TruckUnit = load.Trip.TruckUnitNumber
	}
	if load.AssignedDriverNTruck != nil {
		if record.DriverName == nil {
			record.DriverName = load.AssignedDriverNTruck.DriverFullName
		}
		if record.TruckUnit == nil {
			record.TruckUnit = load.AssignedDriverNTruck.TruckUnitNumber
		}
	}

	return record, nil
}

const upsertLoadSQL = `
INSERT INTO loads (
	id, load_id, shipment_id, status,
	load_pay, total_other_pay, total_pay, total_miles, per_mile_revenue,
	dispatcher_name, driver_name, team_driver_name, truck_unit, customer_name,
	pickup_time, delivery_time, pickup_appointment_time, delivery_appointment_time,
	created_datetime, synced_at, raw_payload
) VALUES (
	$1, $2, $3, $4,
	$5, $6, $7, $8, $9,
	$10, $11, $12, $13, $14,
	$15, $16, $17, $18,
	$19, $20, $21
)
ON CONFLICT (id) DO UPDATE SET
	load_id = EXCLUDED.load_id,
	shipment_id = EXCLUDED.shipment_id,
	status = EXCLUDED.status,
	load_pay = EXCLUDED.load_pay,
	total_other_pay = EXCLUDED.total_other_pay,
	total_pay = EXCLUDED.total_pay,
	total_miles = EXCLUDED.total_miles,
	per_mile_revenue = EXCLUDED.per_mile_revenue,
	dispatcher_name = EXCLUDED.dispatcher_name,
	driver_name = EXCLUDED.driver_name,
	team_driver_name = EXCLUDED.team_driver_name,
	truck_unit = EXCLUDED.truck_unit,
	customer_name = EXCLUDED.customer_name,
	pickup_time = EXCLUDED.pickup_time,
	delivery_time = EXCLUDED.delivery_time,
	pickup_appointment_time = EXCLUDED.pickup_appointment_time,
	delivery_appointment_time = EXCLUDED.delivery_appointment_time,
	created_datetime = EXCLUDED.created_datetime,
	synced_at = EXCLUDED.synced_at,
	raw_payload = EXCLUDED.raw_payload`

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func flexibleStringPtr(value *datatruck.FlexibleString) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(value.String())
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func flexibleStringOrDefault(value *datatruck.FlexibleString, fallback string) string {
	if value == nil {
		return fallback
	}
	trimmed := strings.TrimSpace(value.String())
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func stringOrDefault(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

var ErrMissingLoadID = errors.New("datatruck load is missing load_id")
