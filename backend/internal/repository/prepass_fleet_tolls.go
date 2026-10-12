package repository

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/prepass"
)

func fleetTollKeys(v prepass.Vehicle) []string {
	keys := []string{}
	if tag := strings.ToUpper(strings.TrimSpace(v.TransponderNumber)); tag != "" {
		keys = append(keys, "tag:"+tag)
	}
	if unit := strings.ToUpper(strings.TrimSpace(v.VehicleNumber)); unit != "" && unit != "UNKNOWN" && !strings.HasPrefix(unit, "UNASSIGNED") {
		keys = append(keys, "unit:"+unit)
	}
	return keys
}

func validateFleetTollLink(in PrePassFleetChange) error {
	if _, err := time.Parse(time.DateOnly, in.TollFrom); err != nil {
		return prepassInvalid("Select the start date for toll linking")
	}
	return nil
}

// Only unlinked rows can be affected. A reviewed tag rule for another vehicle
// takes precedence over this vehicle's unit fallback.
const fleetTollAffected = ` FROM tolls t WHERE t.truck_id IS NULL
 AND coalesce(t.prepass_environment,'production')=$1 AND t.customer_id=$2 AND t.exit_date >= $3::date
 AND (('tag:'||upper(btrim(coalesce(t.prepass_tag_id,''))))=ANY($4::text[])
 OR (prepass_truck_key(NULL,t.equipment_unit)=ANY($4::text[]) AND NOT EXISTS(
 SELECT 1 FROM prepass_truck_mappings m WHERE m.active AND m.environment=$1
 AND (m.account_number IS NULL OR m.account_number=$2)
 AND m.identity_key='tag:'||upper(btrim(coalesce(t.prepass_tag_id,'')))
 AND t.exit_date>=m.effective_from AND (m.effective_to IS NULL OR t.exit_date<=m.effective_to)
 AND NOT (m.account_number IS NOT DISTINCT FROM $2 AND m.fleet_vin IS NOT DISTINCT FROM $5))))`

func (r *TollRepository) fleetTollPreview(ctx context.Context, tx pgx.Tx, in PrePassFleetChange, v prepass.Vehicle) (PrePassTruckPreview, []string, error) {
	out := PrePassTruckPreview{Units: []string{}}
	keys := fleetTollKeys(v)
	if len(keys) == 0 {
		return out, nil, prepassInvalid("This PrePass vehicle has no tag or usable unit number")
	}
	var conflict bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prepass_truck_mappings WHERE active AND environment=$1
 AND (account_number IS NULL OR account_number=$2) AND identity_key=ANY($3::text[])
 AND (effective_to IS NULL OR effective_to >= $4::date) AND truck_id<>$5
 AND NOT (account_number IS NOT DISTINCT FROM $2 AND fleet_vin IS NOT DISTINCT FROM $6))`, r.prepassEnvironment, in.AccountNumber, keys, in.TollFrom, in.TruckID, in.VIN).Scan(&conflict)
	if err != nil {
		return out, nil, err
	}
	if conflict {
		return out, nil, prepassInvalid("Another truck has an overlapping toll mapping. Review it in Needs attention before linking.")
	}
	var mappingSnapshot string
	err = tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]')::text FROM prepass_truck_mappings m
 WHERE active AND environment=$1 AND (account_number IS NULL OR account_number=$2)
 AND (identity_key=ANY($3::text[]) OR fleet_vin=$4)`, r.prepassEnvironment, in.AccountNumber, keys, in.VIN).Scan(&mappingSnapshot)
	if err != nil {
		return out, nil, err
	}
	var ids []string
	var snapshot string
	err = tx.QueryRow(ctx, `SELECT count(*),coalesce(sum(t.amount),0)::text,coalesce(array_agg(DISTINCT t.equipment_unit),'{}'),coalesce(array_agg(t.id::text ORDER BY t.id),'{}'),coalesce(jsonb_agg(jsonb_build_array(t.id,t.amount,t.posting_date,t.exit_date,t.equipment_unit,t.prepass_tag_id) ORDER BY t.id),'[]')::text`+fleetTollAffected, r.prepassEnvironment, in.AccountNumber, in.TollFrom, keys, in.VIN).Scan(&out.Count, &out.Amount, &out.Units, &ids, &snapshot)
	if err != nil {
		return out, nil, err
	}
	out.Closed, err = prePassClosed(ctx, tx, ids, in.TruckID)
	if err != nil {
		return out, nil, err
	}
	in.TollRevision = ""
	in.RequestID = ""
	out.Revision = fleetHash(struct {
		Input           PrePassFleetChange
		Vehicle         prepass.Vehicle
		Mappings, Tolls string
		Closed          bool
	}{in, v, mappingSnapshot, snapshot, out.Closed})
	return out, ids, nil
}

