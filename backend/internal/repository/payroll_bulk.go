package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
)

func expenseDeductionsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]ExpenseDeduction, error) {
	if rows, ok := payrollBatchedDeductions(ctx, tx, drivers, week, "all"); ok {
		return rows, nil
	}
	// Open statements show all unpaid expenses, independently of invoice date.
	// Only posted payments reduce the central balance; choices are overlaid later.
	rows, err := tx.Query(ctx, `WITH relevant_expenses AS MATERIALIZED (
 SELECT e.*, `+expenseOwnerExternalSQL+` AS external_paid, `+expenseSettlementTotalSQL+` AS settlement_total FROM expenses e WHERE charge_driver_id=ANY($1::uuid[]) AND (lower(btrim(coalesce(payment_type,'')))<>'reimburse' OR coalesce((to_jsonb(e)->>'owner_recovery')::boolean,false)) AND accounting_live_source('expense',e.id,e.expense_date) AND (NOT driver_settled OR expense_balance_adjustment(id)<>0) AND amount>=0
 ), payments AS MATERIALIZED (
 SELECT p.expense_id,sum(p.amount) FILTER(WHERE p.week_start<>$2::date) other,sum(p.amount) FILTER(WHERE p.week_start<$2::date) prior
 FROM expense_payments p JOIN relevant_expenses e ON e.id=p.expense_id GROUP BY p.expense_id
 ) SELECT e.charge_driver_id::text,e.id, coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,
 e.expense_date::text,e.settlement_total::text,
 (e.settlement_total-e.external_paid-coalesce(p.other,0))::text,
 coalesce(w.amount,e.settlement_total-e.external_paid-coalesce(p.other,0))::text,
 (e.settlement_total-e.external_paid-coalesce(p.prior,0)-coalesce(w.amount,0))::text,
 (e.settlement_total-e.external_paid-coalesce(p.prior,0))::text,
 e.balance_version,w.expense_id IS NOT NULL,lower(btrim(e.covered_by))='driver'
 FROM relevant_expenses e
 LEFT JOIN expense_payments w ON w.expense_id=e.id AND w.week_start=$2::date
 LEFT JOIN payments p ON p.expense_id=e.id
 WHERE w.investor_truck_id IS NULL
 AND (w.expense_id IS NOT NULL OR e.settlement_total-e.external_paid>coalesce(p.other,0))
 ORDER BY e.expense_date,e.id`, drivers, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]ExpenseDeduction{}
	for rows.Next() {
		var driver string
		var d ExpenseDeduction
		if err := rows.Scan(&driver, &d.ExpenseID, &d.Name, &d.Category, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved, &d.LiveBalance); err != nil {
			return nil, err
		}
		result[driver] = append(result[driver], d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	credits, err := expenseReimbursementDeductionsBulk(ctx, tx, drivers, week)
	if err != nil {
		return nil, err
	}
	for id, entries := range credits {
		result[id] = append(result[id], entries...)
	}
	accessorials, err := expenseAccessorialCredits(ctx, tx, drivers, week)
	if err != nil {
		return nil, err
	}
	for id, entries := range accessorials {
		result[id] = append(result[id], entries...)
	}
	escrows, err := escrowDeductionsBulk(ctx, tx, drivers, week)
	for id, entries := range escrows {
		result[id] = append(result[id], entries...)
	}
	return result, err
}

func escrowDeductionsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]ExpenseDeduction, error) {
	if rows, ok := payrollBatchedDeductions(ctx, tx, drivers, week, "escrow"); ok {
		return rows, nil
	}
	rows, err := tx.Query(ctx, `WITH `+strings.NewReplacer("$1", "$2", "$3", "$1").Replace(payrollEscrowCapacityCTE)+` SELECT e.driver_id::text,e.id,e.start_date::text,e.amount::text,
 capacity.available::text,coalesce(w.amount,capacity.available)::text,
 (e.amount-e.opening_paid+coalesce(p.delta,0)-coalesce(w.amount,0))::text,
 (e.amount-e.opening_paid+coalesce(p.delta,0))::text,e.balance_version,w.escrow_id IS NOT NULL
 FROM pay_escrows e JOIN pay_escrow_capacity capacity ON capacity.id=e.id
 LEFT JOIN driver_escrow_payments w ON w.escrow_id=e.id AND w.week_start=$2::date
 LEFT JOIN pay_escrow_opening p ON p.id=e.id
 WHERE w.escrow_id IS NOT NULL OR (capacity.available>0 AND NOT escrow_payroll_suppressed(e.id,e.driver_id,$2::date)) ORDER BY e.start_date,e.id`, drivers, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]ExpenseDeduction{}
	for rows.Next() {
		var driver string
		d := ExpenseDeduction{Name: "Escrow", Source: "escrow"}
		if err = rows.Scan(&driver, &d.ExpenseID, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved); err != nil {
			return nil, err
		}
		result[driver] = append(result[driver], d)
	}
	return result, rows.Err()
}

func escrowReleaseCreditsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]PayAutoCharge, error) {
	rows, err := tx.Query(ctx, `SELECT e.driver_id::text,r.id::text,r.amount::text,'Escrow release' FROM driver_escrow_releases r JOIN driver_escrows e ON e.id=r.escrow_id WHERE accounting_live_source('escrow',e.id,e.start_date) AND e.driver_id=ANY($1::uuid[]) AND r.week_start=$2::date AND NOT r.cancelled AND r.payroll_return IS NULL AND r.correction_reason IS NULL
 UNION ALL SELECT w.driver_id::text,w.escrow_draft->>'escrowId',w.escrow_draft->>'returnAmount','Escrow returned'
 FROM driver_pay_weeks w WHERE w.driver_id=ANY($1::uuid[]) AND w.week_start=$2::date
 AND (w.escrow_draft->>'returnAmount')::numeric>0 ORDER BY 2`, drivers, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]PayAutoCharge{}
	for rows.Next() {
		var driver string
		var id, amount, name string
		if err = rows.Scan(&driver, &id, &amount, &name); err != nil {
			return nil, err
		}
		result[driver] = append(result[driver], PayAutoCharge{Name: name, Amount: amount, Source: "escrow-release:" + id})
	}
	return result, rows.Err()
}
