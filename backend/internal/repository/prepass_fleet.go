package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/prepass"
)

type PrePassFleetProvider interface {
	Fleet(context.Context) (prepass.FleetSnapshot, error)
	ChangeVehicle(context.Context, string, string, string, prepass.VehicleInput, string) error
}

func (r *TollRepository) ConfigurePrePassFleet(p PrePassFleetProvider, environment string) {
	r.prepassFleet = p
	r.prepassEnvironment = environment
}

var ErrPrePassFleetConflict = errors.New("PrePass or the linked truck changed, or a request is unresolved. Refresh and review before trying again.")
var ErrPrePassFleetNotConfigured = errors.New("PrePass management is not configured on the server")

type PrePassFleetValidation struct{ Message string }

func (e *PrePassFleetValidation) Error() string { return e.Message }
func prepassInvalid(message string) error       { return &PrePassFleetValidation{message} }

type PrePassFleetTruckChoice struct {
	ID         string    `json:"id"`
	Unit       string    `json:"unit"`
	VIN        string    `json:"vin"`
	Plate      string    `json:"plate"`
	PlateState string    `json:"plateState"`
	Active     bool      `json:"active"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
type PrePassFleetEntry struct {
	Vehicle     prepass.Vehicle `json:"vehicle"`
	TruckID     string          `json:"truckId"`
	TruckUnit   string          `json:"truckUnit"`
	Revision    string          `json:"revision"`
	Warning     string          `json:"warning"`
	Suggestions []string        `json:"suggestions"`
	TollsLinked bool            `json:"tollsLinked"`
	TollFrom    string          `json:"tollFrom"`
}
type PrePassFleetOperation struct {
	ID            string    `json:"id"`
	AccountNumber string    `json:"accountNumber"`
	VIN           string    `json:"vin"`
	Action        string    `json:"action"`
	State         string    `json:"state"`
	Actor         string    `json:"actor"`
	CreatedAt     time.Time `json:"createdAt"`
}
type PrePassFleetList struct {
	Configured   bool                      `json:"configured"`
	Environment  string                    `json:"environment"`
	RefreshedAt  *time.Time                `json:"refreshedAt"`
	Accounts     []string                  `json:"accounts"`
	Vehicles     []PrePassFleetEntry       `json:"vehicles"`
	Transponders []prepass.Transponder     `json:"transponders"`
	Trucks       []PrePassFleetTruckChoice `json:"trucks"`
	Operations   []PrePassFleetOperation   `json:"operations"`
}
type PrePassFleetChange struct {
	PlateSource           string               `json:"plateSource,omitempty"`
	RequestID             string               `json:"requestId"`
	Action                string               `json:"action"`
	AccountNumber         string               `json:"accountNumber"`
	VIN                   string               `json:"vin"`
	Revision              string               `json:"revision"`
	TruckID               string               `json:"truckId"`
	TruckUpdatedAt        string               `json:"truckUpdatedAt"`
	Vehicle               prepass.VehicleInput `json:"vehicle"`
	TransponderNumber     string               `json:"transponderNumber"`
	FleetServiceConfirmed bool                 `json:"fleetServiceConfirmed"`
	TollFrom              string               `json:"tollFrom"`
	TollRevision          string               `json:"tollRevision"`
}

func fleetHash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func prepassKey(account, vin string) string { return account + ":" + vin }
func prepassRevision(v prepass.Vehicle, truck string) string {
	return fleetHash(struct {
		Vehicle prepass.Vehicle
		Truck   string
	}{v, truck})
}

func (r *TollRepository) PrePassFleet(ctx context.Context) (PrePassFleetList, error) {
	out := PrePassFleetList{Configured: r.prepassFleet != nil, Environment: r.prepassEnvironment, Accounts: []string{}, Vehicles: []PrePassFleetEntry{}, Transponders: []prepass.Transponder{}, Trucks: []PrePassFleetTruckChoice{}, Operations: []PrePassFleetOperation{}}
	if !out.Configured {
		return out, nil
	}
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT snapshot,refreshed_at FROM prepass_fleet_rosters WHERE environment=$1`, r.prepassEnvironment).Scan(&raw, &out.RefreshedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	snap := prepass.FleetSnapshot{}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &snap); err != nil {
			return out, err
		}
		out.Accounts = snap.Accounts
		out.Transponders = snap.Transponders
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text,unit_number,coalesce(vin,''),coalesce(license_plate,''),coalesce(license_state,''),active,updated_at FROM trucks ORDER BY unit_number,id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var t PrePassFleetTruckChoice
		if err = rows.Scan(&t.ID, &t.Unit, &t.VIN, &t.Plate, &t.PlateState, &t.Active, &t.UpdatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Trucks = append(out.Trucks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	links := map[string]string{}
	rows, err = r.pool.Query(ctx, `SELECT account_number,vin,truck_id::text FROM prepass_fleet_links WHERE environment=$1`, r.prepassEnvironment)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var a, v, t string
		if err = rows.Scan(&a, &v, &t); err != nil {
			rows.Close()
			return out, err
		}
		links[prepassKey(a, v)] = t
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, v := range snap.Vehicles {
		e := PrePassFleetEntry{Vehicle: v, TruckID: links[prepassKey(v.AccountNumber, v.VIN)], Suggestions: []string{}}
		e.Revision = prepassRevision(v, e.TruckID)
		for _, t := range out.Trucks {
			if strings.EqualFold(strings.TrimSpace(t.VIN), v.VIN) {
				e.Suggestions = append(e.Suggestions, t.ID)
			}
			if e.TruckID == t.ID {
				e.TruckUnit = t.Unit
				if !t.Active {
					e.Warning = "Inactive MSERP truck remains in PrePass"
				} else if strings.TrimSpace(t.VIN) == "" {
					e.Warning = "MSERP truck profile is missing its VIN"
				} else if !strings.EqualFold(strings.TrimSpace(t.VIN), v.VIN) {
					e.Warning = "Linked truck VIN differs from PrePass"
				} else if !strings.EqualFold(strings.TrimSpace(t.Plate), strings.TrimSpace(v.LicensePlateNumber)) || !strings.EqualFold(strings.TrimSpace(t.PlateState), strings.TrimSpace(v.LicensePlateState)) {
					e.Warning = "Plate differs from MSERP"
				}
			}
		}
		if e.TruckID == "" {
			e.Warning = "Not linked to an MSERP truck"
		}
		keys := fleetTollKeys(v)
		if e.TruckID != "" && len(keys) > 0 {
			var count int
			if err = r.pool.QueryRow(ctx, `SELECT count(DISTINCT identity_key),coalesce(min(effective_from)::text,'') FROM prepass_truck_mappings WHERE active AND effective_to IS NULL AND environment=$1 AND account_number=$2 AND fleet_vin=$3 AND truck_id=$4 AND identity_key=ANY($5::text[])`, r.prepassEnvironment, v.AccountNumber, v.VIN, e.TruckID, keys).Scan(&count, &e.TollFrom); err != nil {
				return out, err
			}
			e.TollsLinked = count == len(keys)
			if !e.TollsLinked && e.Warning == "" {
				e.Warning = "Toll linking needs a start date"
			}
		}
		out.Vehicles = append(out.Vehicles, e)
	}
	rows, err = r.pool.Query(ctx, `SELECT o.id::text,o.account_number,o.vin,o.action,o.state,coalesce(u.username,'Deleted user'),o.created_at FROM prepass_fleet_operations o LEFT JOIN app_users u ON u.id=o.actor_id WHERE o.environment=$1 ORDER BY (o.state='pending') DESC,o.created_at DESC LIMIT 50`, r.prepassEnvironment)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var o PrePassFleetOperation
		if err = rows.Scan(&o.ID, &o.AccountNumber, &o.VIN, &o.Action, &o.State, &o.Actor, &o.CreatedAt); err != nil {
			return out, err
		}
		out.Operations = append(out.Operations, o)
	}
	return out, rows.Err()
}

