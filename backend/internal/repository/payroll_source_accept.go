package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type PaySourceAcceptance struct {
	Field        string `json:"field,omitempty"`
	DriverID     string `json:"driverId"`
	Date         string `json:"date"`
	Slot         int    `json:"slot"`
	Version      int    `json:"version"`
	LoadRecordID int    `json:"loadRecordId"`
	OriginalRate string `json:"originalRate"`
	Miles        string `json:"miles"`
}

// A receipt restores exact nullable inputs, and can only be used once by its
// author while the board entry is still at the version that action produced.
func (r *DriverPayRepository) UndoPaySource(ctx context.Context, id, actor string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	var input PaySourceAcceptance
	var appliedVersion int
	var before []byte
	err = tx.QueryRow(ctx, `SELECT driver_id::text,service_date::text,slot,expected_version,applied_version,before_values
 FROM payroll_source_actions WHERE id=$1 AND actor_id=$2 AND actor_id<>'' AND undone_at IS NULL FOR UPDATE`, id, actor).Scan(&input.DriverID, &input.Date, &input.Slot, &input.Version, &appliedVersion, &before)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDriverPayConflict
	}
	if err != nil {
		return err
	}
	if err = lockChargeDrivers(ctx, tx, []string{input.DriverID}); err != nil {
		return err
	}
	if err = assertPaySourceOpen(ctx, tx, input); err != nil {
		return err
	}
	table, slot := "gross_board_entries", "0"
	if input.Slot > 0 {
		table, slot = "gross_board_extra_entries", "slot"
	}
	command, err := tx.Exec(ctx, `UPDATE `+table+` SET
 original_rate=CASE WHEN $5::jsonb ? 'original' THEN ($5::jsonb->>'original')::numeric ELSE original_rate END,
 entered_original_rate=CASE WHEN $5::jsonb ? 'original' THEN ($5::jsonb->>'enteredOriginal')::numeric ELSE entered_original_rate END,
 miles=CASE WHEN $5::jsonb ? 'miles' THEN ($5::jsonb->>'miles')::numeric ELSE miles END,
 entered_miles=CASE WHEN $5::jsonb ? 'miles' THEN ($5::jsonb->>'enteredMiles')::numeric ELSE entered_miles END,
 version=version+1,updated_at=now()
 WHERE driver_id=$1 AND service_date=$2::date AND `+slot+`=$3 AND version=$4`, input.DriverID, input.Date, input.Slot, input.Version, json.RawMessage(before))
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrDriverPayConflict
	}
	if _, err = tx.Exec(ctx, "UPDATE payroll_source_actions SET undone_at=now() WHERE id=$1", id); err != nil {
		return err
	}
	// Reversing this actor's next action may expose their immediately preceding
	// receipt. Advance only its expected version; retain the original audit version.
	if _, err = tx.Exec(ctx, `UPDATE payroll_source_actions SET expected_version=$6
 WHERE driver_id=$1 AND service_date=$2::date AND slot=$3 AND actor_id=$4
 AND expected_version=$5 AND undone_at IS NULL`, input.DriverID, input.Date, input.Slot, actor, appliedVersion-1, input.Version+1); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Change only the selected financial input (both for legacy callers). A concurrent board edit or
// source refresh rejects the action; driver gross and plan identity are untouched.
func (r *DriverPayRepository) AcceptPaySource(ctx context.Context, input PaySourceAcceptance) error {
	_, err := r.AcceptPaySourceWithUndo(ctx, input, "")
	return err
}

