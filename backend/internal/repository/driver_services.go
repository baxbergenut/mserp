package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/relay"
	"mserp/internal/weighmytruck"
)

type DriverServiceIntent struct {
	Service    string `json:"service"`
	Action     string `json:"action"`
	FollowUpOn string `json:"followUpOn"`
	Version    int    `json:"version"`
}
type driverServicesKey struct{}
type driverServiceIntentContext struct {
	Items []DriverServiceIntent
	Actor string
}

func WithDriverServiceIntents(ctx context.Context, items []DriverServiceIntent, actor string) context.Context {
	return context.WithValue(ctx, driverServicesKey{}, driverServiceIntentContext{items, actor})
}
func ValidateDriverServiceIntents(items []DriverServiceIntent) error {
	seen := map[string]bool{}
	if len(items) > 2 {
		return &DriverServiceError{"Choose each service once."}
	}
	for _, in := range items {
		if (in.Service != "relay" && in.Service != "weighmytruck") || seen[in.Service] || (in.Action != "add" && in.Action != "defer") || in.Version < 0 {
			return &DriverServiceError{"Invalid service setup choice."}
		}
		seen[in.Service] = true
		if in.Action == "defer" {
			if _, err := time.Parse(time.DateOnly, in.FollowUpOn); err != nil {
				return &DriverServiceError{"Choose a follow-up date."}
			}
		}
	}
	return nil
}

type DriverServiceError struct{ Message string }

func (e *DriverServiceError) Error() string { return e.Message }

// Save intent alongside the fleet transaction; missing vendor prerequisites never reject a driver save.
func queueDriverServices(ctx context.Context, tx pgx.Tx, driver string) error {
	intent, _ := ctx.Value(driverServicesKey{}).(driverServiceIntentContext)
	if err := ValidateDriverServiceIntents(intent.Items); err != nil {
		return err
	}
	for _, in := range intent.Items {
		state := "queued"
		var follow any
		if in.Action == "defer" {
			state = "deferred"
			follow = in.FollowUpOn
		}
		if _, err := tx.Exec(ctx, `INSERT INTO driver_service_requests(driver_id,service,state,follow_up_on,actor_id)
  VALUES($1,$2,$3,coalesce($4::date,(now() AT TIME ZONE 'America/New_York')::date),NULLIF($5,'')::uuid) ON CONFLICT(driver_id,service) DO NOTHING`, driver, in.Service, state, follow, intent.Actor); err != nil {
			return err
		}
	}
	return nil
}

type DriverServiceRepository struct {
	pool *pgxpool.Pool
	fuel *FuelRepository
	wmt  *WeighMyTruckRepository
}

func NewDriverServiceRepository(pool *pgxpool.Pool, fuel *FuelRepository, wmt *WeighMyTruckRepository) *DriverServiceRepository {
	return &DriverServiceRepository{pool, fuel, wmt}
}

type DriverServiceState struct {
	RequestState string                 `json:"requestState"`
	Service      string                 `json:"service"`
	State        string                 `json:"state"`
	Version      int                    `json:"version"`
	Message      string                 `json:"message"`
	FollowUpOn   string                 `json:"followUpOn"`
	Configured   bool                   `json:"configured"`
	Relay        *RelayRosterEntry      `json:"relay,omitempty"`
	Candidates   []RelayRosterEntry     `json:"candidates"`
	Operations   []RelayDriverOperation `json:"operations"`
	Membership   *WMTEntry              `json:"membership,omitempty"`
}
type DriverServiceSetup struct {
	CheckedAt    time.Time            `json:"checkedAt"`
	Environment  string               `json:"environment"`
	DriverID     string               `json:"driverId"`
	Name         string               `json:"name"`
	DriverStatus string               `json:"driverStatus"`
	Tomorrow     string               `json:"tomorrow"`
	Items        []DriverServiceState `json:"items"`
	Policies     []relay.DriverPolicy `json:"policies"`
	CanLinkRelay bool                 `json:"canLinkRelay"`
}

