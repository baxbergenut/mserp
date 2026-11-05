package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
)

// Cross-week commands use the same fenced source phase for all periods. Read
// their complete graphs together, then traverse each period independently. A
// union across dates would incorrectly connect old and current coworkers.
func payrollSelectionBatchContext(ctx context.Context, tx pgx.Tx, weeks []string) (context.Context, error) {
	if len(weeks) < 2 {
		return ctx, nil
	}
	defer db.Phase(ctx, "selection_batch")()
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return ctx, err
	}
	if strings.HasPrefix(snapshot, "snapshot:") {
		return ctx, nil
	}
	fingerprint, err := payrollFuelSelectionFingerprint(ctx, snapshot)
	if err != nil {
		return ctx, err
	}
	// Invoice and principal links are independent of the period. Materialize
	// them once, keeping dated assignments and balance destinations per period.
	query := payrollSelectionPeriodsSQL
	planMode := "force_generic_plan"
	for _, week := range weeks {
		if week == "" {
			planMode = "force_custom_plan"
			// The all-history scope is reserved for lock discovery. Preserve its
			// independent conservative query; publication uses dated periods.
			inner := strings.ReplaceAll(payrollSelectionEdgesWithSharedSQL(payrollPreparedFuelSelectionSQL, `SELECT a,b FROM payroll_batch_shared_edges`), "$1", "period.week")
			query = `WITH payroll_batch_shared_edges(a,b) AS MATERIALIZED (` + payrollSelectionSharedEdgesSQL + `)
 SELECT ARRAY[period.week],edge.a,edge.b FROM unnest($1::text[]) period(week)
 CROSS JOIN LATERAL (` + inner + `) edge`
			break
		}
	}
	// This is always a fleet-wide relationship query, independently of the
	// selected financial component. A reusable plan avoids repeatedly planning
	// its many branches and the poor large-array custom nested-join plan.
	if _, err = tx.Exec(ctx, `SET LOCAL jit=off; SET LOCAL enable_nestloop=on; SET LOCAL plan_cache_mode=`+planMode); err != nil {
		return ctx, err
	}
	rows, err := tx.Query(ctx, query, weeks, fingerprint, payrollCalculatorVersion)
	if err != nil {
		return ctx, err
	}
	defer rows.Close()
	memo := &payrollSelectionGraphMemo{graphs: map[string]*payrollSelectionGraph{}}
	for _, week := range weeks {
		memo.graphs[week] = &payrollSelectionGraph{tx: tx, week: week, links: map[string][]string{}}
	}
	for rows.Next() {
		var dates []string
		var a, b string
		if err = rows.Scan(&dates, &a, &b); err != nil {
			return ctx, err
		}
		for _, date := range dates {
			graph := memo.graphs[date]
			graph.links[a] = append(graph.links[a], b)
			graph.links[b] = append(graph.links[b], a)
		}
	}
	if err = rows.Err(); err != nil {
		return ctx, err
	}
	rows.Close()
	if _, err = tx.Exec(ctx, `SET LOCAL plan_cache_mode=force_custom_plan`); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, payrollSelectionGraphKey{}, memo), nil
}

