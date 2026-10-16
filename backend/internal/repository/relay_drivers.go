package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/phone"
	"mserp/internal/relay"
)

type RelayDriverProvider interface {
	Drivers(context.Context) ([]relay.Driver, error)
	Driver(context.Context, string) (relay.Driver, error)
	Policies(context.Context) ([]relay.DriverPolicy, error)
	Assignment(context.Context, string) (*relay.PolicyAssignment, error)
	CreateDriver(context.Context, relay.DriverInput) (relay.Driver, error)
	UpdateDriver(context.Context, string, relay.DriverInput) error
	RemoveDriver(context.Context, string) error
	AssignPolicy(context.Context, string, string) error
	EnablePolicy(context.Context, string, bool) error
	UnassignPolicy(context.Context, string) error
}

func (r *FuelRepository) ConfigureRelayDrivers(p RelayDriverProvider, environment string) {
	r.relayDrivers = p
	r.relayEnvironment = environment
}

var ErrRelayDriverConflict = errors.New("Relay driver changed or has an unresolved request. Refresh and review before trying again.")
var ErrRelayNotConfigured = errors.New("Relay driver management is not configured on the server")

type RelayDriverValidation struct{ Message string }

func (e *RelayDriverValidation) Error() string { return e.Message }

type RelayRosterEntry struct {
	Driver      relay.Driver            `json:"driver"`
	Assignment  *relay.PolicyAssignment `json:"assignment"`
	Revision    string                  `json:"revision"`
	IdentityID  string                  `json:"identityId"`
	DriverID    string                  `json:"driverId"`
	DriverName  string                  `json:"driverName"`
	Status      string                  `json:"status"`
	Warning     string                  `json:"warning"`
	Pending     bool                    `json:"pending"`
	Suggestions []RelaySuggestion       `json:"suggestions"`
}
type RelayRosterSnapshot struct {
	Items    []RelayRosterEntry   `json:"items"`
	Policies []relay.DriverPolicy `json:"policies"`
}
type RelayDriverChoice struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Truck     string `json:"truck"`
}
type RelayDriverOperation struct {
	ID            string    `json:"id"`
	Action        string    `json:"action"`
	RelayDriverID string    `json:"relayDriverId"`
	DriverID      string    `json:"driverId"`
	CreatedAt     time.Time `json:"createdAt"`
}
type RelayDriverList struct {
	RelayRosterSnapshot
	Configured  bool                   `json:"configured"`
	RefreshedAt *time.Time             `json:"refreshedAt"`
	Environment string                 `json:"environment"`
	CanLink     bool                   `json:"canLink"`
	Drivers     []RelayDriverChoice    `json:"drivers"`
	Operations  []RelayDriverOperation `json:"operations"`
}
type RelayDriverChange struct {
	RequestID     string `json:"requestId"`
	Action        string `json:"action"`
	RelayDriverID string `json:"relayDriverId"`
	DriverID      string `json:"driverId"`
	Revision      string `json:"revision"`
	PolicyID      string `json:"policyId"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	DriverPIN     string `json:"driverPin"`
	TruckNumber   string `json:"truckNumber"`
}

// Never cache or return driver PINs/custom secret values. Edits preserve them from
// a fresh server-only driver read. Policy and profile revisions omit usage counters.
func relayRosterEntry(d relay.Driver, a *relay.PolicyAssignment) RelayRosterEntry {
	fields := []relay.DriverDataField{}
	for _, field := range d.DataFields {
		if field.Name == "Truck #" {
			fields = append(fields, field)
		}
	}
	d.DataFields = fields
	revisionData := struct {
		Driver       relay.Driver
		ID, PolicyID string
		Enabled      bool
	}{Driver: d}
	if a != nil {
		revisionData.ID = a.ID
		revisionData.PolicyID = a.PolicyID
		revisionData.Enabled = a.Enabled
	}
	b, _ := json.Marshal(revisionData)
	hash := sha256.Sum256(b)
	return RelayRosterEntry{Driver: d, Assignment: a, Revision: hex.EncodeToString(hash[:]), Status: "unlinked", Suggestions: []RelaySuggestion{}}
}
func relayPolicyAllowed(p relay.DriverPolicy) bool {
	name := strings.ToLower(strings.TrimSpace(p.Name))
	return name == "ms express company - eld" || name == "ms express o/o - eld" || name == "non eld policy"
}
func relayNonELD(p relay.DriverPolicy) bool {
	return strings.EqualFold(strings.TrimSpace(p.Name), "Non ELD Policy")
}

func (r *FuelRepository) RelayDrivers(ctx context.Context) (RelayDriverList, error) {
	result := RelayDriverList{Configured: r.relayDrivers != nil, Environment: r.relayEnvironment, RelayRosterSnapshot: RelayRosterSnapshot{Items: []RelayRosterEntry{}, Policies: []relay.DriverPolicy{}}, Drivers: []RelayDriverChoice{}, Operations: []RelayDriverOperation{}}
	if !result.Configured {
		return result, nil
	}
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT snapshot,refreshed_at FROM relay_driver_rosters WHERE environment=$1`, r.relayEnvironment).Scan(&raw, &result.RefreshedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &result.RelayRosterSnapshot); err != nil {
			return result, err
		}
	}
	result.CanLink, err = systemTaskVisible(ctx, r.pool, "relay_review", false)
	if err != nil {
		return result, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text,full_name,status,coalesce(phone,''),coalesce(email,''),coalesce((SELECT t.unit_number FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id WHERE a.driver_id=d.id AND a.unassigned_at IS NULL LIMIT 1),''),coalesce(to_jsonb(d)->>'first_name',''),coalesce(to_jsonb(d)->>'last_name','') FROM drivers d ORDER BY full_name,id`)
	if err != nil {
		return result, err
	}
	candidates := []RelaySuggestion{}
	for rows.Next() {
		var d RelayDriverChoice
		if err = rows.Scan(&d.ID, &d.Name, &d.Status, &d.Phone, &d.Email, &d.Truck, &d.FirstName, &d.LastName); err != nil {
			rows.Close()
			return result, err
		}
		result.Drivers = append(result.Drivers, d)
		p, e := d.Phone, d.Email
		candidates = append(candidates, RelaySuggestion{DriverID: d.ID, Name: d.Name, Phone: &p, Email: &e, Active: d.Status == "active"})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	type link struct {
		id, driverID, name, status string
		rejected                   []string
	}
	links := map[string]link{}
	rows, err = r.pool.Query(ctx, `SELECT l.relay_driver_id,l.id::text,coalesce(d.id::text,''),coalesce(d.full_name,''),coalesce(d.status,'unlinked'),ARRAY(SELECT driver_id::text FROM relay_identity_reviews v WHERE v.identity_id=l.id AND v.action='reject') FROM relay_driver_links l LEFT JOIN drivers d ON d.id=l.driver_id WHERE l.relay_environment=$1`, r.relayEnvironment)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var id string
		var l link
		if err = rows.Scan(&id, &l.id, &l.driverID, &l.name, &l.status, &l.rejected); err != nil {
			rows.Close()
			return result, err
		}
		links[id] = l
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = r.pool.Query(ctx, `SELECT id::text,action,relay_driver_id,coalesce(driver_id::text,''),created_at FROM relay_driver_operations WHERE environment=$1 AND state='pending' ORDER BY created_at`, r.relayEnvironment)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var op RelayDriverOperation
		if err = rows.Scan(&op.ID, &op.Action, &op.RelayDriverID, &op.DriverID, &op.CreatedAt); err != nil {
			rows.Close()
			return result, err
		}
		result.Operations = append(result.Operations, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for i := range result.Items {
		e := &result.Items[i]
		l := links[e.Driver.ID]
		e.IdentityID = l.id
		e.DriverID = l.driverID
		e.DriverName = l.name
		e.Status = l.status
		if e.Status == "terminated" {
			e.Warning = "Terminated driver is still in Relay."
		} else if e.Status == "inactive" {
			e.Warning = "Inactive driver is still in Relay."
		} else if e.DriverID == "" {
			e.Warning = "Link this Relay account to an MSERP driver to check employment status."
		}
		for _, op := range result.Operations {
			if op.RelayDriverID == e.Driver.ID {
				e.Pending = true
				e.Warning = "Unconfirmed Relay change. Refresh and verify before another change."
			}
		}
		if result.CanLink && e.DriverID == "" {
			p, email := e.Driver.Phone, e.Driver.Email
			e.Suggestions = relaySuggestions(RelayIdentityTask{Name: e.Driver.FirstName + " " + e.Driver.LastName, Phone: &p, Email: &email, RejectedDriverIDs: l.rejected}, candidates)
		}
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		a, b := result.Items[i], result.Items[j]
		if (a.Status == "terminated") != (b.Status == "terminated") {
			return a.Status == "terminated"
		}
		if (a.Warning != "") != (b.Warning != "") {
			return a.Warning != ""
		}
		return strings.ToLower(a.Driver.FirstName+" "+a.Driver.LastName) < strings.ToLower(b.Driver.FirstName+" "+b.Driver.LastName)
	})
	return result, nil
}

// A session advisory lock spans network calls and short committed transactions.
// It serializes roster refresh/mutations across API processes without holding a DB transaction open.
func (r *FuelRepository) relayDriverLock(ctx context.Context) (*pgxpool.Conn, func(), error) {
	if r.relayDrivers == nil {
		return nil, nil, ErrRelayNotConfigured
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	var locked bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "relay-drivers:"+r.relayEnvironment).Scan(&locked)
	if err != nil || !locked {
		conn.Release()
		if err == nil {
			err = ErrRelayDriverConflict
		}
		return nil, nil, err
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "relay-drivers:"+r.relayEnvironment); e != nil {
			_ = conn.Conn().Close(cleanup)
		}
		conn.Release()
	}
	return conn, release, nil
}
func (r *FuelRepository) RefreshRelayDrivers(ctx context.Context) error {
	conn, done, err := r.relayDriverLock(ctx)
	if err != nil {
		return err
	}
	defer done()
	_, err = r.refreshRelayDrivers(ctx, conn)
	return err
}
func (r *FuelRepository) refreshRelayDrivers(ctx context.Context, conn *pgxpool.Conn) (RelayRosterSnapshot, error) {
	snapshot := RelayRosterSnapshot{Items: []RelayRosterEntry{}, Policies: []relay.DriverPolicy{}}
	drivers, err := r.relayDrivers.Drivers(ctx)
	if err != nil {
		return snapshot, err
	}
	policies, err := r.relayDrivers.Policies(ctx)
	if err != nil {
		return snapshot, err
	}
	for _, p := range policies {
		if relayPolicyAllowed(p) {
			snapshot.Policies = append(snapshot.Policies, p)
		}
	}
	snapshot.Items = make([]RelayRosterEntry, len(drivers))
	errs := make([]error, len(drivers))
	var wg sync.WaitGroup
	work := make(chan int)
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				a, e := r.relayDrivers.Assignment(ctx, drivers[i].ID)
				errs[i] = e
				snapshot.Items[i] = relayRosterEntry(drivers[i], a)
			}
		}()
	}
	for i := range drivers {
		work <- i
	}
	close(work)
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return snapshot, e
		}
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback(ctx)
	for _, d := range drivers {
		email, integration := d.Email, d.IntegrationID
		if _, err = ensureRelayDriver(ctx, tx, r.relayEnvironment, relay.TransactionDriver{ID: d.ID, FirstName: d.FirstName, LastName: d.LastName, Phone: d.Phone, Email: &email, IntegrationID: &integration}); err != nil {
			return snapshot, err
		}
	}
	raw, _ := json.Marshal(snapshot)
	_, err = tx.Exec(ctx, `INSERT INTO relay_driver_rosters(environment,snapshot) VALUES($1,$2) ON CONFLICT(environment) DO UPDATE SET snapshot=EXCLUDED.snapshot,refreshed_at=now()`, r.relayEnvironment, raw)
	if err != nil {
		return snapshot, err
	}
	return snapshot, tx.Commit(ctx)
}

func (r *FuelRepository) ChangeRelayDriver(ctx context.Context, in RelayDriverChange, actor string) error {
	conn, done, err := r.relayDriverLock(ctx)
	if err != nil {
		return err
	}
	defer done()
	return r.changeRelayDriverLocked(ctx, conn, in, actor, "", "", true)
}

func (r *FuelRepository) changeRelayDriverLocked(ctx context.Context, conn *pgxpool.Conn, in RelayDriverChange, actor, autoDriver, terminationDate string, refresh bool) error {
	var err error
	target := in.RelayDriverID
	if in.Action == "add" {
		target = "driver:" + in.DriverID
	}
	var state, oldTarget, oldAction, oldActor string
	err = conn.QueryRow(ctx, `SELECT state,target,action,coalesce(actor_id::text,'') FROM relay_driver_operations WHERE id=$1 AND environment=$2`, in.RequestID, r.relayEnvironment).Scan(&state, &oldTarget, &oldAction, &oldActor)
	if err == nil {
		if oldTarget != target || oldAction != in.Action || oldActor != actor || state == "pending" {
			return ErrRelayDriverConflict
		}
		if state == "confirmed" {
			return nil
		}
		return &RelayDriverValidation{"Start a new request after reviewing the previous failure."}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var pending bool
	if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM relay_driver_operations WHERE environment=$1 AND state='pending' AND (target=$2 OR (relay_driver_id<>'' AND relay_driver_id=$3) OR ($4='add' AND action='add')))`, r.relayEnvironment, target, in.RelayDriverID, in.Action).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrRelayDriverConflict
	}
	var d relay.Driver
	var a *relay.PolicyAssignment
	var p relay.DriverPolicy
	if in.Action == "add" {
		var status string
		if err = conn.QueryRow(ctx, `SELECT status FROM drivers WHERE id=$1`, in.DriverID).Scan(&status); err != nil {
			return mapNotFound(err)
		}
		if status != "active" {
			return &RelayDriverValidation{"Only active MSERP drivers can be added."}
		}
		snap, e := r.refreshRelayDrivers(ctx, conn)
		if e != nil {
			return e
		}
		requested := phone.Imported(in.Phone)
		for _, entry := range snap.Items {
			upstream := phone.Imported(entry.Driver.Phone)
			var linked bool
			if e = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM relay_driver_links WHERE relay_environment=$1 AND relay_driver_id=$2 AND driver_id=$3)`, r.relayEnvironment, entry.Driver.ID, in.DriverID).Scan(&linked); e != nil {
				return e
			}
			if linked || (requested != nil && upstream != nil && *requested == *upstream) {
				return &RelayDriverValidation{"This driver or phone is already in Relay. Link the existing account and assign its policy."}
			}
		}
	} else {
		d, err = r.relayDrivers.Driver(ctx, in.RelayDriverID)
		if err != nil {
			return err
		}
		if d.ID != in.RelayDriverID {
			return ErrRelayDriverConflict
		}
		a, err = r.relayDrivers.Assignment(ctx, d.ID)
		if err != nil {
			return err
		}
		if relayRosterEntry(d, a).Revision != in.Revision {
			return ErrRelayDriverConflict
		}
	}
	if autoDriver != "" {
		var eligible bool
		err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drivers d JOIN relay_driver_links l ON l.driver_id=d.id
 WHERE d.id=$1 AND d.status='terminated' AND d.termination_date=$2::date AND l.relay_environment=$3 AND l.relay_driver_id=$4
 AND ($5='disable' OR d.termination_date+10<=(now() AT TIME ZONE 'America/New_York')::date))`, autoDriver, terminationDate, r.relayEnvironment, d.ID, in.Action).Scan(&eligible)
		if err != nil {
			return err
		}
		if !eligible {
			return ErrRelayDriverConflict
		}
		if in.Action == "disable" && (a == nil || !a.Enabled) {
			return nil
		}
	}
	if in.Action == "assign" {
		policies, e := r.relayDrivers.Policies(ctx)
		if e != nil {
			return e
		}
		for _, policy := range policies {
			if policy.ID == in.PolicyID && relayPolicyAllowed(policy) {
				p = policy
			}
		}
		if p.ID == "" {
			return &RelayDriverValidation{"Select an MS Express or Non ELD policy."}
		}
		if a != nil && a.PolicyID == p.ID {
			return &RelayDriverValidation{"This policy is already assigned. Use Enable policy to preserve usage."}
		}
	}
	if (in.Action == "enable" || in.Action == "disable" || in.Action == "unassign") && a == nil {
		return &RelayDriverValidation{"This driver has no policy assignment."}
	}
	if in.Action == "enable" && a.Enabled || in.Action == "disable" && !a.Enabled {
		return ErrRelayDriverConflict
	}
	fields := append([]relay.DriverDataField{}, d.DataFields...)
	setField := func(name, value string) {
		for i := range fields {
			if fields[i].Name == name {
				fields[i].Value = value
				return
			}
		}
		fields = append(fields, relay.DriverDataField{Name: name, Value: value})
	}
	if in.DriverPIN != "" {
		setField("Driver Pin", in.DriverPIN)
	}
	if in.TruckNumber != "" {
		setField("Truck #", in.TruckNumber)
	}
	if in.Action == "assign" && relayNonELD(p) {
		for _, name := range []string{"Driver Pin", "Truck #"} {
			found := false
			for _, f := range fields {
				if f.Name == name && strings.TrimSpace(f.Value) != "" {
					found = true
				}
			}
			if !found {
				return &RelayDriverValidation{name + " is required for Non ELD Policy."}
			}
		}
	}
	source, receiptDriver := "manual", in.DriverID
	if autoDriver != "" {
		source, receiptDriver = "termination", autoDriver
	}
	_, err = conn.Exec(ctx, `INSERT INTO relay_driver_operations(id,environment,target,action,relay_driver_id,driver_id,policy_id,actor_id,source,termination_date) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7,NULLIF($8,'')::uuid,$9,NULLIF($10,'')::date)`, in.RequestID, r.relayEnvironment, target, in.Action, in.RelayDriverID, receiptDriver, in.PolicyID, actor, source, terminationDate)
	if err != nil {
		return err
	}
	// Once sent, cancellation cannot cause a blind retry. The receipt remains pending
	// if we lose the response or cannot durably record it.
	operationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	// Persist the receipt first, then lock the local employment/link state for the
	// remote action. A concurrent rehire either wins this check or waits until the
	// action finishes; it cannot race an already-approved automatic removal.
	var terminationTx pgx.Tx
	if autoDriver != "" {
		terminationTx, err = conn.Begin(operationCtx)
		if err != nil {
			return err
		}
		defer terminationTx.Rollback(context.WithoutCancel(ctx))
		var lockedID string
		err = terminationTx.QueryRow(operationCtx, `SELECT d.id::text FROM drivers d JOIN relay_driver_links l ON l.driver_id=d.id
 WHERE d.id=$1 AND d.status='terminated' AND d.termination_date=$2::date AND l.relay_environment=$3 AND l.relay_driver_id=$4
 AND ($5='disable' OR d.termination_date+10<=(now() AT TIME ZONE 'America/New_York')::date) FOR SHARE OF d,l`, autoDriver, terminationDate, r.relayEnvironment, d.ID, in.Action).Scan(&lockedID)
		if err != nil {
			_ = terminationTx.Rollback(operationCtx)
			if errors.Is(err, pgx.ErrNoRows) {
				_, err = conn.Exec(operationCtx, `UPDATE relay_driver_operations SET state='failed',resolved_at=now() WHERE id=$1`, in.RequestID)
				if err == nil {
					return ErrRelayDriverConflict
				}
			}
			return err
		}
	}
	switch in.Action {
	case "add":
		d, err = r.relayDrivers.CreateDriver(operationCtx, relay.DriverInput{FirstName: in.FirstName, LastName: in.LastName, Phone: in.Phone, Email: in.Email, DataFields: fields})
		if err == nil {
			_, err = conn.Exec(operationCtx, `UPDATE relay_driver_operations SET relay_driver_id=$2 WHERE id=$1`, in.RequestID, d.ID)
			if err == nil {
				err = r.linkCreatedRelayDriver(operationCtx, conn, d, in.DriverID, actor)
			}
		}
	case "edit":
		err = r.relayDrivers.UpdateDriver(operationCtx, d.ID, relay.DriverInput{FirstName: in.FirstName, LastName: in.LastName, Phone: d.Phone, Email: in.Email, DataFields: fields})
	case "remove":
		err = r.relayDrivers.RemoveDriver(operationCtx, d.ID)
	case "enable", "disable":
		err = r.relayDrivers.EnablePolicy(operationCtx, d.ID, in.Action == "enable")
	case "unassign":
		err = r.relayDrivers.UnassignPolicy(operationCtx, d.ID)
	case "assign":
		if relayNonELD(p) {
			err = r.relayDrivers.UpdateDriver(operationCtx, d.ID, relay.DriverInput{FirstName: d.FirstName, LastName: d.LastName, Phone: d.Phone, Email: d.Email, DataFields: fields})
		}
		if err == nil {
			err = r.relayDrivers.AssignPolicy(operationCtx, d.ID, p.ID)
		}
	}
	if err != nil {
		var upstream *relay.APIError
		if errors.As(err, &upstream) && !upstream.Uncertain {
			_, saveErr := conn.Exec(operationCtx, `UPDATE relay_driver_operations SET state='failed',resolved_at=now() WHERE id=$1`, in.RequestID)
			if saveErr != nil {
				return saveErr
			}
			if terminationTx != nil {
				if saveErr = terminationTx.Commit(operationCtx); saveErr != nil {
					return saveErr
				}
			}
		}
		return err
	}
	if _, err = conn.Exec(operationCtx, `UPDATE relay_driver_operations SET state='confirmed',resolved_at=now() WHERE id=$1`, in.RequestID); err != nil {
		return err
	}
	if terminationTx != nil {
		if err = terminationTx.Commit(operationCtx); err != nil {
			return err
		}
	}
	if refresh {
		if _, err = r.refreshRelayDrivers(operationCtx, conn); err != nil {
			return &RelayDriverValidation{"Relay confirmed the change, but the roster could not refresh. Refresh to see its current state."}
		}
	}
	return nil
}

