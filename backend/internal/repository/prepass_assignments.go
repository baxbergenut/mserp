package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/prepass"
)

func confirmedPrePassAssignment(s prepass.FleetSnapshot, account, vin, device string) bool {
	vehicle, found := findPrePassVehicle(s, account, vin)
	if !found || device == "" || vehicle.TransponderNumber != device {
		return false
	}
	found = false
	for _, d := range s.Transponders {
		if d.TransponderNumber != device {
			continue
		}
		if d.AccountNumber != account || !strings.EqualFold(d.VIN, vin) {
			return false
		}
		found = true
	}
	for _, v := range s.Vehicles {
		if v.TransponderNumber == device && (v.AccountNumber != account || v.VIN != vin) {
			return false
		}
	}
	return found
}

// ReconcilePrePassAssignments only reads the vendor. It survives browser closes
// and API restarts and never repeats a mutation, including uncertain old requests.
func (r *TollRepository) ReconcilePrePassAssignments(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var waiting bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prepass_fleet_operations WHERE environment=$1 AND action='assign' AND state IN ('pending','queued'))`, r.prepassEnvironment).Scan(&waiting); err != nil {
		return err
	}
	if !waiting {
		return nil
	}
	conn, done, err := r.prepassLock(ctx)
	if errors.Is(err, ErrPrePassFleetConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	defer done()
	snap, err := r.fetchPrePassFleet(ctx)
	if err != nil {
		return err
	}
	return r.savePrePassAssignments(ctx, conn, snap)
}

func (r *TollRepository) savePrePassAssignments(ctx context.Context, conn *pgxpool.Conn, snap prepass.FleetSnapshot) error {
	rows, err := conn.Query(ctx, `SELECT id::text,account_number,vin,request->>'transponderNumber' FROM prepass_fleet_operations WHERE environment=$1 AND action='assign' AND state IN ('pending','queued')`, r.prepassEnvironment)
	if err != nil {
		return err
	}
	var confirmed []string
	for rows.Next() {
		var id, account, vin, device string
		if err = rows.Scan(&id, &account, &vin, &device); err != nil {
			rows.Close()
			return err
		}
		if confirmedPrePassAssignment(snap, account, vin, device) {
			confirmed = append(confirmed, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO prepass_fleet_rosters(environment,snapshot) VALUES($1,$2) ON CONFLICT(environment) DO UPDATE SET snapshot=EXCLUDED.snapshot,refreshed_at=now()`, r.prepassEnvironment, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE prepass_fleet_operations SET state='confirmed',resolved_at=now() WHERE environment=$1 AND id=ANY($2::uuid[]) AND action='assign' AND state IN ('pending','queued')`, r.prepassEnvironment, confirmed); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
