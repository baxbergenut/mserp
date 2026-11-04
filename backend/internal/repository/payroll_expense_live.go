package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
)

// Driver-paid expenses are a live balance overlay on the prepared payroll
// card. This query never reads loads, recalculates wages, or writes statements.
func overlayDriverExpenseBalances(ctx context.Context, tx pgx.Tx, report *DriverPayWeek) error {
	defer db.Phase(ctx, "live_expenses")()
	type scope struct {
		ID      string   `json:"id"`
		Drivers []string `json:"drivers"`
	}
	scopes := []scope{}
	for _, d := range report.Drivers {
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		s := scope{ID: d.ID, Drivers: []string{d.ID}}
		for _, member := range d.TeamDrivers {
			s.Drivers = append(s.Drivers, member.ID)
		}
		scopes = append(scopes, s)
	}
	if len(scopes) == 0 {
		return nil
	}
	body, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `WITH scopes AS MATERIALIZED (
 SELECT * FROM jsonb_to_recordset($1::jsonb) s(id uuid,drivers uuid[])
 ), invoices AS MATERIALIZED (
 SELECT s.id statement,e.*,expense_collectible_total(e) total,expense_collectible_total(e)-expense_external_total(e.id) collectible
 FROM scopes s JOIN expenses e ON e.charge_driver_id=ANY(s.drivers)
 WHERE lower(btrim(e.covered_by))='driver' AND lower(btrim(coalesce(e.payment_type,'')))<>'reimburse'
 AND e.amount>=0 AND (NOT e.driver_settled OR expense_balance_adjustment(e.id)<>0)
 AND accounting_live_source('expense',e.id,e.expense_date)
 ), balances AS (
 SELECT e.*,coalesce(p.paid,0) paid,w.amount posted,
 coalesce(choices.expense_choices->e.id::text,'{}') choice
 FROM invoices e
 LEFT JOIN LATERAL (SELECT sum(amount) paid FROM expense_payments WHERE expense_id=e.id) p ON true
 LEFT JOIN expense_payments w ON w.expense_id=e.id AND w.week_start=$2::date AND w.investor_truck_id IS NULL
 AND (w.statement_id IS NULL OR (w.statement_kind='driver' AND w.statement_id=e.statement))
 LEFT JOIN driver_pay_weeks choices ON choices.driver_id=e.statement AND choices.week_start=$2::date
 ) SELECT statement::text,id::text,coalesce(nullif(btrim(expense_type),''),category),category,expense_date::text,
 total::text,(collectible-paid+coalesce(posted,0))::text,coalesce(posted,collectible-paid)::text,
 balance_version,posted IS NOT NULL,choice
 FROM balances WHERE posted IS NOT NULL OR collectible>paid ORDER BY statement,expense_date,id`, body, report.WeekStart)
	if err != nil {
		return err
	}
	byID := map[string][]ExpenseDeduction{}
	for rows.Next() {
		var id string
		var choiceBody []byte
		d := ExpenseDeduction{LiveBalance: true}
		if err = rows.Scan(&id, &d.ExpenseID, &d.Name, &d.Category, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Version, &d.Saved, &choiceBody); err != nil {
			rows.Close()
			return err
		}
		available, amount := int64(0), int64(0)
		available, err = chargeCents(d.Available)
		if err == nil {
			amount, err = chargeCents(d.Amount)
		}
		if err != nil {
			rows.Close()
			return err
		}
		d.OpeningBalance, d.Remaining = d.Available, chargeMoney(available-amount)
		choice := payrollExpenseChoice{}
		if err = json.Unmarshal(choiceBody, &choice); err != nil {
			rows.Close()
			return err
		}
		choices := payrollExpenseChoices{}
		if choice.Mode != "" {
			choices[d.ExpenseID] = choice
		}
		entry := []ExpenseDeduction{d}
		if err = applyPayrollExpenseChoices(entry, choices); err != nil {
			rows.Close()
			return err
		}
		byID[id] = append(byID[id], entry[0])
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement != nil && d.Settlement.Finalized {
			continue
		}
		live := byID[d.ID]
		if live == nil {
			live = []ExpenseDeduction{}
		}
		if d.Summary != nil {
			var total int64
			for _, item := range live {
				n, e := chargeCents(item.Amount)
				if e != nil {
					return e
				}
				total += n
			}
			previous, _ := chargeCents(d.Summary.LiveExpenseTotal)
			payable, e := chargeCents(d.Summary.Payable)
			if e != nil {
				return e
			}
			d.Summary.Payable = chargeMoney(payable + previous - total)
			d.Summary.LiveExpenseTotal = chargeMoney(total)
		} else {
			kept := []ExpenseDeduction{}
			for _, item := range d.Edits.ExpenseDeductions {
				if !item.LiveBalance {
					kept = append(kept, item)
				}
			}
			d.Edits.ExpenseDeductions = append(kept, live...)
			sort.SliceStable(d.Edits.ExpenseDeductions, func(i, j int) bool {
				a, b := d.Edits.ExpenseDeductions[i], d.Edits.ExpenseDeductions[j]
				if a.Source != b.Source {
					return a.Source < b.Source
				}
				if a.ExpenseDate != b.ExpenseDate {
					return a.ExpenseDate < b.ExpenseDate
				}
				return a.ExpenseID < b.ExpenseID
			})
		}
		proof, err := json.Marshal(live)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(append([]byte(d.ID+":"+report.WeekStart+":"), proof...))
		d.ExpenseRevision = hex.EncodeToString(hash[:])
	}
	return nil
}