func (r *TollRepository) prepassLock(ctx context.Context) (*pgxpool.Conn, func(), error) {
	if r.prepassFleet == nil {
		return nil, nil, ErrPrePassFleetNotConfigured
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	key := "prepass-fleet:" + r.prepassEnvironment
	var locked bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&locked)
	if err != nil || !locked {
		conn.Release()
		if err == nil {
			err = ErrPrePassFleetConflict
		}
		return nil, nil, err
	}
	return conn, func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key); e != nil {
			_ = conn.Conn().Close(cleanup)
		}
		conn.Release()
	}, nil
}
func normalizePrePassFleet(s *prepass.FleetSnapshot) error {
	if s.Accounts == nil || s.Vehicles == nil || s.Transponders == nil {
		return fmt.Errorf("Incomplete PrePass roster")
	}
	seen := map[string]bool{}
	for i := range s.Vehicles {
		v := &s.Vehicles[i]
		v.VIN = strings.ToUpper(strings.TrimSpace(v.VIN))
		key := prepassKey(v.AccountNumber, v.VIN)
		if !slices.Contains(s.Accounts, v.AccountNumber) || !prepass.FleetVIN.MatchString(v.VIN) || seen[key] {
			return fmt.Errorf("Invalid or duplicate PrePass vehicle")
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, d := range s.Transponders {
		key := prepassKey(d.AccountNumber, d.TransponderNumber)
		if !slices.Contains(s.Accounts, d.AccountNumber) || !prepass.TransponderNumber.MatchString(d.TransponderNumber) || seen[key] {
			return fmt.Errorf("Invalid or duplicate PrePass transponder")
		}
		seen[key] = true
	}
	sort.Strings(s.Accounts)
	sort.Slice(s.Vehicles, func(i, j int) bool {
		a, b := s.Vehicles[i], s.Vehicles[j]
		return prepassKey(a.AccountNumber, a.VIN) < prepassKey(b.AccountNumber, b.VIN)
	})
	sort.Slice(s.Transponders, func(i, j int) bool {
		a, b := s.Transponders[i], s.Transponders[j]
		return prepassKey(a.AccountNumber, a.TransponderNumber) < prepassKey(b.AccountNumber, b.TransponderNumber)
	})
	return nil
}
func (r *TollRepository) fetchPrePassFleet(ctx context.Context) (prepass.FleetSnapshot, error) {
	s, e := r.prepassFleet.Fleet(ctx)
	if e == nil {
		e = normalizePrePassFleet(&s)
	}
	return s, e
}
func (r *TollRepository) savePrePassFleet(ctx context.Context, conn *pgxpool.Conn, s prepass.FleetSnapshot) error {
	b, _ := json.Marshal(s)
	_, e := conn.Exec(ctx, `INSERT INTO prepass_fleet_rosters(environment,snapshot) VALUES($1,$2) ON CONFLICT(environment) DO UPDATE SET snapshot=EXCLUDED.snapshot,refreshed_at=now()`, r.prepassEnvironment, b)
	return e
}
func (r *TollRepository) RefreshPrePassFleet(ctx context.Context) error {
	conn, done, e := r.prepassLock(ctx)
	if e != nil {
		return e
	}
	defer done()
	s, e := r.fetchPrePassFleet(ctx)
	if e != nil {
		return e
	}
	return r.savePrePassFleet(ctx, conn, s)
}
func availablePrePassDevice(s prepass.FleetSnapshot, account, number string) bool {
	found := false
	for _, d := range s.Transponders {
		if d.TransponderNumber == number {
			if d.AccountNumber != account || strings.TrimSpace(d.VIN) != "" || strings.TrimSpace(d.VehicleNumber) != "" {
				return false
			}
			found = true
		}
	}
	for _, v := range s.Vehicles {
		if v.TransponderNumber == number {
			return false
		}
	}
	return found
}
func findPrePassVehicle(s prepass.FleetSnapshot, a, v string) (prepass.Vehicle, bool) {
	for _, x := range s.Vehicles {
		if x.AccountNumber == a && x.VIN == v {
			return x, true
		}
	}
	return prepass.Vehicle{}, false
}

func (r *TollRepository) ChangePrePassFleet(ctx context.Context, in PrePassFleetChange, actor string) error {
	resolvePlate := in.PlateSource != ""
	if resolvePlate && (in.Action != "edit" || !slices.Contains([]string{"mserp", "prepass"}, in.PlateSource)) {
		return prepassInvalid("Select the MSERP or PrePass plate to use in both systems")
	}
	if in.Action == "link" {
		if err := validateFleetTollLink(in); err != nil {
			return err
		}
	}
	if !slices.Contains([]string{"add", "edit", "remove", "assign", "link"}, in.Action) {
		return prepassInvalid("Select a supported PrePass action")
	}
	if in.Action != "link" && !in.FleetServiceConfirmed {
		return prepassInvalid("Confirm this truck is not enrolled in VPP")
	}
	if in.Action == "add" || in.Action == "edit" && !resolvePlate {
		if e := in.Vehicle.Validate(); e != nil {
			return prepassInvalid(e.Error())
		}
	}
	conn, done, err := r.prepassLock(ctx)
	if err != nil {
		return err
	}
	defer done()
	hash := fleetHash(in)
	var oldHash, oldActor, state string
	err = conn.QueryRow(ctx, `SELECT request_hash,coalesce(actor_id::text,''),state FROM prepass_fleet_operations WHERE id=$1 AND environment=$2`, in.RequestID, r.prepassEnvironment).Scan(&oldHash, &oldActor, &state)
	if err == nil {
		if oldHash != hash || oldActor != actor {
			return ErrPrePassFleetConflict
		}
		if state == "confirmed" {
			return nil
		}
		return ErrPrePassFleetConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var pending bool
	if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prepass_fleet_operations WHERE environment=$1 AND state='pending')`, r.prepassEnvironment).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrPrePassFleetConflict
	}
	snap, err := r.fetchPrePassFleet(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(snap.Accounts, in.AccountNumber) {
		return prepassInvalid("Select an authorized PrePass account")
	}
	var linked string
	err = conn.QueryRow(ctx, `SELECT truck_id::text FROM prepass_fleet_links WHERE environment=$1 AND account_number=$2 AND vin=$3`, r.prepassEnvironment, in.AccountNumber, in.VIN).Scan(&linked)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	before, exists := findPrePassVehicle(snap, in.AccountNumber, in.VIN)
	if in.Action == "link" {
		if err := validateFleetIdentity(snap, before); err != nil {
			return err
		}
	}
	if in.Action == "add" {
		for _, v := range snap.Vehicles {
			if v.VIN == in.VIN {
				return prepassInvalid("This VIN is already in PrePass. Link the existing truck.")
			}
		}
	} else if !exists || prepassRevision(before, linked) != in.Revision {
		return ErrPrePassFleetConflict
	}
	if resolvePlate && (linked == "" || linked != in.TruckID) {
		return prepassInvalid("Link this PrePass vehicle to the correct MSERP truck before resolving its plate")
	}
	if in.Action == "assign" {
		if before.TransponderNumber != "" {
			return prepassInvalid("This truck already has a transponder. Replacement and transfer are not supported here yet.")
		}
		if !availablePrePassDevice(snap, in.AccountNumber, in.TransponderNumber) {
			return prepassInvalid("This transponder is no longer available in the selected account")
		}
	}
	if in.Action == "add" || in.Action == "edit" && !resolvePlate {
		for _, v := range snap.Vehicles {
			if v.AccountNumber == in.AccountNumber && v.VIN != in.VIN && strings.EqualFold(v.VehicleNumber, in.Vehicle.VehicleNumber) {
				return prepassInvalid("This PrePass unit number is already in use")
			}
		}
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if resolvePlate {
		if _, err = r.checkPrePassPlate(ctx, tx, in, before); err != nil {
			return err
		}
	}
	if in.Action == "link" {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940); SELECT pg_advisory_xact_lock(730077)`); err != nil {
			return err
		}
	}
	if in.Action == "add" || in.Action == "link" {
		if err = r.checkPrePassTruck(ctx, tx, in); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(in)
	var localPlate *prepass.Plate
	if resolvePlate {
		localPlate = &prepass.Plate{}
		if err = tx.QueryRow(ctx, `SELECT coalesce(license_plate,''),coalesce(license_state,'') FROM trucks WHERE id=$1`, in.TruckID).Scan(&localPlate.Number, &localPlate.State); err != nil {
			return err
		}
	}
	old, _ := json.Marshal(struct {
		Vehicle        prepass.Vehicle
		TruckID        string
		TruckUpdatedAt string
		TruckPlate     *prepass.Plate
	}{before, linked, in.TruckUpdatedAt, localPlate})
	if _, err = tx.Exec(ctx, `INSERT INTO prepass_fleet_operations(id,environment,account_number,vin,action,request_hash,request,before_vehicle,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, in.RequestID, r.prepassEnvironment, in.AccountNumber, in.VIN, in.Action, hash, raw, old, actor); err != nil {
		return err
	}
	if in.Action == "link" {
		if err = r.writeFleetTollLink(ctx, tx, in, before, actor); err != nil {
			return err
		}
		if err = r.writePrePassLink(ctx, tx, in, actor); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE prepass_fleet_operations SET state='confirmed',resolved_at=now() WHERE id=$1`, in.RequestID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	// A committed receipt precedes every network mutation. Neither a timeout nor
	// a browser retry can send the mutation twice, including across server restarts.
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	var plate prepass.Plate
	if in.Action == "add" || resolvePlate {
		tx, err = conn.Begin(opCtx)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.WithoutCancel(ctx))
		if resolvePlate {
			plate, err = r.checkPrePassPlate(opCtx, tx, in, before)
		} else {
			err = r.checkPrePassTruck(opCtx, tx, in)
		}
		if err != nil {
			_ = tx.Rollback(opCtx)
			_, saveErr := conn.Exec(opCtx, `UPDATE prepass_fleet_operations SET state='failed',resolved_at=now() WHERE id=$1`, in.RequestID)
			if saveErr != nil {
				return saveErr
			}
			return err
		}
	}
	action, vehicle := in.Action, in.Vehicle
	if resolvePlate {
		action = "plate"
		vehicle = prepass.VehicleInput{LicensePlate: plate}
	}
	err = r.prepassFleet.ChangeVehicle(opCtx, action, in.AccountNumber, in.VIN, vehicle, in.TransponderNumber)
	if err != nil {
		if in.Action == "add" || resolvePlate {
			_ = tx.Rollback(opCtx)
		}
		var upstream *prepass.FleetAPIError
		if errors.As(err, &upstream) && !upstream.Uncertain {
			if _, e := conn.Exec(opCtx, `UPDATE prepass_fleet_operations SET state='failed',resolved_at=now() WHERE id=$1`, in.RequestID); e != nil {
				return e
			}
		}
		return err
	}
	after, err := r.fetchPrePassFleet(opCtx)
	if err != nil {
		if in.Action == "add" || resolvePlate {
			_ = tx.Rollback(opCtx)
		}
		return prepassInvalid("PrePass accepted the change, but it could not be verified. Review the unresolved request before retrying.")
	}
	actual, found := findPrePassVehicle(after, in.AccountNumber, in.VIN)
	verified := false
	switch in.Action {
	case "add", "edit":
		verified = found && actual.Input() == in.Vehicle
		if resolvePlate {
			verified = found && actual.Input().LicensePlate == plate
		}
	case "remove":
		verified = !found
	case "assign":
		verified = found && actual.TransponderNumber == in.TransponderNumber
		if verified {
			verified = false
			for _, d := range after.Transponders {
				if d.AccountNumber == in.AccountNumber && d.TransponderNumber == in.TransponderNumber && strings.EqualFold(d.VIN, in.VIN) {
					verified = true
				}
			}
		}
	}
	if !verified {
		if in.Action == "add" || resolvePlate {
			_ = tx.Rollback(opCtx)
		}
		return prepassInvalid("PrePass has not yet shown the expected change. Review the unresolved request before retrying.")
	}
	if resolvePlate {
		if _, err = tx.Exec(opCtx, `UPDATE trucks SET license_plate=$2,license_state=$3,updated_at=clock_timestamp() WHERE id=$1`, in.TruckID, plate.Number, plate.State); err != nil {
			return err
		}
		// Keep the local plate, verified roster and receipt in one commit.
		roster, _ := json.Marshal(after)
		if _, err = tx.Exec(opCtx, `INSERT INTO prepass_fleet_rosters(environment,snapshot) VALUES($1,$2) ON CONFLICT(environment) DO UPDATE SET snapshot=EXCLUDED.snapshot,refreshed_at=now()`, r.prepassEnvironment, roster); err != nil {
			return err
		}
		if _, err = tx.Exec(opCtx, `UPDATE prepass_fleet_operations SET state='confirmed',resolved_at=now() WHERE id=$1`, in.RequestID); err != nil {
			return err
		}
		return tx.Commit(opCtx)
	}
	if in.Action == "add" {
		if err = r.writePrePassLink(opCtx, tx, in, actor); err != nil {
			return err
		}
		if err = tx.Commit(opCtx); err != nil {
			return err
		}
	}
	if err = r.savePrePassFleet(opCtx, conn, after); err != nil {
		return err
	}
	_, err = conn.Exec(opCtx, `UPDATE prepass_fleet_operations SET state='confirmed',resolved_at=now() WHERE id=$1`, in.RequestID)
	return err
}

// Lock the reviewed local identity through read-back. The selected source is
// derived server-side, so callers cannot smuggle unrelated profile changes.
func (r *TollRepository) checkPrePassPlate(ctx context.Context, tx pgx.Tx, in PrePassFleetChange, before prepass.Vehicle) (prepass.Plate, error) {
	var vin, number, state string
	var updated time.Time
	err := tx.QueryRow(ctx, `SELECT coalesce(vin,''),coalesce(license_plate,''),coalesce(license_state,''),updated_at FROM trucks WHERE id=$1 FOR UPDATE`, in.TruckID).Scan(&vin, &number, &state, &updated)
	if err != nil {
		return prepass.Plate{}, mapNotFound(err)
	}
	expected, err := time.Parse(time.RFC3339Nano, in.TruckUpdatedAt)
	if err != nil || !updated.Equal(expected) || strings.ToUpper(strings.TrimSpace(vin)) != in.VIN {
		return prepass.Plate{}, ErrPrePassFleetConflict
	}
	p := before.Input().LicensePlate
	if in.PlateSource == "mserp" {
		p = prepass.Plate{Number: number, State: state}
	}
	if err := p.Validate(); err != nil {
		return p, prepassInvalid(err.Error())
	}
	return p, nil
}
func (r *TollRepository) checkPrePassTruck(ctx context.Context, tx pgx.Tx, in PrePassFleetChange) error {
	var vin string
	var active bool
	var updated time.Time
	err := tx.QueryRow(ctx, `SELECT coalesce(vin,''),active,updated_at FROM trucks WHERE id=$1 FOR SHARE`, in.TruckID).Scan(&vin, &active, &updated)
	if err != nil {
		return mapNotFound(err)
	}
	expected, err := time.Parse(time.RFC3339Nano, in.TruckUpdatedAt)
	if err != nil || !updated.Equal(expected) {
		return ErrPrePassFleetConflict
	}
	missingVIN := strings.TrimSpace(vin) == "" && in.Action == "add"
	if strings.TrimSpace(vin) == "" && in.Action == "link" {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prepass_fleet_links WHERE environment=$1 AND account_number=$2 AND vin=$3 AND truck_id=$4)`, r.prepassEnvironment, in.AccountNumber, in.VIN, in.TruckID).Scan(&missingVIN); err != nil {
			return err
		}
	}
	if !missingVIN && strings.ToUpper(strings.TrimSpace(vin)) != in.VIN {
		return prepassInvalid("The selected MSERP truck must have the same VIN. Correct its profile first.")
	}
	if missingVIN {
		// A missing VIN may be supplied for this enrollment, without changing the
		// local profile. Do not attach another known truck's identity to this row.
		var otherTruck bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks WHERE id<>$1 AND upper(trim(vin))=$2)`, in.TruckID, in.VIN).Scan(&otherTruck); err != nil {
			return err
		}
		if !prepass.FleetVIN.MatchString(in.VIN) || otherTruck {
			return prepassInvalid("Provide a valid VIN that does not belong to another MSERP truck")
		}
	}
	if in.Action == "add" && !active {
		return prepassInvalid("Only active MSERP trucks can be added to PrePass")
	}
	var conflict bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prepass_fleet_links WHERE environment=$1 AND truck_id=$2 AND (account_number<>$3 OR vin<>$4))`, r.prepassEnvironment, in.TruckID, in.AccountNumber, in.VIN).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return prepassInvalid("This MSERP truck is already linked to another PrePass vehicle")
	}
	return nil
}
func (r *TollRepository) writePrePassLink(ctx context.Context, tx pgx.Tx, in PrePassFleetChange, actor string) error {
	_, err := tx.Exec(ctx, `INSERT INTO prepass_fleet_links(environment,account_number,vin,truck_id,updated_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT(environment,account_number,vin) DO UPDATE SET truck_id=EXCLUDED.truck_id,updated_by=EXCLUDED.updated_by,updated_at=now()`, r.prepassEnvironment, in.AccountNumber, in.VIN, in.TruckID, actor)
	return err
}
func (r *TollRepository) VerifyPrePassFleetOperation(ctx context.Context, id, actor string) error {
	conn, done, err := r.prepassLock(ctx)
	if err != nil {
		return err
	}
	defer done()
	snap, err := r.fetchPrePassFleet(ctx)
	if err != nil {
		return err
	}
	if err = r.savePrePassFleet(ctx, conn, snap); err != nil {
		return err
	}
	result, err := conn.Exec(ctx, `UPDATE prepass_fleet_operations SET state='verified',resolved_at=now(),resolved_by=$3 WHERE id=$1 AND environment=$2 AND state='pending' AND created_at<now()-interval '2 minutes'`, id, r.prepassEnvironment, actor)
	if err == nil && result.RowsAffected() != 1 {
		return ErrPrePassFleetConflict
	}
	return err
}
