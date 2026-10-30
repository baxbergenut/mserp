package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type truckCostAllocation struct {
	transactions []PayCostTransaction
	fuel         int64
	toll         int64
	driverFuel   map[string]int64
	driverToll   map[string]int64
	unlinkedFuel bool
}

func readTruckCostAllocations(ctx context.Context, tx pgx.Tx, week string) (map[string]*truckCostAllocation, error) {
	result := map[string]*truckCostAllocation{}
	ensure := func(id string) *truckCostAllocation {
		if result[id] == nil {
			result[id] = &truckCostAllocation{driverFuel: map[string]int64{}, driverToll: map[string]int64{}}
		}
		return result[id]
	}
	rows, err := tx.Query(ctx, `WITH aliases AS MATERIALIZED (
 SELECT unit_key,(array_agg(DISTINCT truck_id))[1] id FROM truck_unit_aliases GROUP BY unit_key HAVING count(DISTINCT truck_id)=1
 ), fuel AS MATERIALIZED (
 SELECT f.reported_truck_units[1] unit,coalesce(team.main_driver_id,f.driver_id) driver_id,sum(i.total_amount_paid) total
 FROM fuel_transactions f JOIN fuel_transaction_items i ON i.fuel_transaction_id=f.id
 LEFT JOIN driver_teams team ON team.team_driver_id=f.driver_id AND team.start_week<=f.purchased_on AND (team.end_week IS NULL OR f.purchased_on<team.end_week)
 WHERE f.relay_environment='production' AND i.item_kind='fuel' AND f.purchased_on >= $1::date AND f.purchased_on < $1::date+7
 AND cardinality(f.reported_truck_units)=1
 AND ($2::uuid[] IS NULL OR f.reported_truck_units[1] IN (SELECT unit_key FROM aliases WHERE id=ANY($2::uuid[])))
 GROUP BY f.reported_truck_units[1],coalesce(team.main_driver_id,f.driver_id)
 ) SELECT matched.id::text,coalesce(f.driver_id::text,''),sum(f.total)::text
 FROM fuel f JOIN aliases matched ON matched.unit_key=f.unit
 WHERE ($2::uuid[] IS NULL OR matched.id=ANY($2::uuid[])) GROUP BY matched.id,f.driver_id`, week, payrollTruckIDs(ctx))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var truck, driver, amount string
		if err = rows.Scan(&truck, &driver, &amount); err != nil {
			rows.Close()
			return nil, err
		}
		c := ensure(truck)
		if driver == "" {
			c.unlinkedFuel = true
			continue
		}
		n, e := chargeCents(amount)
		if e != nil {
			rows.Close()
			return nil, e
		}
		c.fuel += n
		c.driverFuel[driver] += n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `WITH toll_days AS MATERIALIZED (
 SELECT truck_id,exit_date,sum(amount) amount FROM tolls WHERE truck_id IS NOT NULL
 AND (prepass_environment IS NULL OR prepass_environment='production') AND posting_date>=$1::date AND posting_date<$1::date+7
 AND (accounting_cutover_date() IS NULL OR exit_date >= accounting_cutover_date())
 AND ($2::uuid[] IS NULL OR truck_id=ANY($2::uuid[])) GROUP BY truck_id,exit_date
 ), attributed AS (
 SELECT t.truck_id,t.exit_date,t.amount,CASE WHEN count(DISTINCT a.driver_id)=1 THEN (array_agg(DISTINCT a.driver_id))[1] END driver_id
 FROM toll_days t LEFT JOIN truck_driver_assignments a ON a.truck_id=t.truck_id
 AND a.assigned_at < ((t.exit_date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at >= ((t.exit_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 GROUP BY t.truck_id,t.exit_date,t.amount)
 SELECT truck_id::text,coalesce(driver_id::text,''),sum(amount)::text FROM attributed GROUP BY truck_id,driver_id`, week, payrollTruckIDs(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var truck, driver, amount string
		if err = rows.Scan(&truck, &driver, &amount); err != nil {
			return nil, err
		}
		n, e := chargeCents(amount)
		if e != nil {
			return nil, e
		}
		c := ensure(truck)
		c.toll += n
		c.driverToll[driver] += n
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if payrollCostDetails(ctx) {
		items, e := readNativeCostTransactions(ctx, tx, week)
		if e != nil {
			return nil, e
		}
		for _, item := range items {
			if item.truckID != "" {
				ensure(item.truckID).transactions = append(ensure(item.truckID).transactions, item)
			}
		}
	}
	return result, nil
}
func routeTruckDriverCosts(report *DriverPayWeek, groups map[string]*truckPayGroup, costs map[string]*truckCostAllocation) {
	for i := range report.Drivers {
		d := &report.Drivers[i]
		fuel, _ := chargeCents(d.FuelTotal)
		toll, _ := chargeCents(d.TollTotal)
		oldFuel, oldToll := fuel, toll
		{
			kept := []PayCostTransaction{}
			for _, item := range d.costTransactions {
				if groups[item.truckID] == nil {
					kept = append(kept, item)
				}
			}
			for id, g := range groups {
				if g.ownerOnly && g.ownerDriver == d.ID && costs[id] != nil {
					kept = append(kept, costs[id].transactions...)
				}
			}
			d.costTransactions = kept
		}
		for id, g := range groups {
			c := costs[id]
			if c == nil {
				continue
			}
			fuel -= c.driverFuel[d.ID]
			toll -= c.driverToll[d.ID]
			if g.ownerOnly && g.ownerDriver == d.ID {
				fuel += c.fuel
				toll += c.toll
			}
		}
		d.FuelTotal = chargeMoney(fuel)
		d.TollTotal = chargeMoney(toll)
		if (fuel != oldFuel && d.Edits.FuelOverride != nil) || (toll != oldToll && d.Edits.TollOverride != nil) {
			d.Issues = append(d.Issues, "Review and reset existing Fuel/Toll overrides after routing costs to investor trucks")
		}
	}
}

func investorExpenseDeductions(ctx context.Context, tx pgx.Tx, truck, owner, week string, ownerOnly bool) ([]ExpenseDeduction, error) {
	rows, err := tx.Query(ctx, `WITH relevant_expenses AS (SELECT e.*, expense_external_total(e.id) AS external_paid, `+expenseSettlementTotalSQL+` AS settlement_total FROM expenses e WHERE e.truck_id=$1 AND e.owner_id=$2) SELECT e.id,coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,e.expense_date::text,e.settlement_total::text,
 (e.settlement_total-e.external_paid-coalesce(p.other,0))::text,coalesce(w.amount,e.settlement_total-e.external_paid-coalesce(p.other,0))::text,
 (e.settlement_total-e.external_paid-coalesce(p.prior,0)-coalesce(w.amount,0))::text,(e.settlement_total-e.external_paid-coalesce(p.prior,0))::text,e.balance_version,w.expense_id IS NOT NULL
 FROM relevant_expenses e LEFT JOIN expense_payments w ON w.expense_id=e.id AND w.week_start=$3::date
 LEFT JOIN LATERAL(SELECT sum(amount) FILTER(WHERE week_start<>$3::date) other,sum(amount) FILTER(WHERE week_start<$3::date) prior FROM expense_payments WHERE expense_id=e.id) p ON true
 WHERE e.truck_id=$1 AND e.owner_id=$2 AND lower(btrim(e.covered_by))='truck owner' AND lower(btrim(coalesce(e.payment_type,'')))<>'reimburse' AND accounting_live_source('expense',e.id,e.expense_date) AND NOT e.driver_settled AND e.amount>=0
 AND e.expense_date<$3::date+10 AND (w.expense_id IS NOT NULL OR e.settlement_total-e.external_paid>coalesce(p.other,0))
 AND (w.expense_id IS NULL OR ($4 AND w.investor_truck_id IS NULL) OR (NOT $4 AND w.investor_truck_id=$1))
 ORDER BY e.expense_date,e.id`, truck, owner, week, ownerOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExpenseDeduction{}
	for rows.Next() {
		var d ExpenseDeduction
		if err = rows.Scan(&d.ExpenseID, &d.Name, &d.Category, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func saveInvestorExpenses(ctx context.Context, tx pgx.Tx, d DriverPayDriver, actor string, items []ExpenseDeduction) error {
	ids := []string{}
	seen := map[string]bool{}
	for _, v := range items {
		if !v.Apply {
			continue
		}
		if seen[v.ExpenseID] {
			return ErrDriverPayConflict
		}
		seen[v.ExpenseID] = true
		ids = append(ids, v.ExpenseID)
	}
	if len(ids) == 0 {
		return nil
	}
	// Lock the complete set in a stable order before validating versions or
	// balances. Keep every individual check and database audit trigger.
	rows, err := tx.Query(ctx, `SELECT id::text,balance_version FROM expenses WHERE id=ANY($1::uuid[]) AND truck_id=$2 AND owner_id=$3 AND lower(btrim(covered_by))='truck owner' ORDER BY id FOR UPDATE`, ids, d.ID, d.InvestorID)
	if err != nil {
		return err
	}
	versions := map[string]int{}
	for rows.Next() {
		var id string
		var version int
		if err = rows.Scan(&id, &version); err != nil {
			rows.Close()
			return err
		}
		versions[id] = version
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	current, err := investorExpenseDeductions(ctx, tx, d.ID, d.InvestorID, d.Edits.WeekStart, false)
	if err != nil {
		return err
	}
	balances := map[string]ExpenseDeduction{}
	for _, c := range current {
		balances[c.ExpenseID] = c
	}
	batch := &pgx.Batch{}
	for _, v := range items {
		if !v.Apply {
			continue
		}
		version, exists := versions[v.ExpenseID]
		if !exists || version != v.Version {
			return ErrDriverPayConflict
		}
		c, found := balances[v.ExpenseID]
		if !found {
			return ErrDriverPayConflict
		}
		n, e := chargeCents(v.Amount)
		limit, _ := chargeCents(c.Available)
		if e != nil || n < 0 || n > limit {
			return chargeInvalid("Expense deduction exceeds its available balance")
		}
		batch.Queue(`INSERT INTO expense_payments(expense_id,week_start,amount,updated_by,investor_truck_id) VALUES($1,$2::date,$3::numeric,nullif($4,'')::uuid,$5) ON CONFLICT(expense_id,week_start) DO UPDATE SET amount=excluded.amount,updated_by=excluded.updated_by,updated_at=now() WHERE expense_payments.investor_truck_id=excluded.investor_truck_id`, v.ExpenseID, d.Edits.WeekStart, v.Amount, actor, d.ID)
		batch.Queue(`UPDATE expenses SET updated_at=now() WHERE id=$1`, v.ExpenseID)
	}
	return tx.SendBatch(ctx, batch).Close()
}
