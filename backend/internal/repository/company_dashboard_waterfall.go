package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// This is an earned operating margin, not cash settlement or net company income.
// Owner-paid fuel/tolls are absent from costs, so their recovery deductions must
// also be absent from earnings. Carry, escrow and recurring collections are not
// earnings. Hired-driver earnings deducted from investor shares count only once.
type DashboardWaterfall struct {
	Gross       string          `json:"gross"`
	Fuel        string          `json:"fuel"`
	Tolls       string          `json:"tolls"`
	DriverPay   string          `json:"driverPay"`
	InvestorPay string          `json:"investorPay"`
	Profit      *string         `json:"profit"`
	Review      []DashboardItem `json:"review"`
}

// start_known=false denotes an imported baseline with an open start, as in
// payroll ownership classification. Its recorded end still bounds the owner;
// missing or conflicting owners remain unresolved.
const dashboardCompanyCostsCTE = `WITH aliases AS (
 SELECT unit_key,(array_agg(DISTINCT truck_id))[1] truck_id FROM truck_unit_aliases
 GROUP BY unit_key HAVING count(DISTINCT truck_id)=1
), costs AS (
 SELECT 'fuel' kind,f.id::text id,f.purchased_on cost_date,a.truck_id,sum(i.total_amount_paid) amount,
 f.purchased_on report_date,array_to_string(f.reported_truck_units,', ') source_unit,f.relay_transaction_id reference
 FROM fuel_transactions f JOIN fuel_transaction_items i ON i.fuel_transaction_id=f.id
 LEFT JOIN aliases a ON cardinality(f.reported_truck_units)=1 AND a.unit_key=f.reported_truck_units[1]
 WHERE f.relay_environment='production' AND i.item_kind='fuel'
 AND f.purchased_on BETWEEN $1::date AND $2::date
 GROUP BY f.id,a.truck_id
 UNION ALL
 SELECT 'tolls',t.id::text,t.exit_date,t.truck_id,t.amount,t.posting_date,t.equipment_unit,t.id::text FROM tolls t
 WHERE (t.prepass_environment IS NULL OR t.prepass_environment='production')
 AND t.posting_date BETWEEN $1::date AND $2::date
), attributed AS (
 SELECT c.*,o.owners,i.is_company FROM costs c
 LEFT JOIN LATERAL (
  SELECT count(DISTINCT h.owner_id) owners,(array_agg(DISTINCT h.owner_id))[1] owner_id
   FROM truck_ownership_history h WHERE h.truck_id=c.truck_id
   AND (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<=c.cost_date)
   AND (h.unassigned_at IS NULL OR c.cost_date<(h.unassigned_at AT TIME ZONE 'America/New_York')::date)
 ) o ON true LEFT JOIN investors i ON i.id=o.owner_id
)`

const dashboardCompanyCostsSQL = dashboardCompanyCostsCTE + ` SELECT kind,coalesce(sum(amount) FILTER(WHERE owners=1 AND is_company),0)::text,
 count(*) FILTER(WHERE owners<>1 OR is_company IS NULL OR amount IS NULL)
 FROM attributed GROUP BY kind`

