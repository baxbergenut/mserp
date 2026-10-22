package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
)

type DataTruckProvider interface {
	Configured() bool
	Environment() string
	FleetList(context.Context, string, []map[string]string) ([]map[string]any, error)
	CreateFleetRecord(context.Context, string, json.RawMessage) (json.RawMessage, error)
	UpdateDriver(context.Context, int64, json.RawMessage) (json.RawMessage, error)
}
type DataTruckRepository struct {
	pool   *pgxpool.Pool
	client DataTruckProvider
	mcID   int
}

func NewDataTruckRepository(pool *pgxpool.Pool, client DataTruckProvider, mcID int) *DataTruckRepository {
	return &DataTruckRepository{pool, client, mcID}
}
func (r *DataTruckRepository) Configured() bool {
	return r != nil && r.client != nil && r.client.Configured() && r.mcID > 0
}
func (r *FleetRepository) ConfigureDataTruck(repo *DataTruckRepository) { r.datatruck = repo }
func (r *FleetRepository) DataTruckServices() *DataTruckRepository {
	if r == nil {
		return nil
	}
	return r.datatruck
}

type DataTruckEnrollment struct {
	SyncState   string `json:"syncState,omitempty"`
	SyncMessage string `json:"syncMessage,omitempty"`
	ProviderID  *int64 `json:"providerId,omitempty"`
	State       string `json:"state"`
	Message     string `json:"message"`
	Version     int    `json:"version"`
}

