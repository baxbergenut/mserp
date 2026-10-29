package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// The count and drilldown share load identity, dated ownership and the exact
// unresolved predicate. Only Clustie's dated assignments determine the truck.
const dashboardLoadOwnershipCTE = companyDashboardCTE + `, ownership AS (
 SELECT c.*,assignment.truck_id,
 assignment.units assigned_units,
 o.owners,i.is_company,i.driver_id owner_driver,term.owner_id term_owner,o.owner_id
 FROM dated c
 LEFT JOIN LATERAL (
  SELECT (array_agg(DISTINCT a.truck_id))[1] truck_id,array_agg(DISTINCT t.unit_number ORDER BY t.unit_number) units
  FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id
  WHERE a.driver_id=c.driver_id AND a.assigned_at<((c.service_date+1)::timestamp AT TIME ZONE 'America/New_York')
  AND (a.unassigned_at IS NULL OR a.unassigned_at>=((c.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
  HAVING count(DISTINCT a.truck_id)=1
 ) assignment ON true
 LEFT JOIN LATERAL (
  SELECT count(DISTINCT h.owner_id) owners,(array_agg(DISTINCT h.owner_id))[1] owner_id
  FROM truck_ownership_history h WHERE h.truck_id=assignment.truck_id
  AND (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<=c.service_date)
  AND (h.unassigned_at IS NULL OR c.service_date<(h.unassigned_at AT TIME ZONE 'America/New_York')::date)
 ) o ON true LEFT JOIN investors i ON i.id=o.owner_id
 LEFT JOIN LATERAL (SELECT owner_id FROM truck_settlement_terms s WHERE s.truck_id=assignment.truck_id
 AND s.week_start<=$1::date ORDER BY s.week_start DESC LIMIT 1) term ON true
 WHERE c.inclusion='included'
), unresolved_ownership AS (
 SELECT * FROM ownership WHERE owners<>1 OR is_company IS NULL
 OR (NOT is_company AND owner_driver IS DISTINCT FROM payroll_driver_at(driver_id,service_date) AND term_owner IS DISTINCT FROM owner_id)
) `

func dashboardLoadOwnershipReview(ctx context.Context, tx pgx.Tx, p DashboardPeriod, pagination Pagination) (Page[DashboardCostReview], error) {
	var empty Page[DashboardCostReview]
	var total int
	if err := tx.QueryRow(ctx, dashboardLoadOwnershipCTE+` SELECT count(*) FROM unresolved_ownership`, p.DateFrom, p.DateTo).Scan(&total); err != nil {
		return empty, err
	}
	pagination = pagination.Normalize(total)
	rows, err := tx.Query(ctx, dashboardLoadOwnershipCTE+` SELECT
 c.driver_id::text||':'||c.service_date::text||':'||c.slot::text,c.load_number,c.service_date::text,c.service_date::text,
 coalesce(t.id::text,''),coalesce(t.unit_number,'Unknown truck'),c.driver_name,
 coalesce(c.assigned_units,'{}'::text[]),c.gross::text,
 CASE WHEN c.truck_id IS NULL THEN 'Clustie truck assignment is missing or conflicting on the load date'
 WHEN c.owners=0 THEN 'No ownership recorded for the load date'
 WHEN c.owners>1 THEN 'Conflicting owners on the load date'
 WHEN c.is_company IS NULL THEN 'Owner classification is missing'
 ELSE 'Investor settlement terms are missing or name a different owner' END
 FROM unresolved_ownership c LEFT JOIN trucks t ON t.id=c.truck_id
 ORDER BY c.service_date,c.driver_name,c.driver_id,c.slot LIMIT $3 OFFSET $4`, p.DateFrom, p.DateTo, pagination.PageSize, pagination.Offset())
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	items := []DashboardCostReview{}
	for rows.Next() {
		var item DashboardCostReview
		if err = rows.Scan(&item.ID, &item.Reference, &item.Date, &item.OwnershipDate, &item.TruckID, &item.TruckUnit, &item.DriverName, &item.AssignedUnits, &item.Amount, &item.Reason); err != nil {
			return empty, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return empty, err
	}
	return NewPage(items, total, pagination), nil
}
