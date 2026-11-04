package repository

import (
	"context"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Resolve resource membership once for a publication, rather than querying
// invoices, escrow, schedules and legacy overrides for each statement. Each
// statement still owns its exact period component, not the union of bulk roots.
// Call only after canonical writes; this map never survives that read phase.
func payrollPublicationResources(ctx context.Context, tx pgx.Tx, week string, reports [2]DriverPayWeek) (map[string][]string, error) {
	components := map[string]*payrollSelection{}
	principals := map[string]bool{}
	for i, kind := range []string{"driver", "investor"} {
		prefix := "driver:"
		if i == 1 {
			prefix = "truck:"
		}
		for _, row := range reports[i].Drivers {
			principal := prefix + row.ID
			selection := components[principal]
			if selection == nil {
				read := context.WithValue(ctx, payrollSelectionKey{}, (*payrollSelection)(nil))
				read = context.WithValue(read, payrollSelectionFactsKey{}, true)
				read, err := selectPayrollContexts(payrollWeekContext(read, week), tx, "", []string{principal})
				if err != nil {
					return nil, err
				}
				selection, _ = read.Value(payrollSelectionKey{}).(*payrollSelection)
				if selection == nil {
					return nil, ErrPayrollResourceChanged
				}
				for k, ids := range map[string][]string{"driver": selection.drivers, "truck": selection.trucks, "owner": selection.owners} {
					for _, id := range ids {
						key := k + ":" + id
						components[key], principals[key] = selection, true
					}
				}
			}
			components[kind+":"+row.ID] = selection
		}
	}
	keys := make([]string, 0, len(principals))
	for key := range principals {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return map[string][]string{}, nil
	}
	links, err := payrollPublicationResourceLinks(ctx, tx, keys)
	if err != nil {
		return nil, err
	}
	resolved := map[*payrollSelection][]string{}
	result := map[string][]string{}
	for i, kind := range []string{"driver", "investor"} {
		for _, row := range reports[i].Drivers {
			identity := kind + ":" + row.ID
			component := components[identity]
			shared, ok := resolved[component]
			if !ok {
				set := map[string]bool{}
				for k, ids := range map[string][]string{"driver": component.drivers, "truck": component.trucks, "owner": component.owners} {
					for _, id := range ids {
						principal := k + ":" + id
						set[principal] = true
						for _, key := range links[principal] {
							set[key] = true
						}
					}
				}
				for key := range set {
					shared = append(shared, key)
				}
				sort.Strings(shared)
				resolved[component] = shared
			}
			result[identity] = append(append([]string{}, shared...), "statement:"+identity+":"+week)
		}
	}
	return result, nil
}

func payrollPublicationResourceLinks(ctx context.Context, tx pgx.Tx, keys []string) (map[string][]string, error) {
	sort.Strings(keys)
	cacheKey := strings.Join(keys, ",")
	publication, _ := ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState)
	if publication != nil && publication.tx == tx {
		if links, ok := publication.resourceLinks[cacheKey]; ok {
			return links, nil
		}
	}
	drivers, trucks, owners := []string{}, []string{}, []string{}
	for _, key := range keys {
		kind, id, _ := strings.Cut(key, ":")
		switch kind {
		case "driver":
			drivers = append(drivers, id)
		case "truck":
			trucks = append(trucks, id)
		case "owner":
			owners = append(owners, id)
		}
	}
	// Legacy statement keys intentionally span all periods. Their original
	// frozen bases can affect current carry when no canonical cost receipt exists.
	rows, err := tx.Query(ctx, `WITH principals AS MATERIALIZED (SELECT unnest($1::text[]) key), expense_principals AS MATERIALIZED (
 `+payrollExpensePrincipalsSQL+`
 ), links AS (
 SELECT p.principal key,'expense:'||p.expense_id resource FROM expense_principals p JOIN principals wanted ON wanted.key=p.principal
 UNION SELECT 'driver:'||e.driver_id,'escrow:'||e.id FROM driver_escrows e WHERE e.driver_id=ANY($2::uuid[])
 UNION SELECT 'driver:'||s.driver_id,'schedule:'||s.id FROM driver_charge_schedules s WHERE s.driver_id=ANY($2::uuid[])
 UNION SELECT 'driver:'||w.driver_id,'statement:driver:'||w.driver_id||':'||w.week_start FROM driver_pay_weeks w
 WHERE w.driver_id=ANY($2::uuid[]) AND (w.fuel_override IS NOT NULL OR w.toll_override IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM driver_pay_cost_collections c WHERE c.driver_id=w.driver_id AND c.week_start=w.week_start)
 UNION SELECT 'truck:'||w.truck_id,'statement:investor:'||w.truck_id||':'||w.week_start FROM investor_pay_weeks w
 WHERE w.truck_id=ANY($3::uuid[]) AND (
 (w.edits->>'fuelOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':fuel')) OR
 (w.edits->>'tollOverride' IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pay_source_collections c WHERE c.owner_id=w.owner_id AND c.week_start=w.week_start AND c.source='investor_cost:'||w.truck_id::text||':toll')))
 ) SELECT key,resource FROM links`, keys, drivers, trucks)
	if err != nil {
		return nil, err
	}
	links := map[string][]string{}
	for rows.Next() {
		var principal, resource string
		if err = rows.Scan(&principal, &resource); err != nil {
			rows.Close()
			return nil, err
		}
		links[principal] = append(links[principal], resource)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if publication != nil && publication.tx == tx {
		publication.resourceLinks[cacheKey] = links
	}
	return links, nil
}
