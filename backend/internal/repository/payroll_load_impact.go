package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Include every native destination for both captured numbers and exact IDs.
// Number-only board matches can change when another record acquires the same
// number. Transfers and manual placements can use a week
// different from the original board date. Broker expenses also follow the saved
// load numbers in frozen statements.
func payrollLoadImpact(ctx context.Context, tx pgx.Tx, ids []int64, numbers, brokerNumbers []string, roots map[string]bool) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `WITH numbers AS (
 SELECT unnest($2::text[]) number UNION SELECT lower(btrim(load_id)) FROM loads WHERE id=ANY($1::bigint[])
 ), matching_placements AS (
 SELECT p.* FROM payroll_load_placements p JOIN loads l ON l.id=p.load_record_id
 WHERE p.load_record_id=ANY($1::bigint[]) OR lower(btrim(l.load_id)) IN (SELECT number FROM numbers)
 ) SELECT DISTINCT root,date_trunc('week',day)::date::text FROM (
 SELECT r.root,d.day FROM `+grossBoardEntriesSQL+` e
 LEFT JOIN gross_board_transfers t ON t.driver_id=e.driver_id AND t.source_date=e.service_date AND t.source_slot=e.slot AND t.active
 CROSS JOIN LATERAL (VALUES ('driver:'||e.driver_id),('truck:'||t.truck_id)) r(root)
 CROSS JOIN LATERAL (VALUES (e.service_date),(t.settlement_date)) d(day)
 WHERE e.load_record_id=ANY($1::bigint[]) OR lower(btrim(e.load_number)) IN (SELECT number FROM numbers)
 UNION ALL
 SELECT r.root,p.week_start FROM matching_placements p
 CROSS JOIN LATERAL (VALUES ('driver:'||p.driver_id),('truck:'||p.truck_id)) r(root)
 UNION ALL
 SELECT r.root,(v.image->>'week_start')::date FROM payroll_load_placement_history h
 CROSS JOIN LATERAL (VALUES (h.before_value),(h.after_value)) v(image)
 CROSS JOIN LATERAL (VALUES ('driver:'||(v.image->>'driver_id')),('truck:'||(v.image->>'truck_id'))) r(root)
 WHERE (v.image->>'load_record_id')::bigint=ANY($1::bigint[])
 OR (v.image->>'load_record_id')::bigint IN (SELECT load_record_id FROM matching_placements)
 UNION ALL
 SELECT s.root,s.week_start FROM (
 SELECT 'driver:'||driver_id root,week_start,report FROM payroll_settlements WHERE finalized AND cardinality($3::text[]) > 0
 UNION ALL SELECT 'truck:'||truck_id,week_start,report FROM investor_pay_weeks WHERE finalized AND cardinality($3::text[]) > 0
 ) s CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.report->'loads','[]'::jsonb)) l
 WHERE lower(btrim(l->>'loadNumber'))=ANY($3::text[])
 ) affected WHERE root IS NOT NULL AND day IS NOT NULL`, ids, numbers, brokerNumbers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	weeks := map[string]bool{}
	for rows.Next() {
		var root, week string
		if err := rows.Scan(&root, &week); err != nil {
			return nil, err
		}
		roots[root] = true
		weeks[week] = true
	}
	return weeks, rows.Err()
}
