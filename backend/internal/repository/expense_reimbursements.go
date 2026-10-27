package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (r *ExpenseRepository) loadReimbursementBalances(ctx context.Context, values []Expense) error {
	ids := []string{}
	for _, e := range values {
		if e.PaymentType != nil && strings.EqualFold(strings.TrimSpace(*e.PaymentType), "reimburse") {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var available bool
	if err := r.pool.QueryRow(ctx, `SELECT to_regclass('expense_reimbursement_payments') IS NOT NULL`).Scan(&available); err != nil {
		return err
	}
	if !available {
		return nil
	}
	rows, err := r.pool.Query(ctx, `SELECT e.id::text,coalesce(sum(p.amount),0)::text,(e.amount-coalesce(sum(p.amount),0))::text,
 coalesce(jsonb_agg(jsonb_build_object('weekStart',p.week_start::text,'amount',p.amount::text) ORDER BY p.week_start) FILTER(WHERE p.expense_id IS NOT NULL),'[]'::jsonb)
 FROM expenses e LEFT JOIN expense_reimbursement_payments p ON p.expense_id=e.id WHERE e.id=ANY($1::uuid[]) GROUP BY e.id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, paid, left string
		var payments []ExpensePayment
		if err = rows.Scan(&id, &paid, &left, &payments); err != nil {
			return err
		}
		for i := range values {
			if values[i].ID == id {
				values[i].PaidAmount = &paid
				values[i].RemainingAmount = &left
				values[i].Payments = payments
				values[i].DriverSettled = false
				break
			}
		}
	}
	return rows.Err()
}

// Reimbursement payouts use the same expense window and balance reservations as deductions.
func reimbursementLedgerAvailable(ctx context.Context, tx pgx.Tx) (bool, error) {
	var available bool
	err := tx.QueryRow(ctx, `SELECT to_regclass('expense_reimbursement_payments') IS NOT NULL`).Scan(&available)
	return available, err
}
func expenseReimbursementDeductionsBulk(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]ExpenseDeduction, error) {
	result := map[string][]ExpenseDeduction{}
	available, err := reimbursementLedgerAvailable(ctx, tx)
	if err != nil || !available {
		return result, err
	}
	rows, err := tx.Query(ctx, `WITH relevant AS MATERIALIZED (
 SELECT e.* FROM expenses e WHERE driver_id=ANY($1::uuid[]) AND lower(btrim(payment_type))='reimburse' AND expense_date<$2::date+10 AND amount>=0
 ), payments AS (SELECT p.expense_id,sum(p.amount) FILTER(WHERE p.week_start<>$2::date) other,sum(p.amount) FILTER(WHERE p.week_start<$2::date) prior FROM expense_reimbursement_payments p JOIN relevant e ON e.id=p.expense_id GROUP BY p.expense_id)
 SELECT e.driver_id::text,e.id,coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,e.expense_date::text,e.amount::text,
 (e.amount-coalesce(p.other,0))::text,coalesce(w.amount,e.amount-coalesce(p.other,0))::text,
 (e.amount-coalesce(p.prior,0)-coalesce(w.amount,0))::text,(e.amount-coalesce(p.prior,0))::text,e.balance_version,w.expense_id IS NOT NULL
 FROM relevant e LEFT JOIN payments p ON p.expense_id=e.id LEFT JOIN expense_reimbursement_payments w ON w.expense_id=e.id AND w.week_start=$2::date
 WHERE w.expense_id IS NOT NULL OR e.amount>coalesce(p.other,0) ORDER BY e.expense_date,e.id`, drivers, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var driver string
		d := ExpenseDeduction{Source: "reimburse"}
		if err = rows.Scan(&driver, &d.ExpenseID, &d.Name, &d.Category, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved); err != nil {
			return nil, err
		}
		result[driver] = append(result[driver], d)
	}
	return result, rows.Err()
}

// Older frozen statements predate expenseDate metadata. Add only the immutable
// source date to the response; never rewrite the frozen amounts or saved JSON.
func populateExpenseReimbursementDates(ctx context.Context, tx pgx.Tx, statements []DriverPayDriver) error {
	ids := []string{}
	for _, statement := range statements {
		for _, row := range statement.AutoCharges {
			if row.ExpenseDate == "" && strings.HasPrefix(row.Source, "expense-reimburse:") {
				ids = append(ids, strings.TrimPrefix(row.Source, "expense-reimburse:"))
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text,expense_date::text FROM expenses WHERE id=ANY($1::uuid[])`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	dates := map[string]string{}
	for rows.Next() {
		var id, date string
		if err = rows.Scan(&id, &date); err != nil {
			return err
		}
		dates["expense-reimburse:"+id] = date
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, statement := range statements {
		for i := range statement.AutoCharges {
			row := &statement.AutoCharges[i]
			if row.ExpenseDate == "" {
				row.ExpenseDate = dates[row.Source]
			}
		}
	}
	return nil
}