// A newly created account has an explicit user-selected MSERP identity. Existing
// accounts still require the assigned reviewer's normal Tasks authorization.
func (r *FuelRepository) linkCreatedRelayDriver(ctx context.Context, conn *pgxpool.Conn, d relay.Driver, driverID, actor string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	email, integration := d.Email, d.IntegrationID
	current, err := ensureRelayDriver(ctx, tx, r.relayEnvironment, relay.TransactionDriver{ID: d.ID, FirstName: d.FirstName, LastName: d.LastName, Phone: d.Phone, Email: &email, IntegrationID: &integration})
	if err != nil {
		return err
	}
	if current != nil && *current != driverID {
		return ErrRelayReviewConflict
	}
	var conflicting bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fuel_transactions WHERE relay_environment=$1 AND relay_driver_id=$2 AND driver_id IS NOT NULL AND driver_id<>$3)`, r.relayEnvironment, d.ID, driverID).Scan(&conflicting); err != nil {
		return err
	}
	if conflicting {
		return ErrRelayReviewConflict
	}
	var id string
	if err = tx.QueryRow(ctx, `UPDATE relay_driver_links SET driver_id=$3,updated_at=now() WHERE relay_environment=$1 AND relay_driver_id=$2 RETURNING id`, r.relayEnvironment, d.ID, driverID).Scan(&id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE fuel_transactions SET driver_id=$3 WHERE relay_environment=$1 AND relay_driver_id=$2 AND driver_id IS NULL`, r.relayEnvironment, d.ID, driverID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO relay_identity_reviews(identity_id,driver_id,action,reviewed_by) VALUES($1,$2,'link',$3) ON CONFLICT DO NOTHING`, id, driverID, actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Recovery requires a deliberate portal review; a timeout alone never unlocks a retry.
func (r *FuelRepository) VerifyRelayOperation(ctx context.Context, id, actor string) error {
	conn, done, err := r.relayDriverLock(ctx)
	if err != nil {
		return err
	}
	defer done()
	if _, err = r.refreshRelayDrivers(ctx, conn); err != nil {
		return err
	}
	command, err := conn.Exec(ctx, `UPDATE relay_driver_operations SET state='verified',resolved_at=now(),resolved_by=$3 WHERE id=$1 AND environment=$2 AND state='pending' AND created_at<now()-interval '2 minutes'`, id, r.relayEnvironment, actor)
	if err == nil && command.RowsAffected() != 1 {
		return ErrRelayDriverConflict
	}
	return err
}

func (r *FuelRepository) LinkRelayRosterDriver(ctx context.Context, relayID, driverID, actor string) (int64, error) {
	var identity string
	err := r.pool.QueryRow(ctx, `SELECT id::text FROM relay_driver_links WHERE relay_environment=$1 AND relay_driver_id=$2`, r.relayEnvironment, relayID).Scan(&identity)
	if err != nil {
		return 0, mapNotFound(err)
	}
	return r.ReviewRelayIdentity(ctx, identity, driverID, "link", actor)
}

type RelayBulkResult struct {
	RelayDriverID string `json:"relayDriverId"`
	Error         string `json:"error,omitempty"`
}
type RelayBulkResponse struct {
	Results []RelayBulkResult `json:"results"`
	Warning string            `json:"warning,omitempty"`
}

func (r *FuelRepository) ChangeRelayDrivers(ctx context.Context, changes []RelayDriverChange, actor string) (RelayBulkResponse, error) {
	out := RelayBulkResponse{Results: []RelayBulkResult{}}
	conn, done, err := r.relayDriverLock(ctx)
	if err != nil {
		return out, err
	}
	defer done()
	for _, in := range changes {
		result := RelayBulkResult{RelayDriverID: in.RelayDriverID}
		if err = r.changeRelayDriverLocked(ctx, conn, in, actor, "", "", false); err != nil {
			var invalid *RelayDriverValidation
			if errors.As(err, &invalid) || errors.Is(err, ErrRelayDriverConflict) {
				result.Error = err.Error()
			} else {
				result.Error = "Relay could not confirm this change. Refresh and review any unconfirmed request before retrying."
			}
		}
		out.Results = append(out.Results, result)
	}
	if _, err = r.refreshRelayDrivers(ctx, conn); err != nil {
		out.Warning = "Changes were processed, but the roster refresh failed. Refresh Relay to see the latest state."
	}
	return out, nil
}
