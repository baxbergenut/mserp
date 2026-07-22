package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("record not found")

type FleetRepository struct {
	pool *pgxpool.Pool
}

func NewFleetRepository(pool *pgxpool.Pool) *FleetRepository {
	return &FleetRepository{pool: pool}
}

type Driver struct {
	ID                 string     `json:"id"`
	FullName           string     `json:"fullName"`
	IsOwnerOperator    bool       `json:"isOwnerOperator"`
	PayType            string     `json:"payType"`
	PayRate            float64    `json:"payRate"`
	Phone              *string    `json:"phone"`
	Email              *string    `json:"email"`
	LicenseNumber      *string    `json:"licenseNumber"`
	LicenseState       *string    `json:"licenseState"`
	LicenseExpires     *time.Time `json:"licenseExpires"`
	HireDate           *time.Time `json:"hireDate"`
	Address            *string    `json:"address"`
	City               *string    `json:"city"`
	State              *string    `json:"state"`
	PostalCode         *string    `json:"postalCode"`
	EmergencyContact   *string    `json:"emergencyContact"`
	DispatcherID       *string    `json:"dispatcherId"`
	DispatcherName     *string    `json:"dispatcherName"`
	TruckID            *string    `json:"truckId"`
	TruckUnit          *string    `json:"truckUnit"`
	Active             bool       `json:"active"`
	Notes              *string    `json:"notes"`
	CDLFileID          *string    `json:"cdlFileId"`
	CDLFileName        *string    `json:"cdlFileName"`
	CDLFileContentType *string    `json:"cdlFileContentType"`
	CDLFileSizeBytes   *int64     `json:"cdlFileSizeBytes"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type DriverInput struct {
	FullName         string
	IsOwnerOperator  bool
	PayType          string
	PayRate          float64
	Phone            *string
	Email            *string
	LicenseNumber    *string
	LicenseState     *string
	LicenseExpires   *time.Time
	HireDate         *time.Time
	Address          *string
	City             *string
	State            *string
	PostalCode       *string
	EmergencyContact *string
	DispatcherID     *string
	TruckID          *string
	Active           bool
	Notes            *string
	CDLFileID        *string
}

type Truck struct {
	ID                  string     `json:"id"`
	UnitNumber          string     `json:"unitNumber"`
	VIN                 *string    `json:"vin"`
	Year                *int       `json:"year"`
	Make                *string    `json:"make"`
	Model               *string    `json:"model"`
	LicensePlate        *string    `json:"licensePlate"`
	LicenseState        *string    `json:"licenseState"`
	IsCompanyOwned      bool       `json:"isCompanyOwned"`
	Status              string     `json:"status"`
	Mileage             *int       `json:"mileage"`
	RegistrationExpires *time.Time `json:"registrationExpires"`
	InsuranceExpires    *time.Time `json:"insuranceExpires"`
	LastServiceDate     *time.Time `json:"lastServiceDate"`
	NextServiceMiles    *int       `json:"nextServiceMiles"`
	DriverID            *string    `json:"driverId"`
	DriverName          *string    `json:"driverName"`
	Active              bool       `json:"active"`
	Notes               *string    `json:"notes"`
	IRPFileID           *string    `json:"irpFileId"`
	IRPFileName         *string    `json:"irpFileName"`
	IRPFileContentType  *string    `json:"irpFileContentType"`
	IRPFileSizeBytes    *int64     `json:"irpFileSizeBytes"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type TruckInput struct {
	UnitNumber          string
	VIN                 *string
	Year                *int
	Make                *string
	Model               *string
	LicensePlate        *string
	LicenseState        *string
	IsCompanyOwned      bool
	Status              string
	Mileage             *int
	RegistrationExpires *time.Time
	InsuranceExpires    *time.Time
	LastServiceDate     *time.Time
	NextServiceMiles    *int
	DriverID            *string
	Active              bool
	Notes               *string
	IRPFileID           *string
}

type Dispatcher struct {
	ID            string    `json:"id"`
	FullName      string    `json:"fullName"`
	Email         *string   `json:"email"`
	Phone         *string   `json:"phone"`
	PayPercentage *float64  `json:"payPercentage"`
	Active        bool      `json:"active"`
	Notes         *string   `json:"notes"`
	DriverCount   int       `json:"driverCount"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type DispatcherInput struct {
	FullName      string
	Email         *string
	Phone         *string
	PayPercentage *float64
	DriverIDs     []string
	Active        bool
	Notes         *string
}

func (r *FleetRepository) ListDrivers(ctx context.Context) ([]Driver, error) {
	rows, err := r.pool.Query(ctx, selectDriversSQL+" ORDER BY d.full_name, d.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	drivers := make([]Driver, 0)
	for rows.Next() {
		driver, scanErr := scanDriver(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		drivers = append(drivers, driver)
	}
	return drivers, rows.Err()
}

func (r *FleetRepository) ListDriversPage(
	ctx context.Context,
	pagination Pagination,
	search string,
	includeInactive bool,
) (Page[Driver], error) {
	const where = `
WHERE ($1 = '' OR concat_ws(' ', d.full_name, d.email, d.phone, t.unit_number,
	dp.full_name, d.license_number) ILIKE '%' || $1 || '%')
	AND ($2 OR d.active)`
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM drivers d
		LEFT JOIN dispatchers dp ON dp.id = d.dispatcher_id
		LEFT JOIN truck_driver_assignments a ON a.driver_id = d.id AND a.unassigned_at IS NULL
		LEFT JOIN trucks t ON t.id = a.truck_id`+where, search, includeInactive).Scan(&total)
	if err != nil {
		return Page[Driver]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := r.pool.Query(ctx, selectDriversSQL+where+`
		ORDER BY d.full_name, d.id LIMIT $3 OFFSET $4`,
		search, includeInactive, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[Driver]{}, err
	}
	defer rows.Close()
	drivers := make([]Driver, 0, pagination.PageSize)
	for rows.Next() {
		driver, scanErr := scanDriver(rows)
		if scanErr != nil {
			return Page[Driver]{}, scanErr
		}
		drivers = append(drivers, driver)
	}
	if err := rows.Err(); err != nil {
		return Page[Driver]{}, err
	}
	return NewPage(drivers, total, pagination), nil
}

func (r *FleetRepository) GetDriver(ctx context.Context, id string) (Driver, error) {
	driver, err := scanDriver(r.pool.QueryRow(ctx, selectDriversSQL+" WHERE d.id = $1", id))
	return driver, mapNotFound(err)
}

func (r *FleetRepository) CreateDriver(ctx context.Context, input DriverInput) (Driver, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Driver{}, err
	}
	defer tx.Rollback(ctx)

	var id string
	displayName := formatPersonName(input.FullName)
	err = tx.QueryRow(ctx, `
		INSERT INTO drivers (
			full_name, normalized_name, is_owner_operator, pay_type, pay_rate,
			phone, email, license_number, license_state, license_expires, hire_date,
			address, city, state, postal_code, emergency_contact, dispatcher_id,
			active, notes, cdl_file_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		) RETURNING id`,
		displayName, normalizeName(displayName), input.IsOwnerOperator, input.PayType, input.PayRate,
		input.Phone, input.Email, input.LicenseNumber, input.LicenseState, input.LicenseExpires, input.HireDate,
		input.Address, input.City, input.State, input.PostalCode, input.EmergencyContact, input.DispatcherID,
		input.Active, input.Notes, input.CDLFileID,
	).Scan(&id)
	if err != nil {
		return Driver{}, err
	}
	if err = setDriverTruck(ctx, tx, id, input.TruckID); err != nil {
		return Driver{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Driver{}, err
	}
	return r.GetDriver(ctx, id)
}

func (r *FleetRepository) UpdateDriver(ctx context.Context, id string, input DriverInput) (Driver, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Driver{}, err
	}
	defer tx.Rollback(ctx)

	displayName := formatPersonName(input.FullName)
	command, err := tx.Exec(ctx, `
		UPDATE drivers SET
			full_name = $2, normalized_name = $3, is_owner_operator = $4,
			pay_type = $5, pay_rate = $6, phone = $7, email = $8,
			license_number = $9, license_state = $10, license_expires = $11,
			hire_date = $12, address = $13, city = $14, state = $15,
			postal_code = $16, emergency_contact = $17, dispatcher_id = $18,
			active = $19, notes = $20, cdl_file_id = $21, updated_at = now()
		WHERE id = $1`,
		id, displayName, normalizeName(displayName), input.IsOwnerOperator,
		input.PayType, input.PayRate, input.Phone, input.Email, input.LicenseNumber,
		input.LicenseState, input.LicenseExpires, input.HireDate, input.Address,
		input.City, input.State, input.PostalCode, input.EmergencyContact,
		input.DispatcherID, input.Active, input.Notes, input.CDLFileID,
	)
	if err != nil {
		return Driver{}, err
	}
	if command.RowsAffected() == 0 {
		return Driver{}, ErrNotFound
	}
	if err = setDriverTruck(ctx, tx, id, input.TruckID); err != nil {
		return Driver{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Driver{}, err
	}
	return r.GetDriver(ctx, id)
}

func (r *FleetRepository) DeleteDriver(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err = releaseDriverTruck(ctx, tx, id); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, "DELETE FROM drivers WHERE id = $1", id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

const selectDriversSQL = `
SELECT d.id, d.full_name, d.is_owner_operator, d.pay_type, d.pay_rate,
	d.phone, d.email, d.license_number, d.license_state, d.license_expires,
	d.hire_date, d.address, d.city, d.state, d.postal_code, d.emergency_contact,
	d.dispatcher_id, dp.full_name, a.truck_id, t.unit_number,
	d.active, d.notes, d.cdl_file_id, f.file_name, f.content_type, f.size_bytes,
	d.created_at, d.updated_at
FROM drivers d
LEFT JOIN dispatchers dp ON dp.id = d.dispatcher_id
LEFT JOIN truck_driver_assignments a ON a.driver_id = d.id AND a.unassigned_at IS NULL
LEFT JOIN trucks t ON t.id = a.truck_id
LEFT JOIN files f ON f.id = d.cdl_file_id`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDriver(row rowScanner) (Driver, error) {
	var value Driver
	err := row.Scan(
		&value.ID, &value.FullName, &value.IsOwnerOperator, &value.PayType, &value.PayRate,
		&value.Phone, &value.Email, &value.LicenseNumber, &value.LicenseState, &value.LicenseExpires,
		&value.HireDate, &value.Address, &value.City, &value.State, &value.PostalCode,
		&value.EmergencyContact, &value.DispatcherID, &value.DispatcherName,
		&value.TruckID, &value.TruckUnit, &value.Active, &value.Notes,
		&value.CDLFileID, &value.CDLFileName, &value.CDLFileContentType,
		&value.CDLFileSizeBytes,
		&value.CreatedAt, &value.UpdatedAt,
	)
	value.FullName = formatPersonName(value.FullName)
	value.DispatcherName = formatPersonNamePtr(value.DispatcherName)
	value.TruckUnit = normalizeTruckUnitPtr(value.TruckUnit)
	return value, err
}

func (r *FleetRepository) ListTrucks(ctx context.Context) ([]Truck, error) {
	rows, err := r.pool.Query(ctx, selectTrucksSQL+" ORDER BY t.unit_number, t.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	trucks := make([]Truck, 0)
	for rows.Next() {
		truck, scanErr := scanTruck(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		trucks = append(trucks, truck)
	}
	return trucks, rows.Err()
}

func (r *FleetRepository) ListTrucksPage(
	ctx context.Context,
	pagination Pagination,
	search string,
) (Page[Truck], error) {
	const where = `
WHERE ($1 = '' OR concat_ws(' ', t.unit_number, t.vin, t.make, t.model,
	t.license_plate, d.full_name) ILIKE '%' || $1 || '%')`
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM trucks t
		LEFT JOIN truck_driver_assignments a ON a.truck_id = t.id AND a.unassigned_at IS NULL
		LEFT JOIN drivers d ON d.id = a.driver_id`+where, search).Scan(&total)
	if err != nil {
		return Page[Truck]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := r.pool.Query(ctx, selectTrucksSQL+where+`
		ORDER BY t.unit_number, t.id LIMIT $2 OFFSET $3`,
		search, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[Truck]{}, err
	}
	defer rows.Close()
	trucks := make([]Truck, 0, pagination.PageSize)
	for rows.Next() {
		truck, scanErr := scanTruck(rows)
		if scanErr != nil {
			return Page[Truck]{}, scanErr
		}
		trucks = append(trucks, truck)
	}
	if err := rows.Err(); err != nil {
		return Page[Truck]{}, err
	}
	return NewPage(trucks, total, pagination), nil
}

func (r *FleetRepository) GetTruck(ctx context.Context, id string) (Truck, error) {
	truck, err := scanTruck(r.pool.QueryRow(ctx, selectTrucksSQL+" WHERE t.id = $1", id))
	return truck, mapNotFound(err)
}

func (r *FleetRepository) CreateTruck(ctx context.Context, input TruckInput) (Truck, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Truck{}, err
	}
	defer tx.Rollback(ctx)

	var id string
	unitNumber := normalizeTruckUnit(input.UnitNumber)
	err = tx.QueryRow(ctx, `
		INSERT INTO trucks (
			unit_number, vin, year, make, model, license_plate, license_state,
			is_company_owned, status, mileage, registration_expires,
			insurance_expires, last_service_date, next_service_miles, active, notes,
			irp_file_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id`,
		unitNumber, input.VIN, input.Year, input.Make, input.Model,
		input.LicensePlate, input.LicenseState, input.IsCompanyOwned, input.Status,
		input.Mileage, input.RegistrationExpires, input.InsuranceExpires,
		input.LastServiceDate, input.NextServiceMiles, input.Active, input.Notes,
		input.IRPFileID,
	).Scan(&id)
	if err != nil {
		return Truck{}, err
	}
	if err = setTruckDriver(ctx, tx, id, input.DriverID); err != nil {
		return Truck{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Truck{}, err
	}
	return r.GetTruck(ctx, id)
}

func (r *FleetRepository) UpdateTruck(ctx context.Context, id string, input TruckInput) (Truck, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Truck{}, err
	}
	defer tx.Rollback(ctx)

	unitNumber := normalizeTruckUnit(input.UnitNumber)
	command, err := tx.Exec(ctx, `
		UPDATE trucks SET unit_number = $2, vin = $3, year = $4, make = $5,
			model = $6, license_plate = $7, license_state = $8,
			is_company_owned = $9, status = $10, mileage = $11,
			registration_expires = $12, insurance_expires = $13,
			last_service_date = $14, next_service_miles = $15, active = $16,
			notes = $17, irp_file_id = $18, updated_at = now()
		WHERE id = $1`,
		id, unitNumber, input.VIN, input.Year, input.Make, input.Model,
		input.LicensePlate, input.LicenseState, input.IsCompanyOwned, input.Status,
		input.Mileage, input.RegistrationExpires, input.InsuranceExpires,
		input.LastServiceDate, input.NextServiceMiles, input.Active, input.Notes,
		input.IRPFileID,
	)
	if err != nil {
		return Truck{}, err
	}
	if command.RowsAffected() == 0 {
		return Truck{}, ErrNotFound
	}
	if err = setTruckDriver(ctx, tx, id, input.DriverID); err != nil {
		return Truck{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Truck{}, err
	}
	return r.GetTruck(ctx, id)
}

func (r *FleetRepository) DeleteTruck(ctx context.Context, id string) error {
	command, err := r.pool.Exec(ctx, "DELETE FROM trucks WHERE id = $1", id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const selectTrucksSQL = `
SELECT t.id, t.unit_number, t.vin, t.year, t.make, t.model,
	t.license_plate, t.license_state, t.is_company_owned, t.status, t.mileage,
	t.registration_expires, t.insurance_expires, t.last_service_date,
	t.next_service_miles, a.driver_id, d.full_name, t.active, t.notes,
	t.irp_file_id, f.file_name, f.content_type, f.size_bytes,
	t.created_at, t.updated_at
FROM trucks t
LEFT JOIN truck_driver_assignments a ON a.truck_id = t.id AND a.unassigned_at IS NULL
LEFT JOIN drivers d ON d.id = a.driver_id
LEFT JOIN files f ON f.id = t.irp_file_id`

func scanTruck(row rowScanner) (Truck, error) {
	var value Truck
	err := row.Scan(
		&value.ID, &value.UnitNumber, &value.VIN, &value.Year, &value.Make,
		&value.Model, &value.LicensePlate, &value.LicenseState,
		&value.IsCompanyOwned, &value.Status, &value.Mileage,
		&value.RegistrationExpires, &value.InsuranceExpires, &value.LastServiceDate,
		&value.NextServiceMiles, &value.DriverID, &value.DriverName, &value.Active,
		&value.Notes, &value.IRPFileID, &value.IRPFileName,
		&value.IRPFileContentType, &value.IRPFileSizeBytes,
		&value.CreatedAt, &value.UpdatedAt,
	)
	value.UnitNumber = normalizeTruckUnit(value.UnitNumber)
	value.DriverName = formatPersonNamePtr(value.DriverName)
	return value, err
}

func (r *FleetRepository) ListDispatchers(ctx context.Context) ([]Dispatcher, error) {
	rows, err := r.pool.Query(ctx, selectDispatchersSQL+" GROUP BY dp.id ORDER BY dp.full_name, dp.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dispatchers := make([]Dispatcher, 0)
	for rows.Next() {
		dispatcher, scanErr := scanDispatcher(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		dispatchers = append(dispatchers, dispatcher)
	}
	return dispatchers, rows.Err()
}

func (r *FleetRepository) ListDispatchersPage(
	ctx context.Context,
	pagination Pagination,
	search string,
) (Page[Dispatcher], error) {
	const where = `
WHERE ($1 = '' OR concat_ws(' ', dp.full_name, dp.email, dp.phone) ILIKE '%' || $1 || '%')`
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM dispatchers dp`+where, search).Scan(&total); err != nil {
		return Page[Dispatcher]{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := r.pool.Query(ctx, selectDispatchersSQL+where+`
		GROUP BY dp.id ORDER BY dp.full_name, dp.id LIMIT $2 OFFSET $3`,
		search, pagination.PageSize, pagination.Offset())
	if err != nil {
		return Page[Dispatcher]{}, err
	}
	defer rows.Close()
	dispatchers := make([]Dispatcher, 0, pagination.PageSize)
	for rows.Next() {
		dispatcher, scanErr := scanDispatcher(rows)
		if scanErr != nil {
			return Page[Dispatcher]{}, scanErr
		}
		dispatchers = append(dispatchers, dispatcher)
	}
	if err := rows.Err(); err != nil {
		return Page[Dispatcher]{}, err
	}
	return NewPage(dispatchers, total, pagination), nil
}

func (r *FleetRepository) GetDispatcher(ctx context.Context, id string) (Dispatcher, error) {
	dispatcher, err := scanDispatcher(r.pool.QueryRow(ctx,
		selectDispatchersSQL+" WHERE dp.id = $1 GROUP BY dp.id", id))
	return dispatcher, mapNotFound(err)
}

func (r *FleetRepository) CreateDispatcher(ctx context.Context, input DispatcherInput) (Dispatcher, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Dispatcher{}, err
	}
	defer tx.Rollback(ctx)

	var id string
	displayName := formatPersonName(input.FullName)
	err = tx.QueryRow(ctx, `
		INSERT INTO dispatchers (
			full_name, normalized_name, email, phone, pay_percentage, active, notes
		) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		displayName, normalizeName(displayName), input.Email, input.Phone,
		input.PayPercentage, input.Active, input.Notes,
	).Scan(&id)
	if err != nil {
		return Dispatcher{}, err
	}
	if err = setDispatcherDrivers(ctx, tx, id, input.DriverIDs); err != nil {
		return Dispatcher{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Dispatcher{}, err
	}
	return r.GetDispatcher(ctx, id)
}

func (r *FleetRepository) UpdateDispatcher(ctx context.Context, id string, input DispatcherInput) (Dispatcher, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Dispatcher{}, err
	}
	defer tx.Rollback(ctx)

	displayName := formatPersonName(input.FullName)
	command, err := tx.Exec(ctx, `
		UPDATE dispatchers SET full_name = $2, normalized_name = $3, email = $4,
			phone = $5, pay_percentage = $6, active = $7, notes = $8,
			updated_at = now()
		WHERE id = $1`, id, displayName, normalizeName(displayName),
		input.Email, input.Phone, input.PayPercentage, input.Active, input.Notes)
	if err != nil {
		return Dispatcher{}, err
	}
	if command.RowsAffected() == 0 {
		return Dispatcher{}, ErrNotFound
	}
	if err = setDispatcherDrivers(ctx, tx, id, input.DriverIDs); err != nil {
		return Dispatcher{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Dispatcher{}, err
	}
	return r.GetDispatcher(ctx, id)
}

func (r *FleetRepository) DeleteDispatcher(ctx context.Context, id string) error {
	command, err := r.pool.Exec(ctx, "DELETE FROM dispatchers WHERE id = $1", id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const selectDispatchersSQL = `
SELECT dp.id, dp.full_name, dp.email, dp.phone, dp.pay_percentage,
	dp.active, dp.notes, COUNT(d.id)::int, dp.created_at, dp.updated_at
FROM dispatchers dp
LEFT JOIN drivers d ON d.dispatcher_id = dp.id`

func scanDispatcher(row rowScanner) (Dispatcher, error) {
	var value Dispatcher
	err := row.Scan(&value.ID, &value.FullName, &value.Email, &value.Phone,
		&value.PayPercentage, &value.Active, &value.Notes, &value.DriverCount,
		&value.CreatedAt, &value.UpdatedAt)
	value.FullName = formatPersonName(value.FullName)
	return value, err
}

func setDispatcherDrivers(ctx context.Context, tx pgx.Tx, dispatcherID string, driverIDs []string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE drivers SET dispatcher_id = NULL, updated_at = now()
		WHERE dispatcher_id = $1 AND NOT (id = ANY($2::uuid[]))`, dispatcherID, driverIDs); err != nil {
		return err
	}
	if len(driverIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE drivers SET dispatcher_id = $1, updated_at = now()
		WHERE id = ANY($2::uuid[])`, dispatcherID, driverIDs)
	return err
}

func setDriverTruck(ctx context.Context, tx pgx.Tx, driverID string, truckID *string) error {
	if err := releaseDriverTruck(ctx, tx, driverID); err != nil {
		return err
	}
	if truckID == nil {
		return nil
	}
	return assignTruck(ctx, tx, *truckID, driverID)
}

func setTruckDriver(ctx context.Context, tx pgx.Tx, truckID string, driverID *string) error {
	if err := releaseTruckDriver(ctx, tx, truckID); err != nil {
		return err
	}
	if driverID == nil {
		return nil
	}
	return assignTruck(ctx, tx, truckID, *driverID)
}

func assignTruck(ctx context.Context, tx pgx.Tx, truckID, driverID string) error {
	if err := releaseDriverTruck(ctx, tx, driverID); err != nil {
		return err
	}
	if err := releaseTruckDriver(ctx, tx, truckID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO truck_driver_assignments (truck_id, driver_id) VALUES ($1, $2)`,
		truckID, driverID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE trucks SET status = 'assigned', updated_at = now() WHERE id = $1`, truckID)
	return err
}

func releaseDriverTruck(ctx context.Context, tx pgx.Tx, driverID string) error {
	rows, err := tx.Query(ctx, `
		UPDATE truck_driver_assignments SET unassigned_at = now()
		WHERE driver_id = $1 AND unassigned_at IS NULL RETURNING truck_id`, driverID)
	if err != nil {
		return err
	}
	var truckIDs []string
	for rows.Next() {
		var truckID string
		if err = rows.Scan(&truckID); err != nil {
			rows.Close()
			return err
		}
		truckIDs = append(truckIDs, truckID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, truckID := range truckIDs {
		if _, err = tx.Exec(ctx, `
			UPDATE trucks SET status = 'available', updated_at = now()
			WHERE id = $1 AND status = 'assigned'`, truckID); err != nil {
			return err
		}
	}
	return nil
}

func releaseTruckDriver(ctx context.Context, tx pgx.Tx, truckID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE truck_driver_assignments SET unassigned_at = now()
		WHERE truck_id = $1 AND unassigned_at IS NULL`, truckID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE trucks SET status = 'available', updated_at = now()
		WHERE id = $1 AND status = 'assigned'`, truckID)
	return err
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
