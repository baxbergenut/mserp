package repository

import (
	"context"
	"errors"
	"fmt"
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

// Discovery, reconciliation and refresh only maintain imported load records.
// Fleet profiles and truck/dispatcher assignments are managed by MSERP users.
func (r *LoadRepository) UpsertLoads(ctx context.Context, records []LoadRecord) error {
	return r.refreshLoads(ctx, records, true)
}

func (r *LoadRepository) HealthCheck(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *LoadRepository) MaxLoadID(ctx context.Context) (int, error) {
	var id int
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(MAX(id), 0) FROM loads`).Scan(&id)
	return id, err
}

func LoadToRecord(load datatruck.Load, payload []byte, syncedAt time.Time) (LoadRecord, error) {
	if load.ID <= 0 {
		return LoadRecord{}, ErrMissingLoadRecordID
	}
	// load_id is a display number, not the upstream identity. Unnumbered
	// orders must not block the entire sync or disappear from reporting.
	loadID := fmt.Sprintf("DataTruck #%d", load.ID)
	if load.LoadID != nil && strings.TrimSpace(*load.LoadID) != "" {
		loadID = strings.TrimSpace(*load.LoadID)
	}

	record := LoadRecord{
		ID:                      load.ID,
		LoadID:                  loadID,
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
	rows, err := r.pool.Query(ctx, selectLoadsSQL+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []LoadRecord
	for rows.Next() {
		rec, err := scanLoad(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

type LoadPageQuery struct {
	Pagination Pagination
	Search     string
	Status     string
	Customer   string
	Dispatcher string
	Driver     string
	PickupFrom *time.Time
	PickupTo   *time.Time
	Sort       string
	Direction  string
}

type LoadFilterOptions struct {
	Statuses    []string `json:"statuses"`
	Customers   []string `json:"customers"`
	Dispatchers []string `json:"dispatchers"`
	Drivers     []string `json:"drivers"`
}

type LoadPage struct {
	Page[LoadRecord]
	Options LoadFilterOptions `json:"options"`
}

func (r *LoadRepository) GetLoadsPage(ctx context.Context, query LoadPageQuery) (LoadPage, error) {
	const where = `
WHERE ($1 = '' OR concat_ws(' ', load_id, shipment_id, customer_name,
	driver_name, truck_unit, dispatcher_name) ILIKE '%' || $1 || '%')
	AND ($2 = '' OR lower(trim(status)) = $2)
	AND ($3 = '' OR customer_name = $3)
	AND ($4 = '' OR dispatcher_name = $4)
	AND ($5 = '' OR driver_name = $5)
	AND ($6::date IS NULL OR pickup_time >= $6)
	AND ($7::date IS NULL OR pickup_time < $7 + interval '1 day')`
	args := []any{
		query.Search, query.Status, query.Customer, query.Dispatcher, query.Driver,
		query.PickupFrom, query.PickupTo,
	}
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM loads "+where, args...).Scan(&total); err != nil {
		return LoadPage{}, err
	}
	query.Pagination = query.Pagination.Normalize(total)

	orderColumn := map[string]string{
		"PickupTime":     "pickup_time",
		"DeliveryTime":   "delivery_time",
		"TotalPay":       "total_pay",
		"TotalMiles":     "total_miles",
		"PerMileRevenue": "per_mile_revenue",
	}[query.Sort]
	if orderColumn == "" {
		orderColumn = "pickup_time"
	}
	direction := "DESC"
	if strings.EqualFold(query.Direction, "asc") {
		direction = "ASC"
	}
	pageArgs := append(args, query.Pagination.PageSize, query.Pagination.Offset())
	rows, err := r.pool.Query(ctx, selectLoadsSQL+where+" ORDER BY "+orderColumn+" "+direction+" NULLS LAST, id DESC LIMIT $8 OFFSET $9", pageArgs...)
	if err != nil {
		return LoadPage{}, err
	}
	defer rows.Close()
	records := make([]LoadRecord, 0, query.Pagination.PageSize)
	for rows.Next() {
		record, scanErr := scanLoad(rows)
		if scanErr != nil {
			return LoadPage{}, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return LoadPage{}, err
	}

	options := LoadFilterOptions{}
	err = r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(array_agg(DISTINCT lower(trim(status)) ORDER BY lower(trim(status))) FILTER (WHERE trim(status) <> ''), '{}'),
			COALESCE(array_agg(DISTINCT customer_name ORDER BY customer_name) FILTER (WHERE customer_name IS NOT NULL AND customer_name <> ''), '{}'),
			COALESCE(array_agg(DISTINCT dispatcher_name ORDER BY dispatcher_name) FILTER (WHERE dispatcher_name IS NOT NULL AND dispatcher_name <> ''), '{}'),
			COALESCE(array_agg(DISTINCT driver_name ORDER BY driver_name) FILTER (WHERE driver_name IS NOT NULL AND driver_name <> ''), '{}')
		FROM loads`).Scan(&options.Statuses, &options.Customers, &options.Dispatchers, &options.Drivers)
	if err != nil {
		return LoadPage{}, err
	}
	return LoadPage{Page: NewPage(records, total, query.Pagination), Options: options}, nil
}

