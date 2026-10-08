package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

type truckPayMetadata struct {
	unit, name, ownerDriver string
	conflict, ownerFullWeek bool
}
type truckPayPerson struct {
	person                       PayPerson
	dispatcherID, dispatcherName string
}
type investorWeekEdit struct {
	edits, report []byte
	meta          PayrollSettlement
}
type truckExpense struct {
	deduction   ExpenseDeduction
	destination *string
}
type truckPayInputs struct {
	metadata map[string]truckPayMetadata
	people   []truckPayPerson
	assigned map[string]map[string]bool
	edits    map[string]investorWeekEdit
	expenses map[string][]truckExpense
}

func readTruckPayInputs(ctx context.Context, tx pgx.Tx, week string) (truckPayInputs, error) {
	v := truckPayInputs{metadata: map[string]truckPayMetadata{}, assigned: map[string]map[string]bool{}, edits: map[string]investorWeekEdit{}, expenses: map[string][]truckExpense{}}
	rows, e := tx.Query(ctx, `WITH terms AS (SELECT DISTINCT ON(truck_id) * FROM truck_settlement_terms WHERE week_start<=$1::date ORDER BY truck_id,week_start DESC)
 SELECT s.truck_id::text,t.unit_number,coalesce(d.full_name,i.full_name),coalesce(i.driver_id::text,''),
 EXISTS(SELECT 1 FROM truck_ownership_history h WHERE h.truck_id=s.truck_id AND h.owner_id<>s.owner_id AND (h.assigned_at AT TIME ZONE 'America/New_York')::date<$1::date+7 AND (h.unassigned_at IS NULL OR (h.unassigned_at AT TIME ZONE 'America/New_York')::date>$1::date)),
 (SELECT count(*)=1 AND bool_and(a.driver_id=i.driver_id) FROM truck_driver_assignments a WHERE a.truck_id=s.truck_id AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=$1::date AND (a.unassigned_at IS NULL OR (a.unassigned_at AT TIME ZONE 'America/New_York')::date>=$1::date+7))
 FROM terms s JOIN trucks t ON t.id=s.truck_id JOIN investors i ON i.id=s.owner_id LEFT JOIN drivers d ON d.id=i.driver_id`, week)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var id string
		var m truckPayMetadata
		var ownerFull *bool
		if e = rows.Scan(&id, &m.unit, &m.name, &m.ownerDriver, &m.conflict, &ownerFull); e != nil {
			rows.Close()
			return v, e
		}
		m.ownerFullWeek = ownerFull != nil && *ownerFull
		v.metadata[id] = m
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = tx.Query(ctx, `SELECT d.id::text,d.full_name,coalesce(p.id::text,''),coalesce(p.full_name,'Unassigned') FROM drivers d LEFT JOIN dispatchers p ON p.id=d.dispatcher_id ORDER BY d.full_name,d.id`)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var p truckPayPerson
		if e = rows.Scan(&p.person.ID, &p.person.Name, &p.dispatcherID, &p.dispatcherName); e != nil {
			rows.Close()
			return v, e
		}
		v.people = append(v.people, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = tx.Query(ctx, `SELECT truck_id::text,driver_id::text FROM truck_driver_assignments WHERE (assigned_at AT TIME ZONE 'America/New_York')::date<$1::date+7 AND (unassigned_at IS NULL OR (unassigned_at AT TIME ZONE 'America/New_York')::date>$1::date)`, week)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var truck, driver string
		if e = rows.Scan(&truck, &driver); e != nil {
			rows.Close()
			return v, e
		}
		if v.assigned[truck] == nil {
			v.assigned[truck] = map[string]bool{}
		}
		v.assigned[truck][driver] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = tx.Query(ctx, `SELECT w.truck_id::text,w.edits,w.version,w.finalized,w.report,coalesce(w.finalized_at,now()),coalesce(u.username,'') FROM investor_pay_weeks w LEFT JOIN app_users u ON u.id=w.finalized_by WHERE w.week_start=$1::date`, week)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var id string
		var w investorWeekEdit
		if e = rows.Scan(&id, &w.edits, &w.meta.Version, &w.meta.Finalized, &w.report, &w.meta.FinalizedAt, &w.meta.FinalizedBy); e != nil {
			rows.Close()
			return v, e
		}
		v.edits[id] = w
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = tx.Query(ctx, `WITH terms AS (SELECT DISTINCT ON(truck_id) truck_id,owner_id FROM truck_settlement_terms WHERE week_start<=$1::date ORDER BY truck_id,week_start DESC)
 SELECT e.truck_id::text,w.investor_truck_id::text,e.id,coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,e.expense_date::text,e.amount::text,
 (e.amount-coalesce(p.other,0))::text,coalesce(w.amount,e.amount-coalesce(p.other,0))::text,
 (e.amount-coalesce(p.prior,0)-coalesce(w.amount,0))::text,(e.amount-coalesce(p.prior,0))::text,e.balance_version,w.expense_id IS NOT NULL
 FROM expenses e JOIN terms s ON s.truck_id=e.truck_id AND s.owner_id=e.owner_id
 LEFT JOIN expense_payments w ON w.expense_id=e.id AND w.week_start=$1::date
 LEFT JOIN LATERAL(SELECT sum(amount) FILTER(WHERE week_start<>$1::date) other,sum(amount) FILTER(WHERE week_start<$1::date) prior FROM expense_payments WHERE expense_id=e.id) p ON true
 WHERE lower(btrim(e.covered_by))='truck owner' AND NOT e.driver_settled AND e.amount>=0 AND e.expense_date<$1::date+7 AND (w.expense_id IS NOT NULL OR e.amount>coalesce(p.other,0)) ORDER BY e.expense_date,e.id`, week)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var x truckExpense
		d := &x.deduction
		if e = rows.Scan(&id, &x.destination, &d.ExpenseID, &d.Name, &d.Category, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved); e != nil {
			return v, e
		}
		v.expenses[id] = append(v.expenses[id], x)
	}
	return v, rows.Err()
}
func (v truckPayInputs) applyEdits(d *DriverPayDriver) error {
	w, ok := v.edits[d.ID]
	if !ok {
		return nil
	}
	if w.meta.Finalized {
		if e := json.Unmarshal(w.report, d); e != nil {
			return e
		}
		d.Settlement = &w.meta
		return nil
	}
	expenses := d.Edits.ExpenseDeductions
	if e := json.Unmarshal(w.edits, &d.Edits); e != nil {
		return e
	}
	d.Edits.Version = w.meta.Version
	d.Edits.ExpenseDeductions = expenses
	return nil
}
func (v truckPayInputs) deductions(truck string, ownerOnly bool) []ExpenseDeduction {
	out := []ExpenseDeduction{}
	for _, x := range v.expenses[truck] {
		if !x.deduction.Saved || (ownerOnly && x.destination == nil) || (!ownerOnly && x.destination != nil && *x.destination == truck) {
			out = append(out, x.deduction)
		}
	}
	return out
}