func (r *TollRepository) PreviewPrePassFleetLink(ctx context.Context, in PrePassFleetChange) (PrePassTruckPreview, error) {
	if err := validateFleetTollLink(in); err != nil {
		return PrePassTruckPreview{}, err
	}
	conn, done, err := r.prepassLock(ctx)
	if err != nil {
		return PrePassTruckPreview{}, err
	}
	defer done()
	snap, err := r.fetchPrePassFleet(ctx)
	if err != nil {
		return PrePassTruckPreview{}, err
	}
	v, found := findPrePassVehicle(snap, in.AccountNumber, in.VIN)
	if !found {
		return PrePassTruckPreview{}, ErrPrePassFleetConflict
	}
	if err = validateFleetIdentity(snap, v); err != nil {
		return PrePassTruckPreview{}, err
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return PrePassTruckPreview{}, err
	}
	defer tx.Rollback(ctx)
	var linked string
	if err = tx.QueryRow(ctx, `SELECT coalesce((SELECT truck_id::text FROM prepass_fleet_links WHERE environment=$1 AND account_number=$2 AND vin=$3),'')`, r.prepassEnvironment, in.AccountNumber, in.VIN).Scan(&linked); err != nil {
		return PrePassTruckPreview{}, err
	}
	if prepassRevision(v, linked) != in.Revision {
		return PrePassTruckPreview{}, ErrPrePassFleetConflict
	}
	if err = r.checkPrePassTruck(ctx, tx, in); err != nil {
		return PrePassTruckPreview{}, err
	}
	out, _, err := r.fleetTollPreview(ctx, tx, in, v)
	return out, err
}

func validateFleetIdentity(s prepass.FleetSnapshot, v prepass.Vehicle) error {
	for _, other := range s.Vehicles {
		if other.AccountNumber == v.AccountNumber && other.VIN != v.VIN && (strings.EqualFold(strings.TrimSpace(other.VehicleNumber), strings.TrimSpace(v.VehicleNumber)) || v.TransponderNumber != "" && other.TransponderNumber == v.TransponderNumber) {
			return prepassInvalid("Multiple PrePass vehicles share this unit or tag. Correct the roster before linking tolls.")
		}
	}
	return nil
}

func (r *TollRepository) writeFleetTollLink(ctx context.Context, tx pgx.Tx, in PrePassFleetChange, v prepass.Vehicle, actor string) error {
	// Caller holds the financial and toll-mapping locks, then the truck row lock.
	preview, ids, err := r.fleetTollPreview(ctx, tx, in, v)
	if err != nil {
		return err
	}
	if preview.Revision != in.TollRevision {
		return prepassInvalid("Tolls or mappings changed. Preview the link again.")
	}
	if preview.Closed {
		return prepassInvalid("Reopen affected statements before linking these tolls.")
	}
	// Retire only this fleet vehicle's old rules. Keep all their existing toll links.
	_, err = tx.Exec(ctx, `WITH closed AS (
 UPDATE prepass_truck_mappings SET effective_to=CASE WHEN effective_from<$4::date THEN $4::date-1 ELSE effective_to END,
 active=effective_from<$4::date,version=version+1
 WHERE active AND environment=$1 AND account_number=$2 AND fleet_vin=$3 AND (effective_to IS NULL OR effective_to >= $4::date)
 RETURNING id)
 INSERT INTO prepass_truck_mapping_events(mapping_id,actor_id,action,transaction_ids)
 SELECT id,$5,'close','[]'::jsonb FROM closed`, r.prepassEnvironment, in.AccountNumber, in.VIN, in.TollFrom, actor)
	if err != nil {
		return err
	}
	for _, key := range fleetTollKeys(v) {
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO prepass_truck_mappings(id,environment,account_number,fleet_vin,fleet_request_id,identity_key,truck_id,effective_from,actor_id)
  VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`, r.prepassEnvironment, in.AccountNumber, in.VIN, in.RequestID, key, in.TruckID, in.TollFrom, actor).Scan(&id)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO prepass_truck_mapping_events(mapping_id,actor_id,action,transaction_ids) VALUES($1,$2,'link','[]'::jsonb)`, id, actor); err != nil {
			return err
		}
	}
	// The trigger resolves tag first, then exact unit; only reviewed NULL rows run.
	if _, err = tx.Exec(ctx, `UPDATE tolls SET truck_id=NULL WHERE id=ANY($1::uuid[]) AND truck_id IS NULL`, ids); err != nil {
		return err
	}
	// Store the exact backfilled IDs for each rule. Future imports retain mapping_id.
	_, err = tx.Exec(ctx, `UPDATE prepass_truck_mapping_events e SET transaction_ids=(SELECT coalesce(jsonb_agg(t.id::text ORDER BY t.id),'[]') FROM tolls t WHERE t.truck_mapping_id=e.mapping_id AND t.id=ANY($2::uuid[]))
 WHERE e.mapping_id IN (SELECT id FROM prepass_truck_mappings WHERE fleet_request_id=$1) AND e.action='link'`, in.RequestID, ids)
	return err
}