func scanLoad(row rowScanner) (LoadRecord, error) {
	var rec LoadRecord
	err := row.Scan(
		&rec.ID, &rec.LoadID, &rec.DriverID, &rec.DispatcherID, &rec.ShipmentID,
		&rec.Status, &rec.LoadPay, &rec.TotalOtherPay, &rec.TotalPay,
		&rec.TotalMiles, &rec.PerMileRevenue, &rec.DispatcherName, &rec.DriverName,
		&rec.TeamDriverName, &rec.TruckUnit, &rec.CustomerName, &rec.PickupTime,
		&rec.DeliveryTime, &rec.PickupAppointmentTime, &rec.DeliveryAppointmentTime,
		&rec.CreatedDatetime, &rec.SyncedAt,
	)
	rec.DispatcherName = formatPersonNamePtr(rec.DispatcherName)
	rec.DriverName = formatPersonNamePtr(rec.DriverName)
	rec.TeamDriverName = formatPersonNamePtr(rec.TeamDriverName)
	rec.TruckUnit = normalizeTruckUnitPtr(rec.TruckUnit)
	return rec, err
}

const selectLoadsSQL = `
SELECT
	id, load_id, driver_id, dispatcher_id, shipment_id, status,
	load_pay, total_other_pay, total_pay, total_miles, per_mile_revenue,
	dispatcher_name, driver_name, team_driver_name, truck_unit, customer_name,
	pickup_time, delivery_time, pickup_appointment_time, delivery_appointment_time,
	created_datetime, synced_at
FROM loads
`

var ErrMissingLoadRecordID = errors.New("datatruck load is missing a positive record id")

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

// resolveDriver only links source data to a pre-existing fleet driver. Imports
// must never create a driver, because an upstream spelling or name-order change
// is not reliable proof that a new person joined the fleet.
func resolveDriver(ctx context.Context, tx pgx.Tx, name string, _ *string, _ bool) (string, bool, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM drivers WHERE normalized_name=$1 ORDER BY created_at,id LIMIT 1`, normalizeName(name)).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	id, _, found, err := findCompatibleDriver(ctx, tx, name)
	return id, found, err
}

type driverNameCandidate struct {
	id       string
	fullName string
}

func findCompatibleDriver(ctx context.Context, tx pgx.Tx, name string) (string, string, bool, error) {
	rows, err := tx.Query(ctx, `SELECT id, full_name FROM drivers ORDER BY created_at, id`)
	if err != nil {
		return "", "", false, err
	}
	defer rows.Close()

	candidates := make([]driverNameCandidate, 0)
	for rows.Next() {
		var candidateID, candidateName string
		if err := rows.Scan(&candidateID, &candidateName); err != nil {
			return "", "", false, err
		}
		if relayDriverNameMatchQuality(name, candidateName) >= 90 {
			candidates = append(candidates, driverNameCandidate{id: candidateID, fullName: candidateName})
		}
	}
	if err := rows.Err(); err != nil {
		return "", "", false, err
	}
	return chooseUniqueCompatibleDriver(candidates)
}

func chooseUniqueCompatibleDriver(candidates []driverNameCandidate) (string, string, bool, error) {
	if len(candidates) != 1 {
		return "", "", false, nil
	}
	return candidates[0].id, candidates[0].fullName, true, nil
}

type truckAssignment struct {
	truckID  string
	driverID string
}

// resolveTruck only links source data to a pre-existing fleet truck. DataTruck
// units stay on the load as raw source text when no managed truck matches.
func resolveTruck(ctx context.Context, tx pgx.Tx, unitNumber string) (string, bool, error) {
	canonicalUnit := normalizeTruckUnit(unitNumber)

	var id string
	err := tx.QueryRow(ctx, `
		SELECT id FROM trucks
		WHERE upper(regexp_replace(btrim(unit_number), '[[:space:]]+', ' ', 'g')) = $1
		ORDER BY created_at, id
		LIMIT 1`, canonicalUnit).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	return "", false, nil
}

func syncTruckAssignment(ctx context.Context, tx pgx.Tx, truckID, driverID string) error {
	// Historical loads retain their identity links but cannot reconnect inactive fleet records.
	var driverActive, truckActive bool
	if err := tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1 FOR UPDATE`, driverID).Scan(&driverActive); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT active FROM trucks WHERE id=$1 FOR UPDATE`, truckID).Scan(&truckActive); err != nil {
		return err
	}
	if !driverActive || !truckActive {
		return nil
	}
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