func (r *DriverServiceRepository) Status(ctx context.Context, driver string) (DriverServiceSetup, error) {
	out := DriverServiceSetup{CheckedAt: time.Now(), DriverID: driver, Items: []DriverServiceState{}, Policies: []relay.DriverPolicy{}}
	if err := r.pool.QueryRow(ctx, `SELECT full_name,status,((now() AT TIME ZONE 'America/New_York')::date+1)::text FROM drivers WHERE id=$1`, driver).Scan(&out.Name, &out.DriverStatus, &out.Tomorrow); err != nil {
		return out, mapNotFound(err)
	}
	for _, service := range []string{"relay", "weighmytruck"} {
		item := DriverServiceState{Service: service, State: "not_requested", Candidates: []RelayRosterEntry{}, Operations: []RelayDriverOperation{}}
		err := r.pool.QueryRow(ctx, `SELECT state,version,last_error,follow_up_on::text FROM driver_service_requests WHERE driver_id=$1 AND service=$2`, driver, service).Scan(&item.State, &item.Version, &item.Message, &item.FollowUpOn)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		item.RequestState = item.State
		if service == "relay" {
			roster, err := r.fuel.RelayDrivers(ctx)
			if err != nil {
				return out, err
			}
			item.Configured = roster.Configured
			out.Policies = roster.Policies
			out.Environment = roster.Environment
			out.CanLinkRelay = roster.CanLink
			for _, entry := range roster.Items {
				if entry.DriverID == driver {
					copy := entry
					if item.Relay == nil {
						item.Relay = &copy
					}
				}
				for _, suggestion := range entry.Suggestions {
					if suggestion.DriverID == driver {
						item.Candidates = append(item.Candidates, entry)
						break
					}
				}
			}
			for _, op := range roster.Operations {
				if op.DriverID == driver || item.Relay != nil && op.RelayDriverID == item.Relay.Driver.ID {
					item.Operations = append(item.Operations, op)
				}
			}
			if len(item.Operations) > 0 && item.State != "running" {
				item.State = "unconfirmed"
				item.Message = "Verify the result in Relay before another request."
			} else if item.Relay != nil {
				if item.Relay.Assignment != nil && item.Relay.Assignment.Enabled {
					item.State = "added"
					item.Message = "Account linked and policy enabled."
				} else if item.State != "running" {
					item.State = "needs_setup"
					item.Message = "Account added. Assign or enable a policy to finish fuel setup."
				}
			}
		} else {
			item.Configured = r.wmt != nil && r.wmt.Configured()
			var member WMTEntry
			err := r.pool.QueryRow(ctx, `SELECT id::text,driver_id::text,email,first_name||' '||last_name,phone,driver_code,enrolled,state,version,last_error,updated_at,
   (state='review' OR state='pending' AND updated_at<now()-interval '2 minutes') FROM weighmytruck_memberships WHERE driver_id=$1`, driver).Scan(&member.ID, &member.DriverID, &member.Email, &member.Name, &member.Phone, &member.DriverCode, &member.Enrolled, &member.State, &member.Version, &member.Warning, &member.UpdatedAt, &member.CanVerify)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return out, err
			}
			if err == nil {
				item.Membership = &member
				if member.State != "confirmed" && item.State != "running" {
					item.State = "unconfirmed"
					item.Message = member.Warning
					if item.Message == "" {
						item.Message = "Verify membership on the WeighMyTruck website before retrying."
					}
				} else if member.Enrolled && member.State == "confirmed" {
					item.State = "added"
					item.Message = "Membership confirmed in MSERP."
				}
			}
		}
		if item.State == "done" {
			item.State = "review"
			item.Message = "Previously completed. Review the current account before adding again."
		}
		if out.DriverStatus == "terminated" {
			item.State = "canceled"
			item.Message = "Terminated drivers cannot be enrolled."
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
func (r *DriverServiceRepository) Request(ctx context.Context, driver, actor string, in DriverServiceIntent) error {
	if err := ValidateDriverServiceIntents([]DriverServiceIntent{in}); err != nil {
		return err
	}
	view, err := r.Status(ctx, driver)
	if err != nil {
		return err
	}
	if view.DriverStatus == "terminated" {
		return &DriverServiceError{"Terminated drivers cannot be enrolled."}
	}
	for _, item := range view.Items {
		if item.Service == in.Service && (item.State == "running" || item.State == "queued" || in.Action == "add" && item.State == "unconfirmed") {
			return &DriverServiceError{"Review or wait for the existing request before adding again."}
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	state := "queued"
	var follow any
	if in.Action == "defer" {
		state = "deferred"
		follow = in.FollowUpOn
	}
	// A reminder never interrupts an active worker or erases an uncertain provider receipt.
	tag, err := tx.Exec(ctx, `INSERT INTO driver_service_requests(driver_id,service,state,follow_up_on,actor_id)
 SELECT $1,$2,$3,coalesce($4::date,(now() AT TIME ZONE 'America/New_York')::date),NULLIF($5,'')::uuid WHERE $6::int=0
 ON CONFLICT(driver_id,service) DO NOTHING`, driver, in.Service, state, follow, actor, in.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		tag, err = tx.Exec(ctx, `UPDATE driver_service_requests SET state=$3,follow_up_on=coalesce($4::date,(now() AT TIME ZONE 'America/New_York')::date),actor_id=NULLIF($5,'')::uuid,version=version+1,updated_at=now(),last_error=CASE WHEN $3='queued' THEN '' ELSE last_error END
  WHERE driver_id=$1 AND service=$2 AND version=$6 AND state NOT IN ('running','queued')`, driver, in.Service, state, follow, actor, in.Version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return &DriverServiceError{"Service setup changed. Refresh before trying again."}
		}
	}
	return tx.Commit(ctx)
}

// Only queued requests cause external writes. Review/deferred requests are checked
// against confirmed local membership evidence so manual fixes close the same task.
func (r *DriverServiceRepository) Process(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, `UPDATE driver_service_requests q SET state='canceled',version=version+1,updated_at=now(),last_error='Driver terminated; setup canceled.' FROM drivers d WHERE d.id=q.driver_id AND d.status='terminated' AND q.state NOT IN ('done','canceled');
 UPDATE driver_service_requests SET state='review',version=version+1,updated_at=now(),last_error='Setup was interrupted. Check the service result before retrying.' WHERE state='running' AND updated_at<now()-interval '10 minutes'`); err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx, `UPDATE driver_service_requests q SET state='done',version=version+1,last_error='',updated_at=now()
 WHERE q.state IN ('queued','review','deferred') AND ((q.service='weighmytruck' AND EXISTS(SELECT 1 FROM weighmytruck_memberships m WHERE m.driver_id=q.driver_id AND m.enrolled AND m.state='confirmed')) OR
 (q.service='relay' AND EXISTS(SELECT 1 FROM relay_driver_rosters r CROSS JOIN LATERAL jsonb_array_elements(r.snapshot->'items') entry JOIN relay_driver_links l ON l.relay_environment=r.environment AND l.relay_driver_id=entry->'driver'->>'id' WHERE l.driver_id=q.driver_id AND r.environment=$1 AND entry->'assignment'->>'enabled'='true'
 AND NOT EXISTS(SELECT 1 FROM relay_driver_operations o WHERE o.environment=r.environment AND o.state='pending' AND (o.driver_id=q.driver_id OR o.relay_driver_id=l.relay_driver_id)))))`, r.fuel.relayEnvironment); err != nil {
		return err
	}
	// Publish the existing task invalidation once when a deferred date becomes due.
	if _, err := r.pool.Exec(ctx, `UPDATE driver_service_requests SET reminder_notified_on=follow_up_on,version=version+1 WHERE state='deferred' AND follow_up_on<=(now() AT TIME ZONE 'America/New_York')::date AND reminder_notified_on IS DISTINCT FROM follow_up_on`); err != nil {
		return err
	}
	rows, err := r.pool.Query(ctx, `SELECT driver_id::text,service,coalesce(actor_id::text,'') FROM driver_service_requests WHERE state='queued' ORDER BY created_at LIMIT 20`)
	if err != nil {
		return err
	}
	type candidate struct{ driver, service, actor string }
	work := []candidate{}
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.driver, &c.service, &c.actor); err != nil {
			rows.Close()
			return err
		}
		work = append(work, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range work {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var version int
		var requestID string
		err = r.pool.QueryRow(ctx, `UPDATE driver_service_requests SET state='running',version=version+1,updated_at=now() WHERE driver_id=$1 AND service=$2 AND state='queued' RETURNING version,gen_random_uuid()::text`, c.driver, c.service).Scan(&version, &requestID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		err = r.enroll(callCtx, c.driver, c.service, c.actor, requestID)
		cancel()
		message := ""
		state := "review"
		if err != nil {
			message = serviceEnrollmentError(err)
		}
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		view, readErr := r.Status(finish, c.driver)
		if readErr == nil {
			for _, item := range view.Items {
				if item.Service == c.service {
					if item.State == "added" {
						state = "done"
						message = ""
					} else if message == "" {
						message = item.Message
						if message == "" {
							message = "Account added. Review service setup to finish."
						}
					}
				}
			}
		}
		_, saveErr := r.pool.Exec(finish, `UPDATE driver_service_requests SET state=$3,last_error=$4,version=version+1,updated_at=now() WHERE driver_id=$1 AND service=$2 AND state='running' AND version=$5`, c.driver, c.service, state, message, version)
		cancel()
		if saveErr != nil {
			return saveErr
		}
	}
	return nil
}
func serviceEnrollmentError(err error) string {
	var invalid *RelayDriverValidation
	var wmtInvalid *WMTValidationError
	var wmtError *weighmytruck.Error
	var relayError *relay.APIError
	if errors.As(err, &wmtError) || errors.As(err, &relayError) || errors.As(err, &invalid) || errors.As(err, &wmtInvalid) || errors.Is(err, ErrWMTConflict) || errors.Is(err, ErrRelayDriverConflict) || errors.Is(err, ErrRelayNotConfigured) {
		return err.Error()
	}
	// Upstream bodies can contain PII. Keep only vetted error text, never guess a carrier conflict.
	return "The service did not confirm enrollment. Review the account and service status before retrying."
}
func (r *DriverServiceRepository) enroll(ctx context.Context, driver, service, actor, requestID string) error {
	if service == "weighmytruck" {
		if r.wmt == nil {
			return &WMTValidationError{"WeighMyTruck is not configured."}
		}
		var in WMTChange
		in.DriverID = driver
		in.Add = true
		var enrolled bool
		var state string
		err := r.pool.QueryRow(ctx, `SELECT coalesce(m.id::text,''),coalesce(m.version,0),coalesce(m.enrolled,false),coalesce(m.state,'confirmed') FROM drivers d LEFT JOIN weighmytruck_memberships m ON m.driver_id=d.id WHERE d.id=$1`, driver).Scan(&in.MembershipID, &in.Version, &enrolled, &state)
		if err != nil {
			return err
		}
		if enrolled && state == "confirmed" {
			return nil
		}
		return r.wmt.Change(ctx, in, actor)
	}
	view, err := r.Status(ctx, driver)
	if err != nil {
		return err
	}
	for _, item := range view.Items {
		if item.Service == "relay" {
			if len(item.Operations) > 0 {
				return ErrRelayDriverConflict
			}
			if item.Relay != nil {
				return nil
			}
		}
	}
	var name, phone, email, first, last string
	if err = r.pool.QueryRow(ctx, `SELECT full_name,coalesce(phone,''),coalesce(email,''),coalesce(to_jsonb(d)->>'first_name',''),coalesce(to_jsonb(d)->>'last_name','') FROM drivers d WHERE id=$1 AND status='active'`, driver).Scan(&name, &phone, &email, &first, &last); err != nil {
		return err
	}
	first, last = driverProviderName(name, first, last)
	if first == "" || last == "" || len(phone) != 10 {
		return &RelayDriverValidation{"Add a first and last name and a valid phone number to the driver profile."}
	}
	return r.fuel.ChangeRelayDriver(ctx, RelayDriverChange{RequestID: requestID, Action: "add", DriverID: driver, FirstName: first, LastName: last, Phone: "+1" + phone, Email: email}, actor)
}
