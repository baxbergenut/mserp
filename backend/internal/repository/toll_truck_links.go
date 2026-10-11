package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type TollLinkError struct{ Message string }

func (e *TollLinkError) Error() string { return e.Message }

type PrePassTruckGroup struct {
	Environment string   `json:"environment"`
	IdentityKey string   `json:"identityKey"`
	Units       []string `json:"units"`
	Devices     []string `json:"devices"`
	Count       int      `json:"count"`
	Amount      string   `json:"amount"`
	FirstExit   string   `json:"firstExit"`
	LastExit    string   `json:"lastExit"`
}
type PrePassTruckChoice struct {
	ID    string `json:"id"`
	Unit  string `json:"unit"`
	VIN   string `json:"vin"`
	Plate string `json:"plate"`
}
type PrePassTruckMapping struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
	IdentityKey string `json:"identityKey"`
	TruckID     string `json:"truckId"`
	Unit        string `json:"unit"`
	From        string `json:"from"`
	To          string `json:"to"`
	Version     int    `json:"version"`
	Count       int    `json:"count"`
}
type PrePassTruckLinks struct {
	DateFrom string                `json:"dateFrom"`
	Groups   []PrePassTruckGroup   `json:"groups"`
	Trucks   []PrePassTruckChoice  `json:"trucks"`
	Mappings []PrePassTruckMapping `json:"mappings"`
}

func (r *TollRepository) PrePassTruckLinks(ctx context.Context, from string) (PrePassTruckLinks, error) {
	out := PrePassTruckLinks{DateFrom: from, Groups: []PrePassTruckGroup{}, Trucks: []PrePassTruckChoice{}, Mappings: []PrePassTruckMapping{}}
	if from == "" {
		if err := r.pool.QueryRow(ctx, `SELECT (date_trunc('month',now() AT TIME ZONE 'America/New_York')-interval '1 month')::date::text`).Scan(&out.DateFrom); err != nil {
			return out, err
		}
	}
	if _, err := time.Parse(time.DateOnly, out.DateFrom); err != nil {
		return out, &TollLinkError{"Select a valid posting date."}
	}
	rows, err := r.pool.Query(ctx, `SELECT coalesce(prepass_environment,'production'),prepass_truck_key(prepass_tag_id,equipment_unit),
 array_agg(DISTINCT equipment_unit ORDER BY equipment_unit),array_agg(DISTINCT transponder_or_plate ORDER BY transponder_or_plate),count(*),sum(amount)::text,min(exit_date)::text,max(exit_date)::text
 FROM tolls WHERE truck_id IS NULL AND posting_date >= $1::date
 GROUP BY 1,2 ORDER BY count(*) DESC,1,2`, out.DateFrom)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v PrePassTruckGroup
		if err = rows.Scan(&v.Environment, &v.IdentityKey, &v.Units, &v.Devices, &v.Count, &v.Amount, &v.FirstExit, &v.LastExit); err != nil {
			rows.Close()
			return out, err
		}
		out.Groups = append(out.Groups, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = r.pool.Query(ctx, `SELECT id::text,unit_number,coalesce(vin,''),coalesce(license_plate,'') FROM trucks ORDER BY unit_number,id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v PrePassTruckChoice
		if err = rows.Scan(&v.ID, &v.Unit, &v.VIN, &v.Plate); err != nil {
			rows.Close()
			return out, err
		}
		out.Trucks = append(out.Trucks, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = r.pool.Query(ctx, `SELECT m.id::text,m.environment,m.identity_key,m.truck_id::text,t.unit_number,m.effective_from::text,coalesce(m.effective_to::text,''),m.version,(SELECT count(*) FROM tolls WHERE truck_mapping_id=m.id) FROM prepass_truck_mappings m JOIN trucks t ON t.id=m.truck_id WHERE m.active ORDER BY m.created_at DESC,m.id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v PrePassTruckMapping
		if err = rows.Scan(&v.ID, &v.Environment, &v.IdentityKey, &v.TruckID, &v.Unit, &v.From, &v.To, &v.Version, &v.Count); err != nil {
			return out, err
		}
		out.Mappings = append(out.Mappings, v)
	}
	return out, rows.Err()
}

type PrePassTruckLinkInput struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
	IdentityKey string `json:"identityKey"`
	TruckID     string `json:"truckId"`
	From        string `json:"from"`
	To          string `json:"to"`
	Revision    string `json:"revision"`
}
type PrePassTruckPreview struct {
	Count    int      `json:"count"`
	Amount   string   `json:"amount"`
	Units    []string `json:"units"`
	Revision string   `json:"revision"`
	Closed   bool     `json:"closed"`
}

