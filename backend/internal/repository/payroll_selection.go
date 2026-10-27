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
// Source edges use the requested week plus uncollected, unfrozen legacy weeks.
// Historical owners, teams, ambiguous load candidates, fuel identities and saved
// destinations can affect the same principal. Including the complete component
// keeps corrections across weeks equivalent to a full-fleet calculation.
func selectPayrollContext(ctx context.Context, tx pgx.Tx, kind, id string) (context.Context, error) {
	if id == "" {
		return ctx, nil
	}
	return selectPayrollContexts(ctx, tx, kind, []string{id})
}
func selectPayrollContexts(ctx context.Context, tx pgx.Tx, kind string, ids []string) (context.Context, error) {
	if ctx.Value(payrollSelectionKey{}) != nil {
		return ctx, nil
	}
	defer db.Phase(ctx, "selection")()
	if err := payrollQueryPlans(ctx, tx); err != nil {
		return ctx, err
	}
	week, _ := ctx.Value(payrollWeekKey{}).(string)
	roots := make([]string, len(ids))
	for i, id := range ids {
		roots[i] = kind + ":" + id
	}
	rows, err := tx.Query(ctx, `WITH source_weeks AS MATERIALIZED (
 SELECT nullif($1,'')::date week
 UNION SELECT w.week_start FROM driver_pay_weeks w WHERE (w.fuel_override IS NOT NULL OR w.toll_override IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM driver_pay_cost_collections c WHERE c.driver_id=w.driver_id AND c.week_start=w.week_start)
 AND NOT EXISTS(SELECT 1 FROM payroll_settlements s WHERE s.driver_id=w.driver_id AND s.week_start=w.week_start AND s.finalized)
 UNION SELECT w.week_start FROM investor_pay_weeks w WHERE NOT w.finalized AND (
 (w.edits->>'fuelOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':fuel')) OR
 (w.edits->>'tollOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':toll')))
 ), board_entries AS MATERIALIZED (
 SELECT e.driver_id,e.load_record_id,e.load_trip_id,lower(btrim(e.load_number)) number FROM source_weeks w JOIN `+grossBoardEntriesSQL+` e ON e.service_date>=w.week AND e.service_date<w.week+7 WHERE $1<>''
 UNION SELECT driver_id,load_record_id,load_trip_id,lower(btrim(load_number)) number FROM `+grossBoardEntriesSQL+` e WHERE $1=''
 ), board_links AS MATERIALIZED (
 SELECT e.driver_id,CASE WHEN e.load_trip_id IS NULL THEN l.truck_id END truck_id,CASE WHEN e.load_trip_id IS NULL THEN l.truck_unit ELSE trip_source.selected_trip->>'truck__unit_number' END truck_unit FROM board_entries e JOIN loads l ON l.id=e.load_record_id `+loadTripSourceSQL+`
 UNION SELECT e.driver_id,CASE WHEN e.load_trip_id IS NULL THEN l.truck_id END,CASE WHEN e.load_trip_id IS NULL THEN l.truck_unit ELSE trip_source.selected_trip->>'truck__unit_number' END FROM board_entries e JOIN loads l ON lower(btrim(l.load_id))=e.number `+loadTripSourceSQL+` WHERE e.load_record_id IS NULL AND e.number<>''
 ), edges(a,b) AS MATERIALIZED (
 SELECT 'driver:'||driver_id,'truck:'||truck_id FROM truck_driver_assignments
 UNION SELECT 'driver:'||main_driver_id,'driver:'||team_driver_id FROM driver_teams
 UNION SELECT 'owner:'||id,'driver:'||driver_id FROM investors WHERE driver_id IS NOT NULL
 UNION SELECT 'truck:'||id,'owner:'||owner_id FROM trucks WHERE owner_id IS NOT NULL
 UNION SELECT 'truck:'||truck_id,'owner:'||owner_id FROM truck_ownership_history
 UNION SELECT 'truck:'||truck_id,'owner:'||owner_id FROM truck_settlement_terms
 UNION SELECT 'truck:'||truck_id,'owner:'||owner_id FROM investor_pay_weeks
 UNION SELECT 'driver:'||charge_driver_id,'truck:'||truck_id FROM expenses WHERE charge_driver_id IS NOT NULL AND truck_id IS NOT NULL
 UNION SELECT 'driver:'||driver_id,'truck:'||truck_id FROM board_links WHERE truck_id IS NOT NULL
 UNION SELECT 'driver:'||l.driver_id,'truck:'||a.truck_id FROM board_links l JOIN truck_unit_aliases a ON a.unit_key=upper(btrim(l.truck_unit)) WHERE l.truck_id IS NULL
 UNION SELECT 'driver:'||driver_id,'truck:'||truck_id FROM weighmytruck_transactions WHERE driver_id IS NOT NULL AND truck_id IS NOT NULL
 UNION SELECT 'driver:'||f.driver_id,'truck:'||a.truck_id FROM (SELECT f.driver_id,f.reported_truck_units FROM source_weeks w JOIN fuel_transactions f ON f.purchased_on>=w.week AND f.purchased_on<w.week+7 WHERE f.driver_id IS NOT NULL AND $1<>''
 UNION SELECT driver_id,reported_truck_units FROM fuel_transactions WHERE driver_id IS NOT NULL AND $1='') f CROSS JOIN LATERAL unnest(f.reported_truck_units) unit(key) JOIN truck_unit_aliases a ON a.unit_key=unit.key
 UNION SELECT CASE kind WHEN 'driver' THEN 'driver:' ELSE 'truck:' END||statement_id,'owner:'||owner_id FROM pay_source_collections
 UNION SELECT 'truck:'||truck_id,'owner:'||owner_id FROM pay_source_collections
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
