package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type payrollNormalizedFactsKey struct{}

func payrollNormalizedInputs(ctx context.Context) bool {
	if full, _ := ctx.Value(payrollAuthoritativeKey{}).(bool); full {
		return false
	}
	prepared, _ := ctx.Value(payrollNormalizedFactsKey{}).(bool)
	return prepared
}

// Month buckets are safe only when the entire month is older than this
// statement and satisfies its cost boundary. All other ledger rows stay exact.
const payrollCostLedgerBucketsSQL = `(SELECT driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount FROM driver_pay_cost_collections
 WHERE week_start>=date_trunc('month',nullif($2,'')::date)::date OR $2='' OR $3::boolean
 OR NOT payroll_cost_in_scope(date_trunc('month',week_start)::date,nullif($2,'')::date)
 UNION ALL SELECT driver_id,month_start,fuel_base,toll_base,fuel_amount,toll_amount FROM payroll_cost_months
 WHERE row_count>0 AND month_start<date_trunc('month',nullif($2,'')::date)::date AND NOT $3::boolean
 AND payroll_cost_in_scope(month_start,nullif($2,'')::date))`

const payrollSourceLedgerBucketsSQL = `(SELECT source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,version,statement FROM pay_source_collections
 WHERE week_start>=date_trunc('month',nullif($2,'')::date)::date OR $2='' OR $3::boolean
 OR (source LIKE 'investor_cost:%' AND NOT payroll_cost_in_scope(date_trunc('month',week_start)::date,nullif($2,'')::date))
 UNION ALL SELECT b.source,b.owner_id,last.week_start,last.kind,last.statement_id,last.truck_id,last.name,b.base,b.amount,b.version_sum,last.statement
 FROM payroll_source_months b JOIN LATERAL (SELECT week_start,kind,statement_id,truck_id,name,statement FROM pay_source_collections c
 WHERE c.owner_id=b.owner_id AND c.source=b.source AND c.week_start>=b.month_start AND c.week_start<b.month_start+interval '1 month'
 ORDER BY c.week_start DESC LIMIT 1) last ON true
 WHERE b.row_count>0 AND b.month_start<date_trunc('month',nullif($2,'')::date)::date AND NOT $3::boolean
 AND (b.source NOT LIKE 'investor_cost:%' OR payroll_cost_in_scope(b.month_start,nullif($2,'')::date)))`

// Only inputs consumed by trip validation participate in this content revision.
// Identity is the source record, never its nonunique displayed load number.
const payrollLoadFactHashSQL = `md5(jsonb_build_object('gross',l.total_pay,'miles',l.total_miles,
 'trips',l.raw_payload->'trips','allocation',to_jsonb(l)->'split_allocation')::text)`

// Facts are reusable across weeks and processes. New or corrected sources create
// a distinct immutable fact; timestamp-only syncs reuse the existing version.
func preparePayrollLoadFacts(ctx context.Context, tx pgx.Tx, changed ...[]int64) error {
	var ids []int64
	if len(changed) > 0 {
		ids = changed[0]
	}
	_, err := tx.Exec(ctx, payrollPrepareLoadFactsSQL, payrollCalculatorVersion, ids)
	return err
}

// The cohort marker certifies a full scan at one committed loads revision. It
// is created in the same SQL snapshot and transaction as the immutable facts.
// Partial preparations never certify a complete cohort. Missing revision
// coverage forces a scan, and the consumer still checks each content hash.
const payrollPrepareLoadFactsSQL = `WITH revision AS MATERIALIZED (
 SELECT revision::text value FROM payroll_cache_revisions WHERE table_name='loads'
 ), complete AS MATERIALIZED (
 SELECT EXISTS(SELECT 1 FROM payroll_routing_facts f JOIN revision r ON f.input_fingerprint=r.value
 WHERE f.kind='load_cohort' AND f.week_start='1970-01-05' AND f.entity_id='all' AND f.calculator_version=$1) yes
 ), needed AS MATERIALIZED (
 SELECT l.*,` + payrollLoadFactHashSQL + ` fact_hash FROM loads l
 WHERE NOT (SELECT yes FROM complete) AND ($2::bigint[] IS NULL OR l.id=ANY($2::bigint[])) AND NOT EXISTS(
 SELECT 1 FROM payroll_load_facts f WHERE f.load_id=l.id AND f.source_hash=` + payrollLoadFactHashSQL + ` AND f.calculator_version=$1)
 ), facts AS (
 INSERT INTO payroll_load_facts(load_id,source_hash,calculator_version,trip_count,trips_valid,trip_allocation,trip_values)
 SELECT l.id,l.fact_hash,$1,facts.n,facts.valid,facts.saved,facts.trips FROM needed l CROSS JOIN LATERAL (
 ` + loadTripValidationSQL + `
 SELECT stats.n,checked.valid,checked.saved,
 (SELECT jsonb_object_agg(id::text,jsonb_build_object('v',v,'gross',gross,'loaded',loaded,'empty',empty)) FROM trips WHERE id IS NOT NULL) trips
 FROM stats,checked) facts ON CONFLICT DO NOTHING RETURNING load_id
 ) INSERT INTO payroll_routing_facts(kind,week_start,calculator_version,input_fingerprint,entity_id,payload)
 SELECT 'load_cohort','1970-01-05'::date,$1,r.value,'all','{}'::jsonb FROM revision r CROSS JOIN (SELECT count(*) FROM facts) inserted
 WHERE $2::bigint[] IS NULL AND NOT (SELECT yes FROM complete) ON CONFLICT DO NOTHING`

var payrollPreparedEntriesCTE = payrollLoadMatchCTE + `, pay_loaded AS MATERIALIZED (
 SELECT l.*,facts.trip_count,facts.trips_valid,facts.trip_allocation,facts.trip_values
 FROM loads l JOIN (SELECT DISTINCT coalesce(e.load_record_id,n.id) id
 FROM pay_entries e LEFT JOIN pay_load_numbers n ON n.number=lower(btrim(e.load_number)) WHERE e.day_status='') ids ON ids.id=l.id
 JOIN payroll_load_facts facts ON facts.load_id=l.id AND facts.source_hash=` + payrollLoadFactHashSQL + `
 AND facts.calculator_version='` + payrollCalculatorVersion + `'
)`
