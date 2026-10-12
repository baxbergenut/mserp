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
				} else if t.Plate != "" && (!strings.EqualFold(t.Plate, v.LicensePlateNumber) || !strings.EqualFold(t.PlateState, v.LicensePlateState)) {
					e.Warning = "Plate differs from MSERP"
				}
			}
		}
		if e.TruckID == "" {
			e.Warning = "Not linked to an MSERP truck"
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
	if !slices.Contains([]string{"add", "edit", "remove", "assign", "link"}, in.Action) {
		return prepassInvalid("Select a supported PrePass action")
	}
	if in.Action != "link" && !in.FleetServiceConfirmed {
		return prepassInvalid("Confirm this truck is not enrolled in VPP")
	}
	if in.Action == "add" || in.Action == "edit" {
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
	if in.Action == "add" {
		for _, v := range snap.Vehicles {
			if v.VIN == in.VIN {
				return prepassInvalid("This VIN is already in PrePass. Link the existing truck.")
			}
		}
	} else if !exists || prepassRevision(before, linked) != in.Revision {
		return ErrPrePassFleetConflict
	}
	if in.Action == "assign" {
		if before.TransponderNumber != "" {
			return prepassInvalid("This truck already has a transponder. Replacement and transfer are not supported here yet.")
		}
		if !availablePrePassDevice(snap, in.AccountNumber, in.TransponderNumber) {
			return prepassInvalid("This transponder is no longer available in the selected account")
		}
	}
	if in.Action == "add" || in.Action == "edit" {
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
	if in.Action == "add" || in.Action == "link" {
		if err = r.checkPrePassTruck(ctx, tx, in); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(in)
	old, _ := json.Marshal(struct {
		Vehicle prepass.Vehicle
		TruckID string
	}{before, linked})
	if _, err = tx.Exec(ctx, `INSERT INTO prepass_fleet_operations(id,environment,account_number,vin,action,request_hash,request,before_vehicle,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, in.RequestID, r.prepassEnvironment, in.AccountNumber, in.VIN, in.Action, hash, raw, old, actor); err != nil {
		return err
	}
	if in.Action == "link" {
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
	if in.Action == "add" {
		tx, err = conn.Begin(opCtx)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.WithoutCancel(ctx))
		if err = r.checkPrePassTruck(opCtx, tx, in); err != nil {
			_ = tx.Rollback(opCtx)
			_, saveErr := conn.Exec(opCtx, `UPDATE prepass_fleet_operations SET state='failed',resolved_at=now() WHERE id=$1`, in.RequestID)
			if saveErr != nil {
				return saveErr
			}
			return err
		}
	}
	err = r.prepassFleet.ChangeVehicle(opCtx, in.Action, in.AccountNumber, in.VIN, in.Vehicle, in.TransponderNumber)
	if err != nil {
		if in.Action == "add" {
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
		if in.Action == "add" {
			_ = tx.Rollback(opCtx)
		}
		return prepassInvalid("PrePass accepted the change, but it could not be verified. Review the unresolved request before retrying.")
	}
	actual, found := findPrePassVehicle(after, in.AccountNumber, in.VIN)
	verified := false
	switch in.Action {
	case "add", "edit":
		verified = found && actual.Input() == in.Vehicle
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
		if in.Action == "add" {
			_ = tx.Rollback(opCtx)
		}
		return prepassInvalid("PrePass has not yet shown the expected change. Review the unresolved request before retrying.")
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