// Resolve each dated source week once, then attach it to the periods that need
// it. Legacy overrides can add remote source weeks, but must never merge the
// financial destination graphs of distinct report periods.
const payrollSelectionPeriodsSQL = `WITH company_label AS MATERIALIZED (` + payrollPeriodCompanyOwnershipLabelSQL + `),
 periods AS MATERIALIZED (SELECT unnest($1::date[]) week),
 legacy_weeks AS MATERIALIZED (
 SELECT w.week_start week FROM driver_pay_weeks w WHERE accounting_live_date(w.week_start) AND (w.fuel_override IS NOT NULL OR w.toll_override IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM driver_pay_cost_collections c WHERE c.driver_id=w.driver_id AND c.week_start=w.week_start)
 AND NOT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.driver_id=w.driver_id AND s.week_start=w.week_start AND s.finalized)
 UNION SELECT w.week_start FROM investor_pay_weeks w WHERE accounting_live_date(w.week_start) AND NOT w.finalized AND (
 (w.edits->>'fuelOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':fuel')) OR
 (w.edits->>'tollOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':toll')))
 ), cost_weeks AS MATERIALIZED (
 SELECT week period,week FROM periods UNION SELECT p.week,l.week FROM periods p CROSS JOIN legacy_weeks l
 ), source_weeks AS MATERIALIZED (
 SELECT period,week FROM cost_weeks UNION SELECT period,week-7 FROM cost_weeks
 UNION SELECT w.period,date_trunc('week',p.source_date)::date FROM payroll_load_placements p JOIN cost_weeks w ON w.week=p.week_start
 ), dates AS MATERIALIZED (SELECT DISTINCT week FROM source_weeks),
 terms AS MATERIALIZED (SELECT truck_id,owner_id,week_start,lead(week_start) OVER(PARTITION BY truck_id ORDER BY week_start) until_week FROM truck_settlement_terms),
 fuel_complete AS MATERIALIZED (SELECT EXISTS(SELECT 1 FROM payroll_routing_facts WHERE kind='selection_fuel' AND week_start='1970-01-05' AND entity_id='all' AND calculator_version=$3 AND input_fingerprint=$2) yes),
 source_edges(week,a,b) AS MATERIALIZED (
 SELECT w.week,('driver:'||a.driver_id) COLLATE "C",('truck:'||a.truck_id) COLLATE "C" FROM dates w JOIN truck_driver_assignments a
 ON (a.assigned_at AT TIME ZONE 'America/New_York')::date<w.week+7 AND (a.unassigned_at IS NULL OR (a.unassigned_at AT TIME ZONE 'America/New_York')::date>w.week)
 UNION SELECT w.week,'driver:'||t.main_driver_id,'driver:'||t.team_driver_id FROM dates w JOIN driver_teams t ON t.start_week<=w.week AND (t.end_week IS NULL OR t.end_week>=w.week)
 UNION SELECT w.week,'truck:'||h.truck_id,'owner:'||h.owner_id FROM dates w JOIN truck_ownership_history h
 ON (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<w.week+7) AND (h.unassigned_at IS NULL OR (h.unassigned_at AT TIME ZONE 'America/New_York')::date>w.week)
 UNION SELECT w.week,'truck:'||t.truck_id,'owner:'||t.owner_id FROM dates w JOIN terms t ON t.week_start<=w.week AND (t.until_week IS NULL OR t.until_week>w.week)
 UNION SELECT w.week,'truck:'||t.truck_id,'owner:'||t.owner_id FROM dates w JOIN investor_pay_weeks t ON t.week_start=w.week
 UNION SELECT f.week_start,'driver:'||e.driver,'truck:'||e.truck FROM dates w JOIN payroll_routing_facts f ON f.week_start=w.week
 CROSS JOIN LATERAL jsonb_to_recordset(f.payload) e(driver text,truck text)
 WHERE f.kind='selection_fuel' AND f.entity_id='' AND f.calculator_version=$3 AND f.input_fingerprint=$2 AND (SELECT yes FROM fuel_complete)
 UNION SELECT w.week,'driver:'||f.driver_id,'truck:'||a.truck_id FROM dates w JOIN fuel_transactions f ON f.purchased_on>=w.week AND f.purchased_on<w.week+7
 CROSS JOIN LATERAL unnest(f.reported_truck_units) unit(key) JOIN truck_unit_aliases a ON a.unit_key=unit.key
 WHERE f.driver_id IS NOT NULL AND NOT (SELECT yes FROM fuel_complete)
 ), shared_edges(a,b) AS MATERIALIZED (SELECT * FROM (` + payrollPeriodSharedEdgesSQL + `) expense_edges UNION ` + payrollScaleEdgesSQL + `),
 relevant_sources AS MATERIALIZED (
 SELECT p.week period,c.* FROM periods p JOIN pay_source_collections c ON c.week_start>=p.week WHERE accounting_live_date(c.week_start)
 UNION ALL
 SELECT p.week,prior.* FROM periods p CROSS JOIN LATERAL (
 SELECT DISTINCT ON(c.source,c.owner_id) c.* FROM pay_source_collections c
 JOIN (SELECT source,owner_id FROM pay_source_collections WHERE accounting_live_date(week_start) AND week_start<p.week
 GROUP BY source,owner_id HAVING sum(base-amount)<>0) debt USING(source,owner_id)
 WHERE accounting_live_date(c.week_start) AND c.week_start<p.week ORDER BY c.source,c.owner_id,c.week_start DESC) prior
 ), edges(period,a,b) AS (
 SELECT w.period,e.a,e.b FROM source_weeks w JOIN source_edges e USING(week)
 UNION SELECT p.week,e.a,e.b FROM periods p CROSS JOIN shared_edges e
 UNION SELECT period,CASE kind WHEN 'driver' THEN 'driver:' ELSE 'truck:' END||statement_id,'owner:'||owner_id FROM relevant_sources
 UNION SELECT period,'truck:'||truck_id,'owner:'||owner_id FROM relevant_sources
 ) SELECT array_agg(period ORDER BY period)::text[],a COLLATE "C",b COLLATE "C" FROM edges WHERE a IS NOT NULL AND b IS NOT NULL` +
	payrollSkipCompanyLabelSQL + ` GROUP BY a COLLATE "C",b COLLATE "C"`
