package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type truckCostAllocation struct {
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
	rows, err := tx.Query(ctx, `SELECT matched.id::text,coalesce(f.driver_id::text,''),sum(i.total_amount_paid)::text FROM fuel_transactions f
 JOIN fuel_transaction_items i ON i.fuel_transaction_id=f.id
 JOIN LATERAL(SELECT (array_agg(t.id))[1] id FROM trucks t WHERE EXISTS(SELECT 1 FROM jsonb_array_elements(f.prompts) p WHERE lower(btrim(p->>'label'))='truck #' AND upper(btrim(t.unit_number))=upper(btrim(p->>'value'))) HAVING count(*)=1) matched ON true
	WHERE matched.id IS NOT NULL AND f.relay_environment='production' AND i.item_kind='fuel'
 AND (f.purchased_at AT TIME ZONE `+fuelTimezoneExpression("f.timezone")+`)::date >= $1::date
 AND (f.purchased_at AT TIME ZONE `+fuelTimezoneExpression("f.timezone")+`)::date < $1::date+7
 AND (SELECT count(DISTINCT upper(btrim(p->>'value'))) FROM jsonb_array_elements(f.prompts) p WHERE lower(btrim(p->>'label'))='truck #' AND btrim(p->>'value')<>'')=1
 GROUP BY matched.id,f.driver_id`, week)
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
	rows, err = tx.Query(ctx, `SELECT t.truck_id::text,coalesce(a.driver_id::text,''),sum(t.amount)::text FROM tolls t
 LEFT JOIN LATERAL(SELECT (array_agg(DISTINCT a.driver_id))[1] driver_id FROM truck_driver_assignments a WHERE a.truck_id=t.truck_id
 AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=t.exit_date AND (a.unassigned_at IS NULL OR t.exit_date<(a.unassigned_at AT TIME ZONE 'America/New_York')::date) HAVING count(DISTINCT a.driver_id)=1) a ON true
	WHERE t.truck_id IS NOT NULL AND (t.prepass_environment IS NULL OR t.prepass_environment='production') AND t.posting_date>=$1::date AND t.posting_date<$1::date+7 GROUP BY t.truck_id,a.driver_id`, week)
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
	return result, rows.Err()
}
func routeTruckDriverCosts(report *DriverPayWeek, groups map[string]*truckPayGroup, costs map[string]*truckCostAllocation) {
	for i := range report.Drivers {
		d := &report.Drivers[i]
		fuel, _ := chargeCents(d.FuelTotal)
		toll, _ := chargeCents(d.TollTotal)
		oldFuel, oldToll := fuel, toll
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
	rows, err := tx.Query(ctx, `SELECT e.id,coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,e.expense_date::text,e.amount::text,
 (e.amount-coalesce(p.other,0))::text,coalesce(w.amount,e.amount-coalesce(p.other,0))::text,
 (e.amount-coalesce(p.prior,0)-coalesce(w.amount,0))::text,(e.amount-coalesce(p.prior,0))::text,e.balance_version,w.expense_id IS NOT NULL
 FROM expenses e LEFT JOIN expense_payments w ON w.expense_id=e.id AND w.week_start=$3::date
 LEFT JOIN LATERAL(SELECT sum(amount) FILTER(WHERE week_start<>$3::date) other,sum(amount) FILTER(WHERE week_start<$3::date) prior FROM expense_payments WHERE expense_id=e.id) p ON true
 WHERE e.truck_id=$1 AND e.owner_id=$2 AND lower(btrim(e.covered_by))='truck owner' AND NOT e.driver_settled AND e.amount>=0
 AND e.expense_date<$3::date+7 AND (w.expense_id IS NOT NULL OR e.amount>coalesce(p.other,0))
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
	for _, v := range items {
		if !v.Apply {
			continue
		}
		var version int
		if err := tx.QueryRow(ctx, `SELECT balance_version FROM expenses WHERE id=$1 AND truck_id=$2 AND owner_id=$3 AND lower(btrim(covered_by))='truck owner' FOR UPDATE`, v.ExpenseID, d.ID, d.InvestorID).Scan(&version); err != nil {
			return err
		}
		if version != v.Version {
			return ErrDriverPayConflict
		}
		current, err := investorExpenseDeductions(ctx, tx, d.ID, d.InvestorID, d.Edits.WeekStart, false)
		if err != nil {
			return err
		}
		found := false
		for _, c := range current {
			if c.ExpenseID != v.ExpenseID {
				continue
			}
			found = true
			n, e := chargeCents(v.Amount)
			limit, _ := chargeCents(c.Available)
			if e != nil || n < 0 || n > limit {
				return chargeInvalid("Expense deduction exceeds its available balance")
			}
		}
		if !found {
			return ErrDriverPayConflict
		}
		if _, err = tx.Exec(ctx, `INSERT INTO expense_payments(expense_id,week_start,amount,updated_by,investor_truck_id) VALUES($1,$2::date,$3::numeric,nullif($4,'')::uuid,$5) ON CONFLICT(expense_id,week_start) DO UPDATE SET amount=excluded.amount,updated_by=excluded.updated_by,updated_at=now() WHERE expense_payments.investor_truck_id=excluded.investor_truck_id`, v.ExpenseID, d.Edits.WeekStart, v.Amount, actor, d.ID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE expenses SET updated_at=now() WHERE id=$1`, v.ExpenseID); err != nil {
			return err
		}
	}
	return nil
}