// A native expense write may skip payroll publication only when every captured
// event belongs to a driver-paid balance and every dated recipient already has
// a compatible prepared card. New membership and unknown routing use the normal
// publisher. Marking the exact events lets later reads prove that their base
// wages are unchanged; their live balances and review hashes are still reread.
func publishLiveDriverExpenses(ctx context.Context, tx pgx.Tx) (bool, error) {
	var eligible bool
	err := tx.QueryRow(ctx, `WITH changes AS MATERIALIZED (
 SELECT * FROM payroll_projection_changes WHERE source_xid=pg_current_xact_id() AND dispatched_at IS NULL
 ), images AS MATERIALIZED (
 SELECT c.table_name,c.operation,r FROM changes c CROSS JOIN LATERAL jsonb_array_elements(c.old_routes||c.new_routes) r
 ), payers AS MATERIALIZED (
 SELECT r->>'charge_driver_id' driver FROM images WHERE table_name='expenses'
 UNION SELECT e.charge_driver_id::text FROM images i JOIN expenses e ON e.id::text=i.r->>'expense_id'
 WHERE i.table_name IN ('expense_balance_corrections','expense_external_settlements')
 ) SELECT EXISTS(SELECT 1 FROM changes)
 AND NOT EXISTS(SELECT 1 FROM changes WHERE table_name NOT IN ('expenses','expense_balance_corrections','expense_external_settlements') OR operation='TRUNCATE' OR old_routes||new_routes='[]'::jsonb)
 AND NOT EXISTS(SELECT 1 FROM images i WHERE CASE WHEN table_name='expenses' THEN
 NOT (r ? 'payment_type') OR lower(btrim(coalesce(r->>'covered_by','')))<>'driver'
 OR lower(btrim(coalesce(r->>'payment_type','')))='reimburse' OR coalesce(r->>'charge_driver_id','')=''
 ELSE NOT EXISTS(SELECT 1 FROM expenses e WHERE e.id::text=i.r->>'expense_id'
 AND lower(btrim(e.covered_by))='driver' AND lower(btrim(coalesce(e.payment_type,'')))<>'reimburse' AND e.charge_driver_id IS NOT NULL) END)
 AND NOT EXISTS(SELECT 1 FROM payers p CROSS JOIN payroll_projection_weeks w WHERE NOT EXISTS(
 SELECT 1 FROM payroll_statement_current c JOIN payroll_projection_generations g ON g.id=c.generation_id
 WHERE c.kind='driver' AND c.week_start=w.week_start AND c.statement_id=payroll_driver_at(p.driver::uuid,w.week_start)
 AND g.calculator_version=$1
 AND (c.finalized OR c.edit_version>0 OR jsonb_array_length(coalesce(c.detail->'loads','[]'))>0)))`, payrollCalculatorVersion).Scan(&eligible)
	if err != nil || !eligible {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE payroll_projection_changes SET live_expense=true,dispatched_at=clock_timestamp()
 WHERE source_xid=pg_current_xact_id() AND dispatched_at IS NULL`)
	return err == nil, err
}
