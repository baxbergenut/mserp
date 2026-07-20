package repository

import (
	"context"
	"errors"
	"sort"
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
	DriverID                *string
	DispatcherID            *string
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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// DataTruck normally returns newest loads first. Sort defensively so an
	// older load can never overwrite a newer driver/truck assignment.
	sort.SliceStable(records, func(left, right int) bool {
		leftCreated := records[left].CreatedDatetime
		rightCreated := records[right].CreatedDatetime
		if leftCreated == nil {
			return false
		}
		if rightCreated == nil {
			return true
		}
		return leftCreated.After(*rightCreated)
	})

	assignments := make([]truckAssignment, 0)
	assignedDrivers := make(map[string]struct{})
	assignedTrucks := make(map[string]struct{})
	for index := range records {
		record := &records[index]
		if record.DispatcherName != nil {
			dispatcherID, ensureErr := ensureDispatcher(ctx, tx, *record.DispatcherName)
			if ensureErr != nil {
				return ensureErr
			}
			record.DispatcherID = &dispatcherID
		}
		if record.DriverName != nil {
			driverID, ensureErr := ensureDriver(ctx, tx, *record.DriverName, record.DispatcherID)
			if ensureErr != nil {
				return ensureErr
			}
			record.DriverID = &driverID
		}
		if record.TeamDriverName != nil {
			if _, ensureErr := ensureDriver(ctx, tx, *record.TeamDriverName, record.DispatcherID); ensureErr != nil {
				return ensureErr
			}
		}
		var truckID *string
		if record.TruckUnit != nil {
			ensuredTruckID, ensureErr := ensureTruck(ctx, tx, *record.TruckUnit)
			if ensureErr != nil {
				return ensureErr
			}
			truckID = &ensuredTruckID
		}
		if record.DriverID != nil && truckID != nil {
			_, driverAlreadyAssigned := assignedDrivers[*record.DriverID]
			_, truckAlreadyAssigned := assignedTrucks[*truckID]
			if !driverAlreadyAssigned && !truckAlreadyAssigned {
				assignments = append(assignments, truckAssignment{
					driverID: *record.DriverID,
					truckID:  *truckID,
				})
				assignedDrivers[*record.DriverID] = struct{}{}
				assignedTrucks[*truckID] = struct{}{}
			}
		}
	}
	for _, assignment := range assignments {
		if err := syncTruckAssignment(ctx, tx, assignment.truckID, assignment.driverID); err != nil {
			return err
		}
	}

	batch := &pgx.Batch{}
	for _, record := range records {
		batch.Queue(upsertLoadSQL,
			record.ID,
			record.LoadID,
			record.DriverID,
			record.DispatcherID,
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

	results := tx.SendBatch(ctx, batch)

	for range records {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}

	return tx.Commit(ctx)
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
		DispatcherName:          formatPersonNamePtr(load.DispatcherFullName),
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
		record.ShipmentID = trimmedStringPtr(load.ShipmentID)
	}
	if load.Trip != nil {
		record.DriverName = formatPersonNamePtr(load.Trip.DriverFullName)
		record.TeamDriverName = formatPersonNamePtr(load.Trip.TeamDriverFullName)
		record.TruckUnit = normalizeTruckUnitPtr(load.Trip.TruckUnitNumber)
	}
	if load.AssignedDriverNTruck != nil {
		if record.DriverName == nil {
			record.DriverName = formatPersonNamePtr(load.AssignedDriverNTruck.DriverFullName)
		}
		if record.TruckUnit == nil {
			record.TruckUnit = normalizeTruckUnitPtr(load.AssignedDriverNTruck.TruckUnitNumber)
		}
	}

	return record, nil
}

const upsertLoadSQL = `
INSERT INTO loads (
	id, load_id, driver_id, dispatcher_id, shipment_id, status,
	load_pay, total_other_pay, total_pay, total_miles, per_mile_revenue,
	dispatcher_name, driver_name, team_driver_name, truck_unit, customer_name,
	pickup_time, delivery_time, pickup_appointment_time, delivery_appointment_time,
	created_datetime, synced_at, raw_payload
) VALUES (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9, $10, $11,
	$12, $13, $14, $15, $16,
	$17, $18, $19, $20,
	$21, $22, $23
)
ON CONFLICT (id) DO UPDATE SET
	load_id = EXCLUDED.load_id,
	driver_id = EXCLUDED.driver_id,
	dispatcher_id = EXCLUDED.dispatcher_id,
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

func trimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
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
func (r *LoadRepository) GetLoads(ctx context.Context) ([]LoadRecord, error) {
	rows, err := r.pool.Query(ctx, selectLoadsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []LoadRecord
	for rows.Next() {
		var rec LoadRecord
		if err := rows.Scan(
			&rec.ID,
			&rec.LoadID,
			&rec.DriverID,
			&rec.DispatcherID,
			&rec.ShipmentID,
			&rec.Status,
			&rec.LoadPay,
			&rec.TotalOtherPay,
			&rec.TotalPay,
			&rec.TotalMiles,
			&rec.PerMileRevenue,
			&rec.DispatcherName,
			&rec.DriverName,
			&rec.TeamDriverName,
			&rec.TruckUnit,
			&rec.CustomerName,
			&rec.PickupTime,
			&rec.DeliveryTime,
			&rec.PickupAppointmentTime,
			&rec.DeliveryAppointmentTime,
			&rec.CreatedDatetime,
			&rec.SyncedAt,
		); err != nil {
			return nil, err
		}
		rec.DispatcherName = formatPersonNamePtr(rec.DispatcherName)
		rec.DriverName = formatPersonNamePtr(rec.DriverName)
		rec.TeamDriverName = formatPersonNamePtr(rec.TeamDriverName)
		rec.TruckUnit = normalizeTruckUnitPtr(rec.TruckUnit)
		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

const selectLoadsSQL = `
SELECT
	id, load_id, driver_id, dispatcher_id, shipment_id, status,
	load_pay, total_other_pay, total_pay, total_miles, per_mile_revenue,
	dispatcher_name, driver_name, team_driver_name, truck_unit, customer_name,
	pickup_time, delivery_time, pickup_appointment_time, delivery_appointment_time,
	created_datetime, synced_at
FROM loads
ORDER BY id`

var ErrMissingLoadID = errors.New("datatruck load is missing load_id")

func ensureDispatcher(ctx context.Context, tx pgx.Tx, name string) (string, error) {
	displayName := formatPersonName(name)
	normalizedName := normalizeName(displayName)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "dispatcher:"+normalizedName); err != nil {
		return "", err
	}

	var id string
	err := tx.QueryRow(ctx, `
		SELECT id FROM dispatchers
		WHERE normalized_name = $1
		ORDER BY created_at, id
		LIMIT 1`, normalizedName).Scan(&id)
	if err == nil {
		_, err = tx.Exec(ctx, `
			UPDATE dispatchers SET full_name = $2, updated_at = now()
			WHERE id = $1 AND full_name IS DISTINCT FROM $2`, id, displayName)
		return id, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO dispatchers (full_name, normalized_name)
		VALUES ($1, $2)
		RETURNING id`, displayName, normalizedName).Scan(&id)
	return id, err
}