func (r *DashboardRepository) Waterfall(ctx context.Context, p DashboardPeriod) (DashboardWaterfall, error) {
	v := DashboardWaterfall{Gross: "0.00", Fuel: "0.00", Tolls: "0.00", DriverPay: "0.00", InvestorPay: "0.00", Review: []DashboardItem{}}
	week, err := time.Parse(time.DateOnly, p.DateFrom)
	if err != nil || week.Weekday() != time.Monday || week.AddDate(0, 0, 6).Format(time.DateOnly) != p.DateTo {
		return v, fmt.Errorf("waterfall requires a complete Monday-Sunday week")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return v, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, companyDashboardCTE+` SELECT matched_id,load_trip_id,inclusion,coalesce(gross::text,'') FROM dated`, p.DateFrom, p.DateTo)
	if err != nil {
		return v, err
	}
	grossByLoad := map[string]string{}
	var gross int64
	excluded := 0
	for rows.Next() {
		var id, tripID *int
		var inclusion, amount string
		if err = rows.Scan(&id, &tripID, &inclusion, &amount); err != nil {
			rows.Close()
			return v, err
		}
		n, e := chargeCents(amount)
		if inclusion != "included" || id == nil || e != nil {
			excluded++
			continue
		}
		gross += n
		grossByLoad[loadAllocationKey(*id, tripID)] = amount
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	v.Gross = chargeMoney(gross)
	review := func(id, label, href string, count int) {
		if count > 0 {
			v.Review = append(v.Review, DashboardItem{ID: id, Label: label, Href: href, Count: count})
		}
	}
	review("gross", "Gross Board rows need review", "/gross-board?dashboard=1&week="+p.DateFrom, excluded)
	// A historical investor truck without terms can be absent from the current
	// setup list. Resolve each counted load's dated ownership independently.
	var ownershipReview int
	err = tx.QueryRow(ctx, dashboardLoadOwnershipCTE+` SELECT count(*) FROM unresolved_ownership`, p.DateFrom, p.DateTo).Scan(&ownershipReview)
	if err != nil {
		return v, err
	}
	review("ownership", "Load ownership or investor terms need review", "/accounting/investor-pay?dashboard=1&weekStart="+p.DateFrom, ownershipReview)
	rows, err = tx.Query(ctx, dashboardCompanyCostsSQL, p.DateFrom, p.DateTo)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var kind, amount string
		var unresolved int
		if err = rows.Scan(&kind, &amount, &unresolved); err != nil {
			rows.Close()
			return v, err
		}
		if kind == "fuel" {
			v.Fuel = amount
		} else {
			v.Tolls = amount
		}
		review(kind, "Unresolved "+kind+" ownership", "/"+kind+"?dashboard=1&tab=transactions&dateFrom="+p.DateFrom+"&dateTo="+p.DateTo, unresolved)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	drivers, err := readDriverPayWeek(ctx, tx, week, "")
	if err != nil {
		return v, err
	}
	investors, err := readInvestorPay(ctx, tx, week)
	if err != nil {
		return v, err
	}
	paidLoads := map[string]bool{}
	driverPay, driverReview := dashboardEarnings(drivers, false, grossByLoad, paidLoads)
	investorPay, investorReview := dashboardEarnings(investors, true, grossByLoad, paidLoads)
	for id := range grossByLoad {
		if !paidLoads[id] {
			driverReview++
		}
	}
	v.DriverPay, v.InvestorPay = chargeMoney(driverPay), chargeMoney(investorPay)
	review("drivers", "Driver earnings need review", "/accounting/driver-pay?dashboard=1&weekStart="+p.DateFrom, driverReview)
	review("investors", "Investor earnings need review", "/accounting/investor-pay?dashboard=1&weekStart="+p.DateFrom, investorReview)
	if len(v.Review) == 0 {
		fuel, _ := chargeCents(v.Fuel)
		tolls, _ := chargeCents(v.Tolls)
		profit := chargeMoney(gross - fuel - tolls - driverPay - investorPay)
		v.Profit = &profit
	}
	return v, tx.Commit(ctx)
}

func dashboardEarnings(report DriverPayWeek, investor bool, gross map[string]string, covered map[string]bool) (int64, int) {
	var total int64
	review := len(report.Issues)
	// Current-fleet setup rows do not establish historical work. The dated load
	// ownership query checks missing investor terms for the selected week's work.
	for _, d := range report.Drivers {
		review += len(d.Issues)
		seen := map[string]bool{}
		for _, l := range d.Loads {
			fee, e := chargeCents(l.Fee)
			if e != nil {
				review++
				continue
			}
			if l.LoadRecordID == nil {
				review++
				continue
			}
			id := loadAllocationKey(*l.LoadRecordID, l.LoadTripID)
			source, ok := gross[id]
			current, ce := chargeCents(source)
			frozen, fe := chargeCents(l.OriginalRate)
			if !ok || ce != nil || fe != nil || current != frozen || seen[id] {
				review++
				continue
			}
			seen[id] = true
			covered[id] = true
			total += fee
		}
		// Explicit weekly adjustments and legacy finalized layovers belong to this week.
		for _, a := range d.Edits.Adjustments {
			n, e := chargeCents(a.Amount)
			if e != nil {
				review++
				continue
			}
			if a.Kind == "deduction" {
				n = -n
			}
			total += n
		}
		total += driverLayoverTotal(d)
		if investor {
			for _, a := range d.AutoCharges {
				if strings.HasPrefix(a.Source, "driver:") {
					n, e := chargeCents(a.Amount)
					if e != nil {
						review++
						continue
					}
					total += n
				}
			}
		}
	}
	return total, review
}
