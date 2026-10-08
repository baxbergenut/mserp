package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func expenseDeductionsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]ExpenseDeduction, error) {
	rows, err := tx.Query(ctx, `SELECT e.charge_driver_id::text,e.id, coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,
 e.expense_date::text,e.amount::text,
 (e.amount-coalesce(p.other,0))::text,
 coalesce(w.amount,e.amount-coalesce(p.other,0))::text,
 (e.amount-coalesce(p.prior,0)-coalesce(w.amount,0))::text,
 (e.amount-coalesce(p.prior,0))::text,
 e.balance_version,w.expense_id IS NOT NULL
 FROM expenses e
 LEFT JOIN expense_payments w ON w.expense_id=e.id AND w.week_start=$2::date
 LEFT JOIN LATERAL (SELECT sum(amount) FILTER (WHERE week_start<>$2::date) AS other,
 sum(amount) FILTER (WHERE week_start<$2::date) AS prior FROM expense_payments WHERE expense_id=e.id) p ON true
 WHERE e.charge_driver_id=ANY($1::uuid[]) AND w.investor_truck_id IS NULL AND NOT e.driver_settled
 AND e.expense_date<$2::date+7 AND e.amount>=0
 AND (w.expense_id IS NOT NULL OR e.amount>coalesce(p.other,0))
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
	rows, err := tx.Query(ctx, `SELECT e.driver_id::text,e.id,e.start_date::text,e.amount::text,
 capacity.available::text,
 coalesce(w.amount,capacity.available)::text,
 (e.amount-e.opening_paid-coalesce(p.prior,0)+coalesce(r.prior,0)-coalesce(w.amount,0))::text,
 (e.amount-e.opening_paid-coalesce(p.prior,0)+coalesce(r.prior,0))::text,e.balance_version,w.escrow_id IS NOT NULL
 FROM driver_escrows e
 LEFT JOIN driver_escrow_payments w ON w.escrow_id=e.id AND w.week_start=$2::date
 LEFT JOIN LATERAL (SELECT sum(amount) prior FROM driver_escrow_payments WHERE escrow_id=e.id AND week_start<$2::date) p ON true
 LEFT JOIN LATERAL (SELECT sum(amount) prior FROM driver_escrow_releases WHERE escrow_id=e.id AND NOT cancelled AND week_start<$2::date) r ON true
 CROSS JOIN LATERAL (SELECT escrow_collection_available(e.id,$2::date) available) capacity
 WHERE e.driver_id=ANY($1::uuid[]) AND e.start_date<$2::date+7
 AND (w.escrow_id IS NOT NULL OR capacity.available>0) ORDER BY e.start_date,e.id`, drivers, week)
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