func (r *DriverPayRepository) AcceptPaySourceWithUndo(ctx context.Context, input PaySourceAcceptance, actor string) (string, error) {
	if input.Field != "" && input.Field != "originalRate" && input.Field != "totalMiles" {
		return "", chargeInvalid("Invalid source field")
	}
	if input.Field == "originalRate" {
		input.Miles = ""
	}
	if input.Field == "totalMiles" {
		input.OriginalRate = ""
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// Serialize with both kinds of finalization before taking row locks.
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return "", err
	}
	if err = lockChargeDrivers(ctx, tx, []string{input.DriverID}); err != nil {
		return "", err
	}
	if err = assertPaySourceOpen(ctx, tx, input); err != nil {
		return "", err
	}
	var source, miles string
	err = tx.QueryRow(ctx, `SELECT total_pay::text,coalesce(total_miles::text,'') FROM loads WHERE id=$1 FOR SHARE`, input.LoadRecordID).Scan(&source, &miles)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDriverPayConflict
	}
	if err != nil {
		return "", err
	}
	var same bool
	if err = tx.QueryRow(ctx, `SELECT ($5='totalMiles' OR $1::numeric IS NOT DISTINCT FROM nullif($2,'')::numeric) AND ($5='originalRate' OR nullif($3,'')::numeric IS NOT DISTINCT FROM nullif($4,'')::numeric)`, source, input.OriginalRate, miles, input.Miles, input.Field).Scan(&same); err != nil {
		return "", err
	}
	if !same {
		return "", ErrDriverPayConflict
	}
	table := "gross_board_entries"
	slot := "0"
	deleted := "true"
	if input.Slot > 0 {
		table, slot, deleted = "gross_board_extra_entries", "slot", "NOT deleted"
	}
	var before []byte
	err = tx.QueryRow(ctx, "SELECT jsonb_build_object('original',original_rate,'enteredOriginal',entered_original_rate,'miles',miles,'enteredMiles',entered_miles) FROM "+table+" WHERE driver_id=$1 AND service_date=$2::date AND "+slot+"=$3 AND version=$4 FOR UPDATE", input.DriverID, input.Date, input.Slot, input.Version).Scan(&before)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDriverPayConflict
	}
	if err != nil {
		return "", err
	}
	var prior map[string]json.RawMessage
	if err = json.Unmarshal(before, &prior); err != nil {
		return "", err
	}
	if input.Field == "originalRate" {
		delete(prior, "miles")
		delete(prior, "enteredMiles")
	}
	if input.Field == "totalMiles" {
		delete(prior, "original")
		delete(prior, "enteredOriginal")
	}
	before, err = json.Marshal(prior)
	if err != nil {
		return "", err
	}
	// A linked entry can resolve dynamically before load_record_id is persisted.
	command, err := tx.Exec(ctx, `UPDATE `+table+` e SET
 original_rate=CASE WHEN $8='totalMiles' THEN original_rate ELSE $5::numeric END,
 entered_original_rate=CASE WHEN $8='totalMiles' THEN entered_original_rate ELSE $5::numeric END,
 miles=CASE WHEN $8='originalRate' THEN miles ELSE nullif($6,'')::numeric END,
 entered_miles=CASE WHEN $8='originalRate' THEN entered_miles ELSE nullif($6,'')::numeric END,version=version+1,updated_at=now()
 WHERE driver_id=$1 AND service_date=$2::date AND `+slot+`=$3 AND version=$4
 AND day_status='' AND `+deleted+`
 AND EXISTS (SELECT 1 FROM loads l WHERE l.id=$7 AND lower(btrim(l.load_id))=lower(btrim(e.load_number))
 AND (e.load_record_id=l.id OR (e.load_record_id IS NULL AND (SELECT count(*) FROM loads x WHERE lower(btrim(x.load_id))=lower(btrim(e.load_number)))=1)))`, input.DriverID, input.Date, input.Slot, input.Version, source, miles, input.LoadRecordID, input.Field)
	if err != nil {
		return "", err
	}
	if command.RowsAffected() != 1 {
		return "", ErrDriverPayConflict
	}
	var id string
	if err = tx.QueryRow(ctx, "INSERT INTO payroll_source_actions(actor_id,driver_id,service_date,slot,applied_version,expected_version,before_values) VALUES($1,$2,$3::date,$4,$5,$5,$6) RETURNING id::text", actor, input.DriverID, input.Date, input.Slot, input.Version+1, json.RawMessage(before)).Scan(&id); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func assertPaySourceOpen(ctx context.Context, tx pgx.Tx, input PaySourceAcceptance) error {
	date, err := time.Parse(time.DateOnly, input.Date)
	if err != nil {
		return chargeInvalid("Invalid load date")
	}
	week := date.AddDate(0, 0, -(int(date.Weekday())+6)%7).Format(time.DateOnly)
	if err = assertPayrollOpen(ctx, tx, input.DriverID, week); err != nil {
		return err
	}
	var frozen bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investor_pay_weeks w,
 jsonb_array_elements(coalesce(w.report->'loads','[]'::jsonb)) l
 WHERE w.week_start=$1::date AND w.finalized AND l->>'sourceDriverId'=$2
 AND l->>'date'=$3 AND l->>'slot'=$4)`, week, input.DriverID, input.Date, strconv.Itoa(input.Slot)).Scan(&frozen); err != nil {
		return err
	}
	if frozen {
		return chargeInvalid("Reopen the related investor settlement before accepting system values")
	}
	return nil
}
