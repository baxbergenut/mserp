package repository

import "context"

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
