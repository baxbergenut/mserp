package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
)

func expenseDeductionsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]ExpenseDeduction, error) {
	// Expenses are eligible through the following Wednesday (Thursday is
	// exclusive). Older unpaid balances still carry; other weeks reserve payments.
	rows, err := tx.Query(ctx, `WITH relevant_expenses AS MATERIALIZED (
 SELECT e.*, `+expenseSettlementTotalSQL+` AS settlement_total FROM expenses e WHERE charge_driver_id=ANY($1::uuid[]) AND lower(btrim(coalesce(payment_type,'')))<>'reimburse' AND NOT driver_settled AND expense_date<$2::date+10 AND amount>=0
 ), payments AS MATERIALIZED (
 SELECT p.expense_id,sum(p.amount) FILTER(WHERE p.week_start<>$2::date) other,sum(p.amount) FILTER(WHERE p.week_start<$2::date) prior
 FROM expense_payments p JOIN relevant_expenses e ON e.id=p.expense_id GROUP BY p.expense_id
 ) SELECT e.charge_driver_id::text,e.id, coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,
 e.expense_date::text,e.settlement_total::text,
 (e.settlement_total-coalesce(p.other,0))::text,
 coalesce(w.amount,e.settlement_total-coalesce(p.other,0))::text,
 (e.settlement_total-coalesce(p.prior,0)-coalesce(w.amount,0))::text,
 (e.settlement_total-coalesce(p.prior,0))::text,
 e.balance_version,w.expense_id IS NOT NULL
 FROM relevant_expenses e
 LEFT JOIN expense_payments w ON w.expense_id=e.id AND w.week_start=$2::date
 LEFT JOIN payments p ON p.expense_id=e.id
 WHERE w.investor_truck_id IS NULL
 AND (w.expense_id IS NOT NULL OR e.settlement_total>coalesce(p.other,0))
 ORDER BY e.expense_date,e.id`, drivers, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]ExpenseDeduction{}
	for rows.Next() {
		var driver string
		var d ExpenseDeduction
		if err := rows.Scan(&driver, &d.ExpenseID, &d.Name, &d.Category, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved); err != nil {
			return nil, err
		}
		result[driver] = append(result[driver], d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	escrows, err := escrowDeductionsBulk(ctx, tx, drivers, week)
	for id, entries := range escrows {
		result[id] = append(result[id], entries...)
	}
	return result, err
}

func escrowDeductionsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]ExpenseDeduction, error) {
	rows, err := tx.Query(ctx, `WITH `+strings.NewReplacer("$1", "$2", "$3", "$1").Replace(payrollEscrowCapacityCTE)+` SELECT e.driver_id::text,e.id,e.start_date::text,e.amount::text,
 capacity.available::text,coalesce(w.amount,capacity.available)::text,
 (e.amount-e.opening_paid+coalesce(p.delta,0)-coalesce(w.amount,0))::text,
 (e.amount-e.opening_paid+coalesce(p.delta,0))::text,e.balance_version,w.escrow_id IS NOT NULL
 FROM pay_escrows e JOIN pay_escrow_capacity capacity ON capacity.id=e.id
 LEFT JOIN driver_escrow_payments w ON w.escrow_id=e.id AND w.week_start=$2::date
 LEFT JOIN pay_escrow_opening p ON p.id=e.id
 WHERE w.escrow_id IS NOT NULL OR capacity.available>0 ORDER BY e.start_date,e.id`, drivers, week)
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
	rows, err := tx.Query(ctx, `SELECT e.driver_id::text,r.id::text,r.amount::text FROM driver_escrow_releases r JOIN driver_escrows e ON e.id=r.escrow_id WHERE e.driver_id=ANY($1::uuid[]) AND r.week_start=$2::date AND NOT r.cancelled ORDER BY r.id`, drivers, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]PayAutoCharge{}
	for rows.Next() {
		var driver string
		var id, amount string
		if err = rows.Scan(&driver, &id, &amount); err != nil {
			return nil, err
		}
		result[driver] = append(result[driver], PayAutoCharge{Name: "Escrow release", Amount: amount, Source: "escrow-release:" + id})
	}
	return result, rows.Err()
}