func (r *DataTruckRepository) Status(ctx context.Context, kind, id string) (DataTruckEnrollment, error) {
	out := DataTruckEnrollment{State: "not_requested"}
	err := r.pool.QueryRow(ctx, `SELECT state,last_error,version,provider_id FROM datatruck_enrollments WHERE entity_kind=$1 AND entity_id=$2`, kind, id).Scan(&out.State, &out.Message, &out.Version, &out.ProviderID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err == nil && kind == "drivers" {
		e := r.pool.QueryRow(ctx, `SELECT state,last_error FROM datatruck_driver_updates WHERE driver_id=$1 AND state<>'superseded' ORDER BY (state='unconfirmed') DESC,id DESC LIMIT 1`, id).Scan(&out.SyncState, &out.SyncMessage)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			err = e
		}
	}
	return out, err
}
func queueDataTruckTruck(ctx context.Context, tx pgx.Tx, id string, enabled bool) error {
	if !enabled {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO datatruck_enrollments(entity_kind,entity_id,state) VALUES('trucks',$1,'queued')`, id)
	return err
}

// Request retries only a definite rejection or a preflight failure. An uncertain
// POST has no replay path, even after a restart or a profile change.
func (r *DataTruckRepository) RetryTruck(ctx context.Context, id string, version int) error {
	tag, err := r.pool.Exec(ctx, `UPDATE datatruck_enrollments SET state='queued',version=version+1,last_error='',updated_at=now() WHERE entity_kind='trucks' AND entity_id=$1 AND state='review' AND version=$2`, id, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return &DriverServiceError{"DataTruck setup changed or requires provider review. Refresh before retrying."}
	}
	return nil
}
func (r *DataTruckRepository) ProcessTrucks(ctx context.Context) error {
	if err := r.ProcessDriverUpdates(ctx); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE datatruck_enrollments SET state=CASE WHEN sent_at IS NULL THEN 'review' ELSE 'unconfirmed' END,last_error='Setup was interrupted. Check DataTruck before retrying.',version=version+1,updated_at=now() WHERE state='running' AND updated_at<now()-interval '10 minutes'`)
	if err != nil {
		return err
	}
	rows, err := r.pool.Query(ctx, `SELECT entity_id::text FROM datatruck_enrollments WHERE entity_kind='trucks' AND state='queued' ORDER BY updated_at LIMIT 20`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err != nil {
		return err
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		call, cancel := context.WithTimeout(ctx, 4*time.Minute)
		enrollErr := r.Enroll(call, "trucks", id)
		cancel()
		if enrollErr != nil {
			return enrollErr
		}
	}
	return nil
}
func (r *DataTruckRepository) Enroll(ctx context.Context, kind, id string) error {
	// Serialize enrollment preflights across workers so two local records cannot
	// both pass a duplicate check before either create becomes visible.
	conn, lockErr := r.pool.Acquire(ctx)
	if lockErr != nil {
		return lockErr
	}
	defer conn.Release()
	if _, lockErr = conn.Exec(ctx, `SELECT pg_advisory_lock(736281949)`); lockErr != nil {
		return lockErr
	}
	defer func() {
		unlock, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(unlock, `SELECT pg_advisory_unlock(736281949)`); e != nil {
			_ = conn.Conn().Close(unlock)
		}
	}()
	tag, err := conn.Exec(ctx, `INSERT INTO datatruck_enrollments(entity_kind,entity_id,state) VALUES($1,$2,'running') ON CONFLICT(entity_kind,entity_id) DO UPDATE SET state='running',version=datatruck_enrollments.version+1,sent_at=NULL,updated_at=now() WHERE datatruck_enrollments.state IN ('queued','review')`, kind, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return &DriverServiceError{"Check the existing DataTruck enrollment before submitting again."}
	}
	// A crash after this point is held for review; no automatic POST retries.
	var payload map[string]any
	var checks [][]map[string]string
	if !r.Configured() {
		err = &DriverServiceError{"DataTruck enrollment is not configured. Ask an administrator to check the carrier and API token."}
	} else {
		payload, checks, err = r.payload(ctx, conn, kind, id)
	}
	state := "review"
	var receipt json.RawMessage
	if err == nil {
		for _, filter := range checks {
			var existing []map[string]any
			existing, err = r.client.FleetList(ctx, kind, filter)
			if err != nil {
				break
			}
			if len(existing) > 0 {
				err = &DriverServiceError{"A possible matching record already exists in DataTruck. Review it there before adding this record."}
				break
			}
		}
	}
	if err == nil {
		var body []byte
		body, err = json.Marshal(payload)
		if err == nil {
			// Commit the exact bytes and tenant before the network call.
			_, err = conn.Exec(ctx, `UPDATE datatruck_enrollments SET payload=$3,environment=$4,sent_at=now(),updated_at=now() WHERE entity_kind=$1 AND entity_id=$2 AND state='running'`, kind, id, body, r.client.Environment())
			if err == nil {
				receipt, err = r.client.CreateFleetRecord(ctx, kind, body)
				state = "unconfirmed"
				if err == nil {
					state = "done"
				} else {
					var upstream *datatruck.FleetError
					if errors.As(err, &upstream) && !upstream.Uncertain {
						state = "review"
					}
				}
			}
		}
	}
	message := ""
	if err != nil {
		message = "Could not confirm DataTruck enrollment. Check the saved profile and DataTruck before retrying."
		var validation *DriverServiceError
		var upstream *datatruck.FleetError
		if errors.As(err, &validation) || errors.As(err, &upstream) {
			message = err.Error()
		}
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	var result struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(receipt, &result)
	var providerID *int64
	if state == "done" && result.ID > 0 {
		providerID = &result.ID
	}
	var baseline []byte
	if kind == "drivers" && state == "done" {
		baseline, _ = json.Marshal(dataTruckCreatedProfile(payload))
	}
	_, saveErr := conn.Exec(finish, `UPDATE datatruck_enrollments SET state=$3,last_error=$4,receipt=$5,provider_id=$6,provider_carrier_id=NULLIF($7,0),
 profile_baseline=coalesce($8,profile_baseline),
 version=version+1,updated_at=now() WHERE entity_kind=$1 AND entity_id=$2 AND state='running'`, kind, id, state, message, receipt, providerID, r.mcID, baseline)
	if saveErr != nil {
		return saveErr
	}
	if kind == "drivers" && state == "done" && providerID != nil {
		_, saveErr = conn.Exec(finish, `INSERT INTO datatruck_driver_updates(driver_id,provider_id,environment,carrier_id,source_snapshot)
 SELECT d.id,e.provider_id,e.environment,e.provider_carrier_id,datatruck_driver_profile(d) FROM drivers d JOIN datatruck_enrollments e ON e.entity_id=d.id AND e.entity_kind='drivers'
 WHERE d.id=$1 AND datatruck_driver_profile(d) IS DISTINCT FROM e.profile_baseline
 AND NOT EXISTS(SELECT 1 FROM datatruck_driver_updates u WHERE u.driver_id=d.id AND u.state IN ('queued','running','unconfirmed'))`, id)
		if saveErr != nil {
			return saveErr
		}
	}
	return err
}
func (r *DataTruckRepository) payload(ctx context.Context, q chargeQuery, kind, id string) (map[string]any, [][]map[string]string, error) {
	invalid := func(message string) (map[string]any, [][]map[string]string, error) {
		return nil, nil, &DriverServiceError{message}
	}
	check := func(column, value string) []map[string]string {
		return []map[string]string{{"column": column, "value": value, "contains": "is"}}
	}
	if kind == "drivers" {
		var first, last, phone, email, license, licenseState, expires, status, employment, birth string
		var team bool
		err := q.QueryRow(ctx, `SELECT coalesce(first_name,''),coalesce(last_name,''),coalesce(phone,''),coalesce(email,''),coalesce(license_number,''),coalesce(license_state,''),coalesce(license_expires::text,''),status,driver_employment_type(id),payroll_driver_at(id,(now() AT TIME ZONE 'America/New_York')::date)<>id,coalesce(date_of_birth::text,'') FROM drivers WHERE id=$1`, id).Scan(&first, &last, &phone, &email, &license, &licenseState, &expires, &status, &employment, &team, &birth)
		if err != nil {
			return nil, nil, err
		}
		if status == "terminated" {
			return invalid("Terminated drivers cannot be added to DataTruck.")
		}
		if first == "" || last == "" || len(phone) != 10 {
			return invalid("Add a confirmed first name, last name and phone number to the driver profile before enrolling in DataTruck.")
		}
		if employment == "unassigned" {
			return invalid("Assign the driver's truck so their company or owner-operator classification is known before enrolling in DataTruck.")
		}
		driverType := "company_driver"
		if employment == "owner_operator" {
			driverType = "owner_operator"
		}
		data := map[string]any{"first_name": first, "last_name": last, "contact_number": "+1" + phone, "driver_type": driverType, "new_employee_status": "active", "status": "available", "mc_number": r.mcID, "is_main": !team}
		if birth != "" {
			data["brith_date"] = birth
		}
		for k, v := range map[string]string{"email": email, "driver_license": license, "driver_license_state": licenseState, "driver_license_expiration": expires} {
			if v != "" {
				data[k] = v
			}
		}
		checks := [][]map[string]string{check("contact_number", "+1"+phone), {{"column": "account__first_name", "value": first, "contains": "is"}, {"column": "account__last_name", "value": last, "contains": "is"}}}
		if email != "" {
			checks = append(checks, check("account__email", email))
		}
		return data, checks, nil
	}
	var unit, vin, make, model, plate, state, color, registration, employment string
	var year, mileage int
	var active, company, driverOwned bool
	err := q.QueryRow(ctx, `SELECT unit_number,coalesce(vin,''),coalesce(make,''),coalesce(model,''),coalesce(license_plate,''),coalesce(license_state,''),coalesce(color,''),coalesce(registration_expires::text,''),coalesce(year,0),coalesce(mileage,0),t.active,i.is_company,i.driver_id IS NOT NULL,t.status FROM trucks t JOIN investors i ON i.id=t.owner_id WHERE t.id=$1`, id).Scan(&unit, &vin, &make, &model, &plate, &state, &color, &registration, &year, &mileage, &active, &company, &driverOwned, &employment)
	if err != nil {
		return nil, nil, err
	}
	if !active {
		return invalid("Inactive trucks cannot be added to DataTruck.")
	}
	if len(vin) != 17 || make == "" || model == "" || year == 0 || plate == "" || len(state) != 2 {
		return invalid("Add VIN, make, model, year, license plate and plate state to the truck profile before enrolling in DataTruck.")
	}
	ownership := "other"
	if driverOwned {
		ownership = "owner_operator"
	}
	if company {
		ownership = "company"
	}
	data := map[string]any{"unit_number": unit, "vin": strings.ToUpper(vin), "make": make, "model": model, "year": year, "plate_number": plate, "state": strings.ToUpper(state), "ownership": ownership, "company_mc": r.mcID, "status": "available", "odometer": mileage}
	if color != "" {
		data["color"] = color
	}
	if registration != "" {
		data["reg_expiry_date"] = registration
	}
	return data, [][]map[string]string{check("vin", vin), check("unit_number", unit)}, nil
}
