package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// RecordLoadSourceLookup is called only after a complete, validated ID-filtered
// provider response and a successful source save. Omission is operational evidence,
// never authority to delete a financial source or choose a replacement identity.
func (r *LoadRepository) RecordLoadSourceLookup(ctx context.Context, requested, returned []int) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO load_source_availability(load_id,present)
 SELECT l.id,l.id=ANY($2::int[]) FROM loads l WHERE l.id=ANY($1::int[])
 ON CONFLICT(load_id) DO UPDATE SET present=EXCLUDED.present,checked_at=now()`, requested, returned)
	return err
}

const boardAvailableSourceSQL = `NOT EXISTS (SELECT 1 FROM load_source_availability availability WHERE availability.load_id=l.id AND NOT availability.present)`

// Existing explicit historical links remain editable, but cannot be newly
// selected. An omitted identity must never silently replace a pinned source.
func resolveAvailableBoardLoad(ctx context.Context, tx pgx.Tx, e GrossBoardEntry) (*int, error) {
	if e.Deleted || e.DayStatus != "" || e.LoadNumber == "" {
		return nil, nil
	}
	var previous *int
	err := tx.QueryRow(ctx, `SELECT (SELECT load_record_id FROM `+grossBoardEntriesSQL+` b WHERE b.driver_id=$1 AND b.service_date=$2::date AND b.slot=$3 AND NOT b.deleted AND lower(btrim(b.load_number))=lower(btrim($4)))`, e.DriverID, e.Date, e.Slot, e.LoadNumber).Scan(&previous)
	if err != nil {
		return nil, err
	}
	requested := e.LoadRecordID
	if requested == nil && previous != nil {
		requested = previous
	}
	if requested != nil {
		var id int
		err = tx.QueryRow(ctx, `SELECT id FROM loads l WHERE id=$1 AND lower(btrim(load_id))=lower(btrim($2)) AND (`+boardAvailableSourceSQL+` OR id=$3)`, *requested, e.LoadNumber, previous).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: the record may be missing from DataTruck; select a current record", ErrGrossBoardLoad)
		}
		return &id, err
	}
	var id *int
	err = tx.QueryRow(ctx, `SELECT CASE WHEN count(*)=1 THEN min(id) END FROM loads l WHERE lower(btrim(load_id))=lower(btrim($1)) AND `+boardAvailableSourceSQL, e.LoadNumber).Scan(&id)
	return id, err
}
