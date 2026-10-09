package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type PaySourceAcceptance struct {
	DriverID     string `json:"driverId"`
	Date         string `json:"date"`
	Slot         int    `json:"slot"`
	Version      int    `json:"version"`
	LoadRecordID int    `json:"loadRecordId"`
	OriginalRate string `json:"originalRate"`
	Miles        string `json:"miles"`
}

// Change only the two reviewed financial inputs. A concurrent board edit or
// source refresh rejects the action; driver gross and plan identity are untouched.
func (r *DriverPayRepository) AcceptPaySource(ctx context.Context, input PaySourceAcceptance) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize with both kinds of finalization before taking row locks.
	if err = lockTruckAccounting(ctx, tx); err != nil {
		return err
	}
	if err = lockChargeDrivers(ctx, tx, []string{input.DriverID}); err != nil {
		return err
	}
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
	var source, miles string
	err = tx.QueryRow(ctx, `SELECT total_pay::text,coalesce(total_miles::text,'') FROM loads WHERE id=$1 FOR SHARE`, input.LoadRecordID).Scan(&source, &miles)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDriverPayConflict
	}
	if err != nil {
		return err
	}
	var same bool
	if err = tx.QueryRow(ctx, `SELECT $1::numeric IS NOT DISTINCT FROM nullif($2,'')::numeric AND nullif($3,'')::numeric IS NOT DISTINCT FROM nullif($4,'')::numeric`, source, input.OriginalRate, miles, input.Miles).Scan(&same); err != nil {
		return err
	}
	if !same {
		return ErrDriverPayConflict
	}
	table := "gross_board_entries"
	slot := "0"
	deleted := "true"
	if input.Slot > 0 {
		table, slot, deleted = "gross_board_extra_entries", "slot", "NOT deleted"
	}
	// A linked entry can resolve dynamically before load_record_id is persisted.
	command, err := tx.Exec(ctx, `UPDATE `+table+` e SET original_rate=$5::numeric,entered_original_rate=$5::numeric,
 miles=nullif($6,'')::numeric,entered_miles=nullif($6,'')::numeric,version=version+1,updated_at=now()
 WHERE driver_id=$1 AND service_date=$2::date AND `+slot+`=$3 AND version=$4
 AND day_status='' AND `+deleted+`
 AND EXISTS (SELECT 1 FROM loads l WHERE l.id=$7 AND lower(btrim(l.load_id))=lower(btrim(e.load_number))
 AND (e.load_record_id=l.id OR (e.load_record_id IS NULL AND (SELECT count(*) FROM loads x WHERE lower(btrim(x.load_id))=lower(btrim(e.load_number)))=1)))`, input.DriverID, input.Date, input.Slot, input.Version, source, miles, input.LoadRecordID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrDriverPayConflict
	}
	return tx.Commit(ctx)
}
