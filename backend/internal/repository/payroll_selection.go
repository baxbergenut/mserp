package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
	"sort"
	"strings"
)

type payrollSelectionKey struct{}
type payrollWeekKey struct{}

func payrollWeekContext(ctx context.Context, week string) context.Context {
	return context.WithValue(ctx, payrollWeekKey{}, week)
}

type payrollSelection struct{ drivers, trucks, owners []string }

func payrollDriverIDs(ctx context.Context) []string {
	if s, ok := ctx.Value(payrollSelectionKey{}).(*payrollSelection); ok {
		return s.drivers
	}
	return nil
}
func payrollTruckIDs(ctx context.Context) []string {
	if s, ok := ctx.Value(payrollSelectionKey{}).(*payrollSelection); ok {
		return s.trucks
	}
	return nil
}
func payrollOwnerIDs(ctx context.Context) []string {
	if s, ok := ctx.Value(payrollSelectionKey{}).(*payrollSelection); ok {
		return s.owners
	}
	return nil
}
func payrollIncludesTruck(ctx context.Context, id string) bool {
	ids := payrollTruckIDs(ctx)
	if ids == nil {
		return true
	}
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// Select a conservative connected component, rather than the current assignment.
// Cost edges use the requested week plus uncollected, unfrozen legacy weeks.
// Historical owners, teams, native assignments, fuel identities and saved
// destinations can affect the same principal. Including the complete component
// keeps corrections across weeks equivalent to a full-fleet calculation.
func selectPayrollContext(ctx context.Context, tx pgx.Tx, kind, id string) (context.Context, error) {
	if id == "" {
		return ctx, nil
	}
	return selectPayrollContexts(ctx, tx, kind, []string{id})
}
func selectPayrollContexts(ctx context.Context, tx pgx.Tx, kind string, ids []string) (context.Context, error) {
	if full, _ := ctx.Value(payrollAuthoritativeKey{}).(bool); full {
		return ctx, nil
	}
	if ctx.Value(payrollSelectionKey{}) != nil {
		return ctx, nil
	}
	// Unknown schema dependencies cannot prove a complete component. Retain the
	// full calculation until their revision/relationship contract is registered.
	snapshot, err := payrollCacheSnapshot(ctx, tx)
	if err != nil {
		return ctx, err
	}
	if strings.HasPrefix(snapshot, "snapshot:") {
		return ctx, nil
	}
	defer db.Phase(ctx, "selection")()
	if _, err := tx.Exec(ctx, `SET LOCAL plan_cache_mode = force_custom_plan; SET LOCAL enable_nestloop = on`); err != nil {
		return ctx, err
	}
	week, _ := ctx.Value(payrollWeekKey{}).(string)
	roots := make([]string, len(ids))
	for i, id := range ids {
		roots[i] = kind + ":" + id
	}
	rows, err := tx.Query(ctx, `WITH cost_weeks AS MATERIALIZED (
 SELECT nullif($1,'')::date week
 UNION SELECT w.week_start FROM driver_pay_weeks w WHERE accounting_live_date(w.week_start) AND (w.fuel_override IS NOT NULL OR w.toll_override IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM driver_pay_cost_collections c WHERE c.driver_id=w.driver_id AND c.week_start=w.week_start)
 AND NOT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.driver_id=w.driver_id AND s.week_start=w.week_start AND s.finalized)
 UNION SELECT w.week_start FROM investor_pay_weeks w WHERE accounting_live_date(w.week_start) AND NOT w.finalized AND (
 (w.edits->>'fuelOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':fuel')) OR
 (w.edits->>'tollOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':toll')))
 ), source_weeks AS MATERIALIZED (
 SELECT week FROM cost_weeks UNION SELECT week-7 FROM cost_weeks
 UNION SELECT date_trunc('week',p.source_date)::date FROM payroll_load_placements p JOIN cost_weeks w ON w.week=p.week_start
 ), relevant_sources AS MATERIALIZED (
 SELECT * FROM pay_source_collections WHERE accounting_live_date(week_start) AND (week_start>=nullif($1,'')::date OR $1='')
 UNION ALL
 SELECT * FROM (SELECT DISTINCT ON(c.source,c.owner_id) c.* FROM pay_source_collections c
 JOIN (SELECT source,owner_id FROM pay_source_collections WHERE accounting_live_date(week_start) AND week_start<nullif($1,'')::date
 GROUP BY source,owner_id HAVING sum(base-amount)<>0) debt USING(source,owner_id)
 WHERE accounting_live_date(c.week_start) AND c.week_start<nullif($1,'')::date ORDER BY c.source,c.owner_id,c.week_start DESC) older_debt
 ), edges(a,b) AS MATERIALIZED (
 SELECT 'driver:'||driver_id,'truck:'||truck_id FROM truck_driver_assignments a WHERE $1='' OR EXISTS(
 SELECT 1 FROM source_weeks w WHERE (a.assigned_at AT TIME ZONE 'America/New_York')::date<w.week+7
 AND (a.unassigned_at IS NULL OR (a.unassigned_at AT TIME ZONE 'America/New_York')::date>w.week))
 UNION SELECT 'driver:'||main_driver_id,'driver:'||team_driver_id FROM driver_teams t WHERE $1='' OR EXISTS(
 SELECT 1 FROM source_weeks w WHERE t.start_week<=w.week AND (t.end_week IS NULL OR t.end_week>=w.week))
 UNION SELECT 'owner:'||id,'driver:'||driver_id FROM investors WHERE driver_id IS NOT NULL
 UNION SELECT 'truck:'||id,'owner:'||owner_id FROM trucks WHERE owner_id IS NOT NULL
 UNION SELECT 'truck:'||truck_id,'owner:'||owner_id FROM truck_ownership_history h WHERE $1='' OR EXISTS(
 SELECT 1 FROM source_weeks w WHERE (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<w.week+7)
 AND (h.unassigned_at IS NULL OR (h.unassigned_at AT TIME ZONE 'America/New_York')::date>w.week))
 UNION SELECT 'truck:'||truck_id,'owner:'||owner_id FROM truck_settlement_terms t WHERE $1='' OR EXISTS(
 SELECT 1 FROM source_weeks w WHERE t.week_start=(SELECT max(prior.week_start) FROM truck_settlement_terms prior WHERE prior.truck_id=t.truck_id AND prior.week_start<=w.week))
 UNION SELECT 'truck:'||truck_id,'owner:'||owner_id FROM investor_pay_weeks WHERE $1='' OR week_start IN(SELECT week FROM source_weeks)
 UNION SELECT 'driver:'||charge_driver_id,'truck:'||truck_id FROM expenses WHERE charge_driver_id IS NOT NULL AND truck_id IS NOT NULL
 UNION SELECT 'driver:'||driver_id,'truck:'||truck_id FROM weighmytruck_transactions t WHERE driver_id IS NOT NULL AND truck_id IS NOT NULL
 AND ($1='' OR EXISTS(SELECT 1 FROM source_weeks w WHERE t.transaction_date>=w.week AND t.transaction_date<w.week+7))
 UNION SELECT 'driver:'||driver_id,'truck:'||truck_id FROM payroll_load_placements WHERE truck_id IS NOT NULL
 UNION SELECT 'driver:'||f.driver_id,'truck:'||a.truck_id FROM (SELECT f.driver_id,f.reported_truck_units FROM source_weeks w JOIN fuel_transactions f ON f.purchased_on>=w.week AND f.purchased_on<w.week+7 WHERE f.driver_id IS NOT NULL AND $1<>''
 UNION SELECT driver_id,reported_truck_units FROM fuel_transactions WHERE driver_id IS NOT NULL AND $1='') f CROSS JOIN LATERAL unnest(f.reported_truck_units) unit(key) JOIN truck_unit_aliases a ON a.unit_key=unit.key
 UNION SELECT CASE kind WHEN 'driver' THEN 'driver:' ELSE 'truck:' END||statement_id,'owner:'||owner_id FROM relevant_sources
 UNION SELECT 'truck:'||truck_id,'owner:'||owner_id FROM relevant_sources
 ) SELECT a,b FROM edges WHERE a IS NOT NULL AND b IS NOT NULL`, week)
	if err != nil {
		return ctx, err
	}
	defer rows.Close()
	// The actual relationship graph is small. Traversing its distinct edges here
	// avoids PostgreSQL reserving a recursive hash table for millions of estimated
	// nodes when a component contains only a handful of identities.
	links := map[string][]string{}
	for rows.Next() {
		var a, b string
		if err = rows.Scan(&a, &b); err != nil {
			return ctx, err
		}
		links[a] = append(links[a], b)
		links[b] = append(links[b], a)
	}
	if err = rows.Err(); err != nil {
		return ctx, err
	}
	seen := map[string]bool{}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if seen[node] {
			continue
		}
		seen[node] = true
		queue = append(queue, links[node]...)
	}
	nodes := make([]string, 0, len(seen))
	for node := range seen {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	s := &payrollSelection{drivers: []string{}, trucks: []string{}, owners: []string{}}
	for _, node := range nodes {
		k, v, _ := strings.Cut(node, ":")
		switch k {
		case "driver":
			s.drivers = append(s.drivers, v)
		case "truck":
			s.trucks = append(s.trucks, v)
		case "owner":
			s.owners = append(s.owners, v)
		}
	}
	return context.WithValue(ctx, payrollSelectionKey{}, s), nil
}

func payrollContextWeek(ctx context.Context) string {
	w, _ := ctx.Value(payrollWeekKey{}).(string)
	return w
}