func ensureDriver(ctx context.Context, tx pgx.Tx, name string, dispatcherID *string) (string, error) {
	displayName := formatPersonName(name)
	normalizedName := normalizeName(displayName)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "driver:"+normalizedName); err != nil {
		return "", err
	}

	var id string
	err := tx.QueryRow(ctx, `
		SELECT id FROM drivers
		WHERE normalized_name = $1
		ORDER BY created_at, id
		LIMIT 1`, normalizedName).Scan(&id)
	if err == nil {
		if _, err = tx.Exec(ctx, `
			UPDATE drivers SET full_name = $2, updated_at = now()
			WHERE id = $1 AND full_name IS DISTINCT FROM $2`, id, displayName); err != nil {
			return "", err
		}
		if dispatcherID != nil {
			_, err = tx.Exec(ctx, `
				UPDATE drivers
				SET dispatcher_id = COALESCE(dispatcher_id, $2), updated_at = now()
				WHERE id = $1 AND dispatcher_id IS NULL`, id, *dispatcherID)
		}
		return id, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO drivers (
			full_name, normalized_name, is_owner_operator, pay_type, pay_rate,
			dispatcher_id, active
		) VALUES ($1, $2, false, 'cpm', 0, $3, true)
		RETURNING id`, displayName, normalizedName, dispatcherID).Scan(&id)
	return id, err
}

type truckAssignment struct {
	truckID  string
	driverID string
}

func ensureTruck(ctx context.Context, tx pgx.Tx, unitNumber string) (string, error) {
	canonicalUnit := normalizeTruckUnit(unitNumber)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "truck:"+canonicalUnit); err != nil {
		return "", err
	}

	var id string
	err := tx.QueryRow(ctx, `
		SELECT id FROM trucks
		WHERE upper(regexp_replace(btrim(unit_number), '[[:space:]]+', ' ', 'g')) = $1
		ORDER BY created_at, id
		LIMIT 1`, canonicalUnit).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO trucks (unit_number, is_company_owned, status, active)
		VALUES ($1, true, 'available', true)
		RETURNING id`, canonicalUnit).Scan(&id)
	return id, err
}

func syncTruckAssignment(ctx context.Context, tx pgx.Tx, truckID, driverID string) error {
	var unchanged bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM truck_driver_assignments
			WHERE truck_id = $1 AND driver_id = $2 AND unassigned_at IS NULL
		)`, truckID, driverID).Scan(&unchanged); err != nil {
		return err
	}
	if unchanged {
		return nil
	}
	return assignTruck(ctx, tx, truckID, driverID)
}