func validatePrePassLink(in PrePassTruckLinkInput) error {
	if in.Environment != "production" && in.Environment != "nonproduction" {
		return &TollLinkError{"Invalid PrePass environment."}
	}
	if len(in.IdentityKey) > 250 || !(strings.HasPrefix(in.IdentityKey, "tag:") && len(in.IdentityKey) > 4 || strings.HasPrefix(in.IdentityKey, "unit:") && len(in.IdentityKey) > 5) {
		return &TollLinkError{"A PrePass tag or unit is required."}
	}
	if _, err := time.Parse(time.DateOnly, in.From); err != nil {
		return &TollLinkError{"Select a valid start date."}
	}
	if in.To != "" {
		if _, err := time.Parse(time.DateOnly, in.To); err != nil || in.To < in.From {
			return &TollLinkError{"Select an end date on or after the start date."}
		}
	}
	return nil
}

const prePassAffected = ` FROM tolls t WHERE t.truck_id IS NULL AND coalesce(t.prepass_environment,'production')=$1 AND prepass_truck_key(t.prepass_tag_id,t.equipment_unit)=$2 AND t.exit_date >= $3::date AND ($4::date IS NULL OR t.exit_date <= $4::date)`

func prePassArgs(in PrePassTruckLinkInput) []any {
	return []any{in.Environment, in.IdentityKey, in.From, nullableDate(in.To)}
}
func nullableDate(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func prePassClosed(ctx context.Context, tx pgx.Tx, ids []string, truck string) (bool, error) {
	var closed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tolls t JOIN investor_pay_weeks p ON p.truck_id=$2::uuid AND p.week_start=date_trunc('week',t.posting_date)::date AND p.finalized WHERE t.id=ANY($1::uuid[]))
 OR EXISTS(SELECT 1 FROM tolls t JOIN truck_driver_assignments a ON a.truck_id=$2::uuid AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=t.exit_date
 AND (a.unassigned_at IS NULL OR t.exit_date<(a.unassigned_at AT TIME ZONE 'America/New_York')::date)
 JOIN payroll_settlements p ON p.driver_id=a.driver_id AND p.week_start=date_trunc('week',t.posting_date)::date AND p.finalized WHERE t.id=ANY($1::uuid[]))`, ids, truck).Scan(&closed)
	return closed, err
}
func prePassPreview(ctx context.Context, tx pgx.Tx, in PrePassTruckLinkInput) (PrePassTruckPreview, []string, error) {
	out := PrePassTruckPreview{Units: []string{}}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks WHERE id=$1)`, in.TruckID).Scan(&exists); err != nil {
		return out, nil, err
	}
	if !exists {
		return out, nil, &TollLinkError{"Truck no longer exists."}
	}
	var ids []string
	var snapshot string
	err := tx.QueryRow(ctx, `SELECT count(*),coalesce(sum(t.amount),0)::text,coalesce(array_agg(DISTINCT t.equipment_unit),'{}'),coalesce(array_agg(t.id::text ORDER BY t.id),'{}'),coalesce(jsonb_agg(jsonb_build_array(t.id,t.amount,t.posting_date,t.exit_date,t.equipment_unit,t.transponder_or_plate) ORDER BY t.id),'[]')::text`+prePassAffected, prePassArgs(in)...).Scan(&out.Count, &out.Amount, &out.Units, &ids, &snapshot)
	if err != nil {
		return out, nil, err
	}
	out.Closed, err = prePassClosed(ctx, tx, ids, in.TruckID)
	if err != nil {
		return out, nil, err
	}
	in.Revision = ""
	in.ID = ""
	raw, _ := json.Marshal(in)
	out.Revision = fmt.Sprintf("%x", sha256.Sum256(append(raw, []byte(snapshot)...)))
	return out, ids, nil
}
func (r *TollRepository) PreviewPrePassTruckLink(ctx context.Context, in PrePassTruckLinkInput) (PrePassTruckPreview, error) {
	if err := validatePrePassLink(in); err != nil {
		return PrePassTruckPreview{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PrePassTruckPreview{}, err
	}
	defer tx.Rollback(ctx)
	out, _, err := prePassPreview(ctx, tx, in)
	return out, err
}
func (r *TollRepository) LinkPrePassTruck(ctx context.Context, in PrePassTruckLinkInput, actor string) error {
	if err := validatePrePassLink(in); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940); SELECT pg_advisory_xact_lock(730077)`); err != nil {
		return err
	}
	var identical bool
	err = tx.QueryRow(ctx, `SELECT environment=$2 AND identity_key=$3 AND truck_id=$4::uuid AND effective_from=$5::date AND effective_to IS NOT DISTINCT FROM $6::date AND active FROM prepass_truck_mappings WHERE id=$1`, in.ID, in.Environment, in.IdentityKey, in.TruckID, in.From, nullableDate(in.To)).Scan(&identical)
	if err == nil {
		if identical {
			return nil
		}
		return &TollLinkError{"This request was already used. Refresh the truck links."}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var overlap bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prepass_truck_mappings WHERE active AND environment=$1 AND identity_key=$2 AND (effective_to IS NULL OR effective_to >= $3::date) AND ($4::date IS NULL OR effective_from<=$4::date))`, prePassArgs(in)...).Scan(&overlap)
	if err != nil {
		return err
	}
	if overlap {
		return &TollLinkError{"An existing mapping overlaps these dates. Undo it before replacing it."}
	}
	// Lock all candidate rows before checking the reviewed snapshot and settlement state.
	rows, err := tx.Query(ctx, `SELECT t.id`+prePassAffected+` FOR UPDATE`, prePassArgs(in)...)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	preview, ids, err := prePassPreview(ctx, tx, in)
	if err != nil {
		return err
	}
	if preview.Count == 0 || preview.Revision != in.Revision {
		return &TollLinkError{"Transactions changed. Preview the mapping again."}
	}
	if preview.Closed {
		return &TollLinkError{"Reopen affected statements before linking these tolls."}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO prepass_truck_mappings(id,environment,identity_key,truck_id,effective_from,effective_to,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, in.ID, in.Environment, in.IdentityKey, in.TruckID, in.From, nullableDate(in.To), actor); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE tolls SET truck_id=$2,truck_mapping_id=$3 WHERE id=ANY($1::uuid[])`, ids, in.TruckID, in.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO prepass_truck_mapping_events(mapping_id,actor_id,action,transaction_ids) VALUES($1,$2,'link',to_jsonb($3::text[]))`, in.ID, actor, ids); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *TollRepository) UndoPrePassTruckLink(ctx context.Context, id string, version int, actor string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940); SELECT pg_advisory_xact_lock(730077)`); err != nil {
		return err
	}
	var truck string
	if err = tx.QueryRow(ctx, `SELECT truck_id::text FROM prepass_truck_mappings WHERE id=$1 AND active AND version=$2 FOR UPDATE`, id, version).Scan(&truck); errors.Is(err, pgx.ErrNoRows) {
		return &TollLinkError{"This mapping changed. Refresh before undoing it."}
	} else if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM tolls WHERE truck_mapping_id=$1 FOR UPDATE`, id)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	closed, err := prePassClosed(ctx, tx, ids, truck)
	if err != nil {
		return err
	}
	if closed {
		return &TollLinkError{"Reopen affected statements before undoing this mapping."}
	}
	if _, err = tx.Exec(ctx, `UPDATE prepass_truck_mappings SET active=false,version=version+1 WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE tolls SET truck_id=NULL,truck_mapping_id=NULL WHERE truck_mapping_id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO prepass_truck_mapping_events(mapping_id,actor_id,action,transaction_ids) VALUES($1,$2,'undo',to_jsonb($3::text[]))`, id, actor, ids); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
