package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Impact matching is deliberately broader than financial matching. Every board
// candidate for a reused number must be revisited; only the calculator decides
// whether that number identifies exactly one source. Read raw board rows as well
// as placements so a newly recognized source cannot hide its former destination.
// Frozen reports retain their financial bytes but need current discrepancy notices.
func payrollLoadImpactRoots(ctx context.Context, tx pgx.Tx, ids []int64, numbers []string, roots map[string]bool) error {
	rows, err := tx.Query(ctx, `WITH numbers AS (
 SELECT unnest($2::text[]) number UNION SELECT lower(btrim(load_id)) FROM loads WHERE id=ANY($1::bigint[])
 ) SELECT DISTINCT root FROM (
 SELECT 'driver:'||e.driver_id root FROM `+grossBoardEntriesSQL+` e
 WHERE e.load_record_id=ANY($1::bigint[]) OR lower(btrim(e.load_number)) IN (SELECT number FROM numbers)
 UNION SELECT 'driver:'||p.driver_id FROM payroll_load_placements p JOIN loads l ON l.id=p.load_record_id
 WHERE p.load_record_id=ANY($1::bigint[]) OR lower(btrim(l.load_id)) IN (SELECT number FROM numbers)
 UNION SELECT 'truck:'||p.truck_id FROM payroll_load_placements p JOIN loads l ON l.id=p.load_record_id
 WHERE p.load_record_id=ANY($1::bigint[]) OR lower(btrim(l.load_id)) IN (SELECT number FROM numbers)
 UNION SELECT s.root FROM (
 SELECT 'driver:'||driver_id root,report FROM payroll_settlements WHERE finalized
 UNION ALL SELECT 'truck:'||truck_id,report FROM investor_pay_weeks WHERE finalized
 ) s CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.report->'loads','[]'::jsonb)) l
 WHERE (l->>'loadRecordId')::bigint=ANY($1::bigint[]) OR lower(btrim(l->>'loadNumber')) IN (SELECT number FROM numbers)
 ) affected WHERE root IS NOT NULL`, ids, numbers)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			return err
		}
		roots[root] = true
	}
	return rows.Err()
}
