package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (r *LoadRepository) LoadModifiedCheckpoint(ctx context.Context, scope string, initial time.Time) (time.Time, error) {
	var through time.Time
	err := r.pool.QueryRow(ctx, `SELECT through_at FROM datatruck_load_sync_state WHERE scope=$1`, scope).Scan(&through)
	if !errors.Is(err, pgx.ErrNoRows) {
		return through, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO datatruck_load_sync_state(scope,through_at) VALUES($1,$2) ON CONFLICT DO NOTHING`, scope, initial); err != nil {
		return time.Time{}, err
	}
	err = r.pool.QueryRow(ctx, `SELECT through_at FROM datatruck_load_sync_state WHERE scope=$1`, scope).Scan(&through)
	return through, err
}

// Source pages commit first. A crash before this compare-and-swap simply replays
// them; no failed or partially saved sweep can move the durable checkpoint.
func (r *LoadRepository) CompleteModifiedSweep(ctx context.Context, scope string, previous, through time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE datatruck_load_sync_state SET through_at=$3,completed_at=now()
 WHERE scope=$1 AND through_at=$2 AND through_at<=$3`, scope, previous, through)
	if err == nil && tag.RowsAffected() != 1 {
		return fmt.Errorf("modified-load checkpoint changed concurrently")
	}
	return err
}

// All source writers hold the existing payroll source lock before this check.
// Keep the provider version separate from raw_payload: its timestamp alone must
// never change financial revisions. Older reconciliation responses cannot undo
// a newer modified-load page, even if their HTTP requests finished later.
func currentLoadSourceRecords(ctx context.Context, tx pgx.Tx, records []LoadRecord) ([]LoadRecord, error) {
	ids := make([]int, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	rows, err := tx.Query(ctx, `SELECT load_id,modified_datetime FROM datatruck_load_versions WHERE load_id=ANY($1::int[])`, ids)
	if err != nil {
		return nil, err
	}
	versions := make(map[int]time.Time)
	for rows.Next() {
		var id int
		var modified time.Time
		if err := rows.Scan(&id, &modified); err != nil {
			rows.Close()
			return nil, err
		}
		versions[id] = modified
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	accepted := make([]LoadRecord, 0, len(records))
	for _, record := range records {
		if old, ok := versions[record.ID]; ok && (record.ModifiedDatetime == nil || record.ModifiedDatetime.Before(old)) {
			continue
		}
		accepted = append(accepted, record)
	}
	return accepted, nil
}

func saveLoadSourceVersions(ctx context.Context, tx pgx.Tx, records []LoadRecord) error {
	batch := &pgx.Batch{}
	for _, record := range records {
		if record.ModifiedDatetime == nil {
			continue
		}
		// Existing-only refreshes must neither create unseen loads nor advance the
		// numeric discovery watermark. Do not create metadata for unseen IDs.
		batch.Queue(`INSERT INTO datatruck_load_versions(load_id,modified_datetime)
 SELECT id,$2::timestamptz FROM loads WHERE id=$1
 ON CONFLICT(load_id) DO UPDATE SET modified_datetime=excluded.modified_datetime
 WHERE datatruck_load_versions.modified_datetime<excluded.modified_datetime`, record.ID, record.ModifiedDatetime)
	}
	if batch.Len() == 0 {
		return nil
	}
	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	for range batch.Len() {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return results.Close()
}
