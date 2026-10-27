package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type DashboardCostReview struct {
	ID            string   `json:"id"`
	Reference     string   `json:"reference"`
	Date          string   `json:"date"`
	OwnershipDate *string  `json:"ownershipDate"`
	TruckID       string   `json:"truckId"`
	TruckUnit     string   `json:"truckUnit"`
	DriverName    string   `json:"driverName"`
	AssignedUnits []string `json:"assignedUnits"`
	Amount        *string  `json:"amount"`
	Reason        string   `json:"reason"`
}

// Reuse the waterfall's attribution, period and unresolved predicate so the
// review shows exactly the transactions excluded from its company costs.
const dashboardCostReviewCTE = dashboardCompanyCostsCTE + `, unresolved AS (
 SELECT * FROM attributed WHERE kind=$3 AND
 (owners<>1 OR is_company IS NULL OR amount IS NULL)
)`

func (r *DashboardRepository) DashboardCostReview(ctx context.Context, p DashboardPeriod, kind string, pagination Pagination) (Page[DashboardCostReview], error) {
	var empty Page[DashboardCostReview]
	if kind != "fuel" && kind != "tolls" && kind != "ownership" {
		return empty, fmt.Errorf("choose fuel, tolls or load ownership")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if kind == "ownership" {
		result, err := dashboardLoadOwnershipReview(ctx, tx, p, pagination)
		if err != nil {
			return empty, err
		}
		return result, tx.Commit(ctx)
	}
	var total int
	if err = tx.QueryRow(ctx, dashboardCostReviewCTE+` SELECT count(*) FROM unresolved`, p.DateFrom, p.DateTo, kind).Scan(&total); err != nil {
		return empty, err
	}
	pagination = pagination.Normalize(total)
	rows, err := tx.Query(ctx, dashboardCostReviewCTE+` SELECT c.id,c.reference,c.report_date::text,c.cost_date::text,
 coalesce(c.truck_id::text,''),coalesce(t.unit_number,nullif(c.source_unit,''),'Unknown truck'),
 coalesce(d.full_name,''),coalesce(assignment.units,'{}'::text[]),c.amount::text,
 CASE WHEN c.truck_id IS NULL THEN
  CASE WHEN c.kind='tolls' THEN 'Toll has no linked truck' ELSE 'Reported truck number is missing or not uniquely linked' END
 WHEN c.owners=0 THEN 'No ownership recorded for the transaction date'
 WHEN c.owners>1 THEN 'Conflicting owners on the transaction date'
 WHEN c.is_company IS NULL THEN 'Owner classification is missing'
 ELSE 'Transaction amount is missing' END
 FROM unresolved c LEFT JOIN trucks t ON t.id=c.truck_id
 LEFT JOIN fuel_transactions f ON c.kind='fuel' AND f.id::text=c.id
 LEFT JOIN drivers d ON d.id=f.driver_id
 LEFT JOIN LATERAL (SELECT array_agg(DISTINCT at.unit_number ORDER BY at.unit_number) units
  FROM truck_driver_assignments a JOIN trucks at ON at.id=a.truck_id
  WHERE a.driver_id=f.driver_id AND a.assigned_at<=f.purchased_at
  AND (a.unassigned_at IS NULL OR f.purchased_at<a.unassigned_at)) assignment ON true
 ORDER BY c.report_date,c.id LIMIT $4 OFFSET $5`, p.DateFrom, p.DateTo, kind, pagination.PageSize, pagination.Offset())
	if err != nil {
		return empty, err
	}
	items := []DashboardCostReview{}
	for rows.Next() {
		var item DashboardCostReview
		if err = rows.Scan(&item.ID, &item.Reference, &item.Date, &item.OwnershipDate, &item.TruckID, &item.TruckUnit, &item.DriverName, &item.AssignedUnits, &item.Amount, &item.Reason); err != nil {
			rows.Close()
			return empty, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return NewPage(items, total, pagination), nil
}
