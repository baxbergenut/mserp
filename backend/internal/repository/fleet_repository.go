package repository

import (
	"context"
	"errors"
	"time"

	"mserp/internal/phone"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("record not found")

type FleetRepository struct {
	pool      *pgxpool.Pool
	datatruck *DataTruckRepository
}

func NewFleetRepository(pool *pgxpool.Pool) *FleetRepository {
	return &FleetRepository{pool: pool}
}

type Driver struct {
	LeavePlan            *DriverLeavePlan `json:"leavePlan,omitempty"`
	LeavePlanVersion     int              `json:"leavePlanVersion"`
	EmploymentStatus     string           `json:"employmentStatus"`
	TruckOperatingStatus string           `json:"truckOperatingStatus"`
	AvailabilityStatus   string           `json:"availabilityStatus"`
	StatusVersion        int              `json:"statusVersion"`
	TeamVersion          int              `json:"teamVersion"`
	TeamTruckID          *string          `json:"teamTruckId,omitempty"`
	TeamTruckUnit        *string          `json:"teamTruckUnit,omitempty"`
	TeamStartWeek        *string          `json:"teamStartWeek,omitempty"`
	TeamRole             string           `json:"teamRole,omitempty"`
	TeamPartnerID        *string          `json:"teamPartnerId,omitempty"`
	TeamPartnerName      *string          `json:"teamPartnerName,omitempty"`
	Status               string           `json:"status"`
	TerminationDate      *time.Time       `json:"terminationDate"`
	ID                   string           `json:"id"`
	FullName             string           `json:"fullName"`
	FirstName            *string          `json:"firstName"`
	LastName             *string          `json:"lastName"`
	EmploymentType       string           `json:"employmentType"`
	DriverType           string           `json:"driverType"`
	IsOwnerOperator      bool             `json:"isOwnerOperator"`
	PayType              string           `json:"payType"`
	PayRate              float64          `json:"payRate"`
	Phone                *string          `json:"phone"`
	Email                *string          `json:"email"`
	LicenseNumber        *string          `json:"licenseNumber"`
	LicenseState         *string          `json:"licenseState"`
	LicenseExpires       *time.Time       `json:"licenseExpires"`
	HireDate             *time.Time       `json:"hireDate"`
	DateOfBirth          *time.Time       `json:"dateOfBirth"`
	Address              *string          `json:"address"`
	City                 *string          `json:"city"`
	State                *string          `json:"state"`
	PostalCode           *string          `json:"postalCode"`
	EmergencyContact     *string          `json:"emergencyContact"`
	DispatcherID         *string          `json:"dispatcherId"`
	DispatcherName       *string          `json:"dispatcherName"`
	TruckID              *string          `json:"truckId"`
	TruckUnit            *string          `json:"truckUnit"`
	Active               bool             `json:"active"`
	Notes                *string          `json:"notes"`
	CDLFileID            *string          `json:"cdlFileId"`
	CDLFileName          *string          `json:"cdlFileName"`
	CDLFileContentType   *string          `json:"cdlFileContentType"`
	CDLFileSizeBytes     *int64           `json:"cdlFileSizeBytes"`
	CreatedAt            time.Time        `json:"createdAt"`
	UpdatedAt            time.Time        `json:"updatedAt"`
}

type DriverInput struct {
	DateOfBirth           *time.Time
	DateOfBirthSet        bool
	StatusVersion         *int
	PreserveFields        []string
	NewTruck              *TruckInput
	NewTruckOwnedByDriver bool
	IsTeamDriver          *bool
	TeamVersion           *int
	TeamMainDriverID      *string
	TeamMainVersion       *int
	Status                string
	TerminationDate       *time.Time
	AssignmentWeek        string
	ChargePauseWeek       string
	ChargeActor           string
	EscrowAmount          string
	FullName              string
	FirstName             *string
	LastName              *string
	IsOwnerOperator       bool
	PayType               string
	PayRate               float64
	Phone                 *string
	Email                 *string
	LicenseNumber         *string
	LicenseState          *string
	LicenseExpires        *time.Time
	HireDate              *time.Time
	Address               *string
	City                  *string
	State                 *string
	PostalCode            *string
	EmergencyContact      *string
	DispatcherID          *string
	TruckID               *string
	Active                bool
	Notes                 *string
	CDLFileID             *string
}

type DriverSetupDefaults struct {
	EscrowAmount string `json:"escrowAmount"`
}

func (r *FleetRepository) GetDriverSetupDefaults(ctx context.Context) (DriverSetupDefaults, error) {
	var value DriverSetupDefaults
	err := r.pool.QueryRow(ctx, `SELECT default_amount::text FROM driver_escrow_settings WHERE singleton`).Scan(&value.EscrowAmount)
	return value, err
}

type Truck struct {
	GPSReportedAt       *time.Time `json:"gpsReportedAt"`
	GPSSource           *string    `json:"gpsSource"`
	GPSAlert            string     `json:"gpsAlert"`
	StatusVersion       int        `json:"statusVersion"`
	StatusReason        string     `json:"statusReason"`
	OperatingStatus     string     `json:"operatingStatus"`
	OwnerSince          *string    `json:"ownerSince"`
	OwnershipVersion    string     `json:"ownershipVersion"`
	OwnerID             string     `json:"ownerId"`
	OwnerName           string     `json:"ownerName"`
	ID                  string     `json:"id"`
	UnitNumber          string     `json:"unitNumber"`
	VIN                 *string    `json:"vin"`
	Year                *int       `json:"year"`
	Make                *string    `json:"make"`
	Model               *string    `json:"model"`
	Color               *string    `json:"color"`
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
	StatusReason             string
	StatusVersion            *int
	StatusActor              string
	AddToDataTruck           bool
	PreserveFields           []string
	OwnershipEffectiveDate   string
	ExpectedOwnershipVersion string
	OwnerDriverID            *string
	AssignmentWeek           string
	OwnerID                  *string
	UnitNumber               string
	VIN                      *string
	Year                     *int
	Make                     *string
	Model                    *string
	Color                    *string // nil preserves an existing color; an empty string clears it.
	LicensePlate             *string
	LicenseState             *string
	IsCompanyOwned           bool
	Status                   string
	Mileage                  *int
	RegistrationExpires      *time.Time
	InsuranceExpires         *time.Time
	LastServiceDate          *time.Time
	NextServiceMiles         *int
	DriverID                 *string
	Active                   bool
	Notes                    *string
	IRPFileID                *string
}

type Dispatcher struct {
	UserAccountID       *string   `json:"userAccountId"`
	Extension           *int      `json:"extension"`
	MainUpdaterID       *string   `json:"mainUpdaterId"`
	AfterHoursUpdaterID *string   `json:"afterHoursUpdaterId"`
	ID                  string    `json:"id"`
	FullName            string    `json:"fullName"`
	Email               *string   `json:"email"`
	Phone               *string   `json:"phone"`
	Active              bool      `json:"active"`
	Notes               *string   `json:"notes"`
	DriverCount         int       `json:"driverCount"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type DispatcherInput struct {
	AccountPasswordHash string
	AccountActor        string
	PreserveFields      []string
	Extension           *int
	ExtensionSet        bool
	Updaters            *UpdaterAssignments
	AssignmentWeek      string
	FullName            string
	Email               *string
	Phone               *string
	DriverIDs           []string
	Active              bool
	Notes               *string
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
	search = phone.Search(search)
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
	if err != nil {
		return driver, mapNotFound(err)
	}
	err = r.pool.QueryRow(ctx, `SELECT coalesce(b.leave_plan,'{}'::jsonb),coalesce(b.version,0) FROM drivers d LEFT JOIN driver_board b ON b.driver_id=d.id WHERE d.id=$1`, id).Scan(&driver.LeavePlan, &driver.LeavePlanVersion)
	return driver, err
}

func (r *FleetRepository) CreateDriver(ctx context.Context, input DriverInput) (Driver, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Driver{}, err
	}
	defer tx.Rollback(ctx)
	if err = setBoardActor(ctx, tx, input.ChargeActor, "profile"); err != nil {
		return Driver{}, err
	}
	if input.IsTeamDriver != nil && *input.IsTeamDriver {
		if err = lockDriverTeamSetup(ctx, tx); err != nil {
			return Driver{}, err
		}
	}
	if err = setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
		return Driver{}, err
	}

	id, err := createDriverTx(ctx, tx, input)
	if err != nil {
		return Driver{}, err
	}
	if err = queueDriverServices(ctx, tx, id); err != nil {
		return Driver{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Driver{}, err
	}
	return r.GetDriver(ctx, id)
}

// Shared by manual entry and atomic completion of a FleetScope intake.
func createDriverTx(ctx context.Context, tx pgx.Tx, input DriverInput) (string, error) {
	if input.HireDate != nil {
		input.AssignmentWeek = input.HireDate.Format(time.DateOnly)
	}
	if input.Status != "" {
		input.Active = input.Status != "terminated"
	}
	var teamTruck *string
	if input.IsTeamDriver != nil && *input.IsTeamDriver {
		input.AssignmentWeek = driverStartWeek(input)
		if err := lockDriverTeamSetup(ctx, tx); err != nil {
			return "", err
		}
		teamTruck = input.TruckID
		if err := applyDriverTeamSetup(ctx, tx, "", &input); err != nil {
			return "", err
		}
		input.EscrowAmount = "0"
	}
	if !input.Active {
		input.TruckID, input.DispatcherID = nil, nil
	}
	if input.AssignmentWeek != "" {
		if err := setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
			return "", err
		}
	}
	var id string
	displayName := formatPersonName(input.FullName)
	err := tx.QueryRow(ctx, `
		INSERT INTO drivers (
			full_name, normalized_name, is_owner_operator, pay_type, pay_rate,
			phone, email, license_number, license_state, license_expires, hire_date,
			address, city, state, postal_code, emergency_contact, dispatcher_id,
			active, notes, cdl_file_id, status, termination_date, first_name, last_name, date_of_birth
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, coalesce(nullif($21,''),'active'), $22, $23, $24, $25
		) RETURNING id`,
		displayName, normalizeName(displayName), false, input.PayType, input.PayRate,
		input.Phone, input.Email, input.LicenseNumber, input.LicenseState, input.LicenseExpires, input.HireDate,
		input.Address, input.City, input.State, input.PostalCode, input.EmergencyContact, input.DispatcherID,
		input.Active, input.Notes, input.CDLFileID, input.Status, input.TerminationDate, input.FirstName, input.LastName, input.DateOfBirth,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	if input.NewTruck != nil {
		if !input.Active || input.TruckID != nil || teamTruck != nil || (input.IsTeamDriver != nil && *input.IsTeamDriver) {
			return "", chargeInvalid("New trucks require an active solo driver without an existing truck selection")
		}
		truck := *input.NewTruck
		truck.DriverID = nil
		truck.Active = true
		truck.Status = "available"
		if input.NewTruckOwnedByDriver {
			var owner string
			if err = tx.QueryRow(ctx, `INSERT INTO investors(full_name,driver_id,email,phone) SELECT full_name,id,email,phone FROM drivers WHERE id=$1 RETURNING id`, id).Scan(&owner); err != nil {
				return "", err
			}
			truck.OwnerID = &owner
			truck.IsCompanyOwned = false
		}
		truckID, createErr := createTruckTx(ctx, tx, truck)
		if createErr != nil {
			return "", createErr
		}
		input.TruckID = &truckID
	}
	if err = setDriverTruck(ctx, tx, id, input.TruckID); err != nil {
		return "", err
	}
	if err = createDriverEscrowTx(ctx, tx, id, input.EscrowAmount, input.ChargeActor); err != nil {
		return "", err
	}
	if teamTruck != nil {
		input.TruckID = teamTruck
		if err = applyDriverTeamSetup(ctx, tx, id, &input); err != nil {
			return "", err
		}
	}
	return id, nil
}

func createDriverEscrowTx(ctx context.Context, tx pgx.Tx, driverID, amount, actor string) error {
	_, err := tx.Exec(ctx, `
        INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount,created_by)
        SELECT d.id,d.full_name,coalesce(d.hire_date,(now() AT TIME ZONE 'America/New_York')::date),
               coalesce(nullif($2,'')::numeric,s.default_amount),nullif($3,'')::uuid
        FROM drivers d CROSS JOIN driver_escrow_settings s WHERE d.id=$1`, driverID, amount, actor)
	return err
}

func (r *FleetRepository) UpdateDriver(ctx context.Context, id string, input DriverInput) (Driver, error) {
	if input.NewTruck != nil || input.NewTruckOwnedByDriver {
		return Driver{}, chargeInvalid("New trucks can only be created during driver setup")
	}
	if input.Status != "" {
		input.Active = input.Status != "terminated"
	}
	if !input.Active {
		input.TruckID, input.DispatcherID = nil, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Driver{}, err
	}
	defer tx.Rollback(ctx)
	if input.IsTeamDriver != nil {
		if err = lockDriverTeamSetup(ctx, tx); err != nil {
			return Driver{}, err
		}
	}
	if err = setBoardActor(ctx, tx, input.ChargeActor, "profile"); err != nil {
		return Driver{}, err
	}
	var oldStatus string
	var statusVersion int
	if err = tx.QueryRow(ctx, `SELECT status,status_version FROM drivers WHERE id=$1 FOR UPDATE`, id).Scan(&oldStatus, &statusVersion); err != nil {
		return Driver{}, mapNotFound(err)
	}
	if input.ChargeActor != "" && input.Status != "" && input.Status != oldStatus && (input.StatusVersion == nil || *input.StatusVersion != statusVersion) {
		return Driver{}, ErrDriverBoardConflict
	}
	if err = setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
		return Driver{}, err
	}

	if err = lockChargeDrivers(ctx, tx, []string{id}); err != nil {
		return Driver{}, err
	}
	if err = syncDriverHireStart(ctx, tx, id, input.HireDate, input.ChargeActor); err != nil {
		return Driver{}, err
	}
	var wasActive bool
	if err = tx.QueryRow(ctx, "SELECT active FROM drivers WHERE id=$1", id).Scan(&wasActive); err != nil {
		return Driver{}, err
	}
	if wasActive && !input.Active {
		if err = pauseDriverCharges(ctx, tx, id, input.ChargePauseWeek, input.ChargeActor); err != nil {
			return Driver{}, err
		}
	}
	if err = applyDriverTeamSetup(ctx, tx, id, &input); err != nil {
		return Driver{}, err
	}
	displayName := formatPersonName(input.FullName)
	command, err := tx.Exec(ctx, `
		UPDATE drivers SET
			full_name = $2, normalized_name = $3, is_owner_operator = $4,
			pay_type = CASE WHEN 'payType'=ANY($26::text[]) THEN pay_type ELSE $5 END, pay_rate = CASE WHEN 'payRate'=ANY($26::text[]) THEN pay_rate ELSE $6 END, phone = $7, email = $8,
			license_number = CASE WHEN 'licenseNumber'=ANY($26::text[]) THEN license_number ELSE $9 END, license_state = CASE WHEN 'licenseState'=ANY($26::text[]) THEN license_state ELSE $10 END, license_expires = CASE WHEN 'licenseExpires'=ANY($26::text[]) THEN license_expires ELSE $11 END,
			hire_date = $12, address = CASE WHEN 'address'=ANY($26::text[]) THEN address ELSE $13 END, city = CASE WHEN 'city'=ANY($26::text[]) THEN city ELSE $14 END, state = CASE WHEN 'state'=ANY($26::text[]) THEN state ELSE $15 END,
			postal_code = CASE WHEN 'postalCode'=ANY($26::text[]) THEN postal_code ELSE $16 END, emergency_contact = CASE WHEN 'emergencyContact'=ANY($26::text[]) THEN emergency_contact ELSE $17 END, dispatcher_id = $18,
			active = $19, notes = $20, cdl_file_id = CASE WHEN 'cdlFileId'=ANY($26::text[]) THEN cdl_file_id ELSE $21 END, updated_at = now(),
			status = coalesce(nullif($22,''),status), termination_date = coalesce($23,termination_date),
			first_name = coalesce($24,first_name), last_name = coalesce($25,last_name),
			date_of_birth = CASE WHEN 'dateOfBirth'=ANY($26::text[]) OR NOT $28::boolean THEN date_of_birth ELSE $27 END
		WHERE id = $1`,
		id, displayName, normalizeName(displayName), false,
		input.PayType, input.PayRate, input.Phone, input.Email, input.LicenseNumber,
		input.LicenseState, input.LicenseExpires, input.HireDate, input.Address,
		input.City, input.State, input.PostalCode, input.EmergencyContact,
		input.DispatcherID, input.Active, input.Notes, input.CDLFileID, input.Status, input.TerminationDate, input.FirstName, input.LastName, input.PreserveFields, input.DateOfBirth, input.DateOfBirthSet,
	)
	if err != nil {
		return Driver{}, err
	}
	if command.RowsAffected() == 0 {
		return Driver{}, ErrNotFound
	}
	if !input.Active {
		err = offboardDriverAssignments(ctx, tx, id, input.ChargeActor, "profile")
	} else {
		err = setDriverTruck(ctx, tx, id, input.TruckID)
	}
	if err != nil {
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

	if err = lockChargeDrivers(ctx, tx, []string{id}); err != nil {
		return err
	}
	var charges bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM driver_charge_schedules WHERE driver_id=$1)", id).Scan(&charges); err != nil {
		return err
	}
	if charges {
		return chargeInvalid("This driver has charge history; deactivate the driver instead of deleting")
	}
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
SELECT d.id, d.full_name, d.first_name, d.last_name, (driver_employment_type(d.id)='owner_operator'), d.pay_type, d.pay_rate,
	coalesce(NOT i.is_company,false), coalesce(i.driver_id=d.id,false),
	d.phone, d.email, d.license_number, d.license_state, d.license_expires,
	d.hire_date, d.address, d.city, d.state, d.postal_code, d.emergency_contact,
	CASE WHEN d.active AND team.role='team' THEN team.main_dispatcher_id ELSE d.dispatcher_id END, dp.full_name, a.truck_id, t.unit_number,
	d.active, d.notes, d.cdl_file_id, f.file_name, f.content_type, f.size_bytes,
	d.created_at, d.updated_at, d.status, d.termination_date,
	driver_employment_type(d.id),coalesce(team.role,''),team.partner_id,team.partner_name,d.team_version,CASE WHEN d.active THEN team.truck_id END,CASE WHEN d.active THEN team.truck_unit END,team.start_week,
 CASE WHEN d.active THEN 'active' ELSE 'terminated' END,d.availability_status,d.status_version,coalesce(fleet_truck_operating_status(CASE WHEN d.active THEN fleet_status_driver_truck(d.id) ELSE a.truck_id END),''),d.date_of_birth
` + driverProfileJoinsSQL

const driverProfileJoinsSQL = `
FROM drivers d
LEFT JOIN LATERAL (SELECT CASE WHEN dt.main_driver_id=d.id THEN 'main' ELSE 'team' END role,
 p.id::text partner_id,p.full_name partner_name,m.dispatcher_id main_dispatcher_id,dt.start_week::text start_week,
 CASE WHEN dt.team_driver_id=d.id THEN shared.truck_id::text END truck_id,
 CASE WHEN dt.team_driver_id=d.id THEN vehicle.unit_number END truck_unit
 FROM driver_teams dt JOIN drivers m ON m.id=dt.main_driver_id
 JOIN drivers p ON p.id=CASE WHEN dt.main_driver_id=d.id THEN dt.team_driver_id ELSE dt.main_driver_id END
 LEFT JOIN truck_driver_assignments shared ON shared.driver_id=m.id AND shared.unassigned_at IS NULL
 LEFT JOIN trucks vehicle ON vehicle.id=shared.truck_id
 WHERE (dt.main_driver_id=d.id OR dt.team_driver_id=d.id)
 AND dt.start_week<coalesce(dt.end_week,'infinity'::date)
 AND (dt.end_week IS NULL OR (now() AT TIME ZONE 'America/New_York')::date<dt.end_week)
 ORDER BY (dt.start_week<=(now() AT TIME ZONE 'America/New_York')::date) DESC,dt.start_week,dt.id LIMIT 1) team ON true
LEFT JOIN dispatchers dp ON dp.id=CASE WHEN d.active AND team.role='team' THEN team.main_dispatcher_id ELSE d.dispatcher_id END
LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
LEFT JOIN trucks t ON t.id=a.truck_id
LEFT JOIN investors i ON i.id=t.owner_id
LEFT JOIN files f ON f.id=d.cdl_file_id`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDriver(row rowScanner) (Driver, error) {
	var value Driver
	var investorTruck, ownTruck bool
	err := row.Scan(
		&value.ID, &value.FullName, &value.FirstName, &value.LastName, &value.IsOwnerOperator, &value.PayType, &value.PayRate,
		&investorTruck, &ownTruck,
		&value.Phone, &value.Email, &value.LicenseNumber, &value.LicenseState, &value.LicenseExpires,
		&value.HireDate, &value.Address, &value.City, &value.State, &value.PostalCode,
		&value.EmergencyContact, &value.DispatcherID, &value.DispatcherName,
		&value.TruckID, &value.TruckUnit, &value.Active, &value.Notes,
		&value.CDLFileID, &value.CDLFileName, &value.CDLFileContentType,
		&value.CDLFileSizeBytes,
		&value.CreatedAt, &value.UpdatedAt, &value.Status, &value.TerminationDate,
		&value.EmploymentType, &value.TeamRole, &value.TeamPartnerID, &value.TeamPartnerName,
		&value.TeamVersion, &value.TeamTruckID, &value.TeamTruckUnit,
		&value.TeamStartWeek,
		&value.EmploymentStatus, &value.AvailabilityStatus, &value.StatusVersion, &value.TruckOperatingStatus,
		&value.DateOfBirth,
	)
	value.DriverType = driverBoardType(value.PayType, value.IsOwnerOperator, investorTruck, ownTruck)
	if value.FirstName == nil {
		value.FullName = formatPersonName(value.FullName)
	}
	value.DispatcherName = formatPersonNamePtr(value.DispatcherName)
	value.TruckUnit = normalizeTruckUnitPtr(value.TruckUnit)
	return value, err
}

func (r *FleetRepository) ListTrucks(ctx context.Context, filters ...TruckFilters) ([]Truck, error) {
	filter := TruckFilters{}
	if len(filters) > 0 {
		filter = filters[0]
	}
	rows, err := r.pool.Query(ctx, selectTrucksSQL+" WHERE ($1='' OR fleet_read.operating_status=$1) AND ($2='' OR gps_health.alert=$2) ORDER BY t.unit_number, t.id", filter.Status, filter.GPSAlert)
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

type TruckPage struct {
	Page[Truck]
	StatusCounts   map[string]int `json:"statusCounts"`
	GPSAlertCounts map[string]int `json:"gpsAlertCounts"`
}

func (r *FleetRepository) ListTrucksPage(
	ctx context.Context,
	pagination Pagination,
	search string,
	filters ...TruckFilters,
) (TruckPage, error) {
	filter := TruckFilters{}
	if len(filters) > 0 {
		filter = filters[0]
	}
	const searchWhere = `
WHERE ($1 = '' OR concat_ws(' ', t.unit_number, t.vin, t.make, t.model,
	t.license_plate, d.full_name) ILIKE '%' || $1 || '%')`
	const where = searchWhere + ` AND ($2='' OR fleet_read.operating_status=$2) AND ($3='' OR gps_health.alert=$3)`
	counts := map[string]int{"ready": 0, "shop": 0, "in_use": 0, "out_of_service": 0, "terminated": 0}
	gpsCounts := map[string]int{"overdue": 0, "missing": 0}
	countRows, err := r.pool.Query(ctx, `
		SELECT fleet_read.operating_status, gps_health.alert, count(*)
		FROM trucks t
		LEFT JOIN truck_driver_assignments a ON a.truck_id = t.id AND a.unassigned_at IS NULL
		LEFT JOIN drivers d ON d.id = a.driver_id`+truckGPSJoinSQL+searchWhere+` GROUP BY 1,2`, search)
	if err != nil {
		return TruckPage{}, err
	}
	total := 0
	for countRows.Next() {
		var code, alert string
		var count int
		if err = countRows.Scan(&code, &alert, &count); err != nil {
			countRows.Close()
			return TruckPage{}, err
		}
		counts[code] += count
		if alert != "" {
			gpsCounts[alert] += count
		}
		if (filter.Status == "" || code == filter.Status) && (filter.GPSAlert == "" || alert == filter.GPSAlert) {
			total += count
		}
	}
	err = countRows.Err()
	countRows.Close()
	if err != nil {
		return TruckPage{}, err
	}
	pagination = pagination.Normalize(total)
	rows, err := r.pool.Query(ctx, selectTrucksSQL+where+`
		ORDER BY CASE fleet_read.operating_status WHEN 'ready' THEN 0 WHEN 'shop' THEN 1 WHEN 'in_use' THEN 2 WHEN 'out_of_service' THEN 3 ELSE 4 END,t.unit_number,t.id LIMIT $4 OFFSET $5`,
		search, filter.Status, filter.GPSAlert, pagination.PageSize, pagination.Offset())
	if err != nil {
		return TruckPage{}, err
	}
	defer rows.Close()
	trucks := make([]Truck, 0, pagination.PageSize)
	for rows.Next() {
		truck, scanErr := scanTruck(rows)
		if scanErr != nil {
			return TruckPage{}, scanErr
		}
		trucks = append(trucks, truck)
	}
	if err := rows.Err(); err != nil {
		return TruckPage{}, err
	}
	return TruckPage{Page: NewPage(trucks, total, pagination), StatusCounts: counts, GPSAlertCounts: gpsCounts}, nil
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
	if err = setFleetStatusContext(ctx, tx, input.StatusActor, "profile", time.Time{}, input.StatusReason); err != nil {
		return Truck{}, err
	}
	if err = setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
		return Truck{}, err
	}

	id, err := createTruckTx(ctx, tx, input)
	if err != nil {
		return Truck{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Truck{}, err
	}
	return r.GetTruck(ctx, id)
}

func createTruckTx(ctx context.Context, tx pgx.Tx, input TruckInput) (string, error) {
	if _, err := tx.Exec(ctx, `SELECT set_config('mserp.status_reason',$1,true)`, input.StatusReason); err != nil {
		return "", err
	}
	if err := resolveTruckDriverOwner(ctx, tx, &input); err != nil {
		return "", err
	}
	if input.OwnerID != nil {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT active FROM investors WHERE id=$1 FOR SHARE`, *input.OwnerID).Scan(&active); err != nil {
			return "", mapNotFound(err)
		}
		if !active {
			return "", ErrInactiveOwner
		}
	}
	var id string
	unitNumber := normalizeTruckUnit(input.UnitNumber)
	err := tx.QueryRow(ctx, `
		INSERT INTO trucks (
			unit_number, vin, year, make, model, license_plate, license_state,
			is_company_owned, status, mileage, registration_expires,
			insurance_expires, last_service_date, next_service_miles, active, notes,
			irp_file_id, owner_id, color
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, coalesce($18::uuid,'00000000-0000-0000-0000-000000000001'), nullif($19,''))
		RETURNING id`,
		unitNumber, input.VIN, input.Year, input.Make, input.Model,
		input.LicensePlate, input.LicenseState, input.IsCompanyOwned, input.Status,
		input.Mileage, input.RegistrationExpires, input.InsuranceExpires,
		input.LastServiceDate, input.NextServiceMiles, input.Active, input.Notes,
		input.IRPFileID, input.OwnerID, input.Color,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	if input.OwnershipEffectiveDate != "" {
		if err = setDatedTruckOwner(ctx, tx, id, input); err != nil {
			return "", err
		}
	}
	if err = setTruckDriver(ctx, tx, id, input.DriverID); err != nil {
		return "", err
	}
	if err = queueDataTruckTruck(ctx, tx, id, input.AddToDataTruck); err != nil {
		return "", err
	}
	return id, nil
}

func (r *FleetRepository) UpdateTruck(ctx context.Context, id string, input TruckInput) (Truck, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Truck{}, err
	}
	defer tx.Rollback(ctx)
	if err = setFleetStatusContext(ctx, tx, input.StatusActor, "profile", time.Time{}, input.StatusReason); err != nil {
		return Truck{}, err
	}
	var oldStatus string
	var statusVersion int
	if err = tx.QueryRow(ctx, `SELECT status,status_version FROM trucks WHERE id=$1 FOR UPDATE`, id).Scan(&oldStatus, &statusVersion); err != nil {
		return Truck{}, mapNotFound(err)
	}
	if input.StatusActor != "" && input.Status != oldStatus && (input.StatusVersion == nil || *input.StatusVersion != statusVersion) {
		return Truck{}, ErrDriverBoardConflict
	}
	if err = setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
		return Truck{}, err
	}

	unitNumber := normalizeTruckUnit(input.UnitNumber)
	command, err := tx.Exec(ctx, `
		UPDATE trucks SET unit_number = $2, vin = $3, year = $4, make = $5,
			model = $6, license_plate = $7, license_state = $8,
			is_company_owned = $9, status = $10, mileage = $11,
			registration_expires = $12, insurance_expires = $13,
			last_service_date = $14, next_service_miles = $15, active = $16,
			notes = $17, irp_file_id = CASE WHEN 'irpFileId'=ANY($19::text[]) THEN irp_file_id ELSE $18 END,
			color = CASE WHEN $20::text IS NULL THEN color ELSE nullif($20,'') END, updated_at = now()
		WHERE id = $1`,
		id, unitNumber, input.VIN, input.Year, input.Make, input.Model,
		input.LicensePlate, input.LicenseState, input.IsCompanyOwned, input.Status,
		input.Mileage, input.RegistrationExpires, input.InsuranceExpires,
		input.LastServiceDate, input.NextServiceMiles, input.Active, input.Notes,
		input.IRPFileID, input.PreserveFields, input.Color,
	)
	if err != nil {
		return Truck{}, err
	}
	if command.RowsAffected() == 0 {
		return Truck{}, ErrNotFound
	}
	if err = resolveTruckDriverOwner(ctx, tx, &input); err != nil {
		return Truck{}, err
	}
	if err = setDatedTruckOwner(ctx, tx, id, input); err != nil {
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
SELECT t.owner_id, coalesce(od.full_name,oi.full_name), t.id, t.unit_number, t.vin, t.year, t.make, t.model, t.color,
	t.license_plate, t.license_state, t.is_company_owned, t.status, t.mileage,
	t.registration_expires, t.insurance_expires, t.last_service_date,
	t.next_service_miles, a.driver_id, d.full_name, t.active, t.notes,
	t.irp_file_id, f.file_name, f.content_type, f.size_bytes,
	t.created_at, t.updated_at,
	(SELECT CASE WHEN h.start_known THEN (h.assigned_at AT TIME ZONE 'America/New_York')::date::text END FROM truck_ownership_history h WHERE h.truck_id=t.id AND h.unassigned_at IS NULL),
	coalesce((SELECT md5(row_to_json(h)::text) FROM truck_ownership_history h WHERE h.truck_id=t.id AND h.unassigned_at IS NULL),''),fleet_read.operating_status,t.status_version,fleet_read.reason,gps.reported_at,gps.source,gps_health.alert
FROM trucks t
LEFT JOIN truck_driver_assignments a ON a.truck_id = t.id AND a.unassigned_at IS NULL
LEFT JOIN drivers d ON d.id = a.driver_id
LEFT JOIN files f ON f.id = t.irp_file_id
JOIN investors oi ON oi.id=t.owner_id
LEFT JOIN drivers od ON od.id=oi.driver_id` + truckGPSJoinSQL

func scanTruck(row rowScanner) (Truck, error) {
	var value Truck
	err := row.Scan(
		&value.OwnerID, &value.OwnerName, &value.ID, &value.UnitNumber, &value.VIN, &value.Year, &value.Make,
		&value.Model, &value.Color, &value.LicensePlate, &value.LicenseState,
		&value.IsCompanyOwned, &value.Status, &value.Mileage,
		&value.RegistrationExpires, &value.InsuranceExpires, &value.LastServiceDate,
		&value.NextServiceMiles, &value.DriverID, &value.DriverName, &value.Active,
		&value.Notes, &value.IRPFileID, &value.IRPFileName,
		&value.IRPFileContentType, &value.IRPFileSizeBytes,
		&value.CreatedAt, &value.UpdatedAt,
		&value.OwnerSince, &value.OwnershipVersion, &value.OperatingStatus, &value.StatusVersion, &value.StatusReason,
		&value.GPSReportedAt, &value.GPSSource, &value.GPSAlert,
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
	search = phone.Search(search)
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
	if err = setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
		return Dispatcher{}, err
	}

	var accountID *string
	if input.AccountPasswordHash != "" {
		account, e := createDispatcherAccount(ctx, tx, input)
		if e != nil {
			return Dispatcher{}, e
		}
		accountID = &account
	}
	var id string
	displayName := formatPersonName(input.FullName)
	err = tx.QueryRow(ctx, `
 INSERT INTO dispatchers (
			full_name, normalized_name, email, phone, active, notes, extension,user_account_id
 ) VALUES ($1, $2, $3, $4, $5, $6, $7,$8) RETURNING id`,
		displayName, normalizeName(displayName), input.Email, input.Phone,
		input.Active, input.Notes, input.Extension, accountID,
	).Scan(&id)
	if err != nil {
		return Dispatcher{}, err
	}
	if err = setDispatcherUpdaters(ctx, tx, id, input.Updaters); err != nil {
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
	if input.AccountActor != "" {
		if err = accessLock(ctx, tx, input.AccountActor); err != nil {
			return Dispatcher{}, err
		}
		if err = accessAudit(ctx, tx, input.AccountActor, "update_dispatcher_account", id, map[string]any{"name": input.FullName, "email": input.Email, "active": input.Active}); err != nil {
			return Dispatcher{}, err
		}
	}
	if err = setAssignmentWeek(ctx, tx, input.AssignmentWeek); err != nil {
		return Dispatcher{}, err
	}

	displayName := formatPersonName(input.FullName)
	command, err := tx.Exec(ctx, `
		UPDATE dispatchers SET full_name = $2, normalized_name = $3, email = $4,
			phone = $5, active = $6, notes = $7,
 extension = CASE WHEN $8 THEN $9::int ELSE extension END,
			updated_at = now()
		WHERE id = $1`, id, displayName, normalizeName(displayName),
		input.Email, input.Phone, input.Active, input.Notes, input.ExtensionSet, input.Extension)
	if err != nil {
		return Dispatcher{}, err
	}
	if command.RowsAffected() == 0 {
		return Dispatcher{}, ErrNotFound
	}
	if err = setDispatcherUpdaters(ctx, tx, id, input.Updaters); err != nil {
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

func (r *FleetRepository) DeleteDispatcher(ctx context.Context, id string, actor ...string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if len(actor) > 0 {
		if err = accessLock(ctx, tx, actor[0]); err != nil {
			return err
		}
	}
	var user *string
	if err = tx.QueryRow(ctx, `SELECT user_account_id FROM dispatchers WHERE id=$1 FOR UPDATE`, id).Scan(&user); err != nil {
		return mapNotFound(err)
	}
	if user != nil {
		if len(actor) == 0 {
			return ErrAccessConflict
		}
		if _, err = tx.Exec(ctx, `UPDATE app_users SET active=false,version=version+1,updated_at=now() WHERE id=$1`, *user); err != nil {
			return err
		}
		if err = revokeUser(ctx, tx, *user); err != nil {
			return err
		}
		if err = accessAudit(ctx, tx, actor[0], "delete_dispatcher_account", *user, map[string]string{"dispatcherId": id}); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM dispatchers WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const selectDispatchersSQL = `
SELECT dp.id, dp.full_name, dp.email, dp.phone,
	dp.active, dp.notes, COUNT(d.id)::int, dp.created_at, dp.updated_at, dp.extension,
 (SELECT updater_id FROM dispatcher_updaters WHERE dispatcher_id=dp.id AND shift='main'),
 (SELECT updater_id FROM dispatcher_updaters WHERE dispatcher_id=dp.id AND shift='after_hours'),dp.user_account_id
FROM dispatchers dp
LEFT JOIN drivers d ON d.dispatcher_id = dp.id`

func scanDispatcher(row rowScanner) (Dispatcher, error) {
	var value Dispatcher
	err := row.Scan(&value.ID, &value.FullName, &value.Email, &value.Phone,
		&value.Active, &value.Notes, &value.DriverCount,
		&value.CreatedAt, &value.UpdatedAt, &value.Extension, &value.MainUpdaterID, &value.AfterHoursUpdaterID, &value.UserAccountID)
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
		WHERE id = ANY($2::uuid[]) AND active`, dispatcherID, driverIDs)
	return err
}

func setDriverTruck(ctx context.Context, tx pgx.Tx, driverID string, truckID *string) error {
	if truckID != nil {
		return assignTruck(ctx, tx, *truckID, driverID)
	}
	var assigned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM truck_driver_assignments WHERE driver_id=$1 AND unassigned_at IS NULL)`, driverID).Scan(&assigned); err != nil {
		return err
	}
	if !assigned {
		return nil
	}
	return releaseDriverTruck(ctx, tx, driverID)
}

func setTruckDriver(ctx context.Context, tx pgx.Tx, truckID string, driverID *string) error {
	if driverID != nil {
		return assignTruck(ctx, tx, truckID, *driverID)
	}
	// Saving an inactive truck's unrelated details must preserve its retained
	// terminated owner-driver link, even when an older form sends driverId=null.
	var retained bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks t JOIN investors o ON o.id=t.owner_id
 JOIN truck_driver_assignments a ON a.truck_id=t.id AND a.unassigned_at IS NULL AND a.driver_id=o.driver_id
 JOIN drivers d ON d.id=a.driver_id WHERE t.id=$1 AND NOT t.active AND NOT d.active)`, truckID).Scan(&retained); err != nil {
		return err
	}
	if retained {
		return nil
	}
	var assigned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM truck_driver_assignments WHERE truck_id=$1 AND unassigned_at IS NULL)`, truckID).Scan(&assigned); err != nil {
		return err
	}
	if !assigned {
		return nil
	}
	return releaseTruckDriver(ctx, tx, truckID)
}

func assignTruck(ctx context.Context, tx pgx.Tx, truckID, driverID string) error {
	var driverActive, truckActive bool
	if err := tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1 FOR UPDATE`, driverID).Scan(&driverActive); err != nil {
		return mapNotFound(err)
	}
	if err := tx.QueryRow(ctx, `SELECT active FROM trucks WHERE id=$1 FOR UPDATE`, truckID).Scan(&truckActive); err != nil {
		return mapNotFound(err)
	}
	var unchanged bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM truck_driver_assignments
 WHERE truck_id=$1 AND driver_id=$2 AND unassigned_at IS NULL)`, truckID, driverID).Scan(&unchanged); err != nil {
		return err
	}
	if unchanged {
		if driverActive {
			return handoffInvestorDriverCharges(ctx, tx, driverID, "")
		}
		return nil
	}
	if !driverActive || !truckActive {
		return chargeInvalid("Only active drivers and trucks can be assigned")
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT fleet_truck_operating_status($1)`, truckID).Scan(&status); err != nil {
		return err
	}
	if status != "ready" && status != "in_use" {
		return chargeInvalid("Return the truck to service before assigning a driver")
	}
	if err := releaseDriverTruck(ctx, tx, driverID); err != nil {
		return err
	}
	if err := releaseTruckDriver(ctx, tx, truckID); err != nil {
		return err
	}
	// An explicitly backdated new truck may precede setup/reactivation. Extend
	// the current roster to that selected week without changing legacy unknowns.
	if _, err := tx.Exec(ctx, `UPDATE drivers SET roster_start_week=nullif(current_setting('mserp.assignment_week',true),'')::date
 WHERE id=$1 AND roster_start_week>nullif(current_setting('mserp.assignment_week',true),'')::date`, driverID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO truck_driver_assignments (truck_id, driver_id,assigned_at) VALUES ($1, $2,coalesce(nullif(current_setting('mserp.assignment_week',true),'')::date::timestamp AT TIME ZONE 'America/New_York',now()))`,
		truckID, driverID); err != nil {
		return err
	}
	if err := handoffInvestorDriverCharges(ctx, tx, driverID, ""); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE trucks SET status = CASE WHEN status IN ('available','assigned') THEN 'assigned' ELSE status END, updated_at = now() WHERE id = $1`, truckID)
	return err
}

func releaseDriverTruck(ctx context.Context, tx pgx.Tx, driverID string) error {
	if err := prepareTruckAssignmentBoundary(ctx, tx, "driver_id", driverID); err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `
		UPDATE truck_driver_assignments SET unassigned_at = coalesce(nullif(current_setting('mserp.assignment_week',true),'')::date::timestamp AT TIME ZONE 'America/New_York',now())
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
	if err := prepareTruckAssignmentBoundary(ctx, tx, "truck_id", truckID); err != nil {
		return err
	}

	_, err := tx.Exec(ctx, `
		UPDATE truck_driver_assignments SET unassigned_at = coalesce(nullif(current_setting('mserp.assignment_week',true),'')::date::timestamp AT TIME ZONE 'America/New_York',now())
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
