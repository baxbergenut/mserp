package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Investor charges mirror saved Driver Pay payouts in their posting week.
// They never create a second payment against the original invoice.
func readOwnerReimbursementCharges(ctx context.Context, tx pgx.Tx, week string) (map[string][]PayAutoCharge, error) {
	result := map[string][]PayAutoCharge{}
	if payrollCostsOnly(ctx) {
		return result, nil
	}
	rows, err := tx.Query(ctx, `WITH terms AS (`+activePayrollTruckTermsSQL+`)
 SELECT e.truck_id::text, coalesce(nullif(btrim(e.expense_type),''),e.category),
 (-p.amount)::text, 'expense-reimburse:'||e.id, e.expense_date::text
 FROM expense_reimbursement_payments p JOIN expenses e ON e.id=p.expense_id
 JOIN terms s ON s.truck_id=e.truck_id AND s.owner_id=e.owner_id
 WHERE p.week_start=$1::date AND p.amount>0 AND lower(btrim(e.payment_type))='reimburse'
 AND NOT coalesce((to_jsonb(e)->>'owner_recovery')::boolean,false)
 AND lower(btrim(e.covered_by))='truck owner' AND e.owner_id<>'`+CompanyOwnerID+`'
 ORDER BY e.expense_date,e.id`, week, payrollTruckIDs(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var truck string
		var charge PayAutoCharge
		if err = rows.Scan(&truck, &charge.Name, &charge.Amount, &charge.Source, &charge.ExpenseDate); err != nil {
			return nil, err
		}
		result[truck] = append(result[truck], charge)
	}
	return result, rows.Err()
}

func validateOwnerReimbursementPayments(ctx context.Context, tx pgx.Tx, week string, ids, amounts []string) error {
	var frozen bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM unnest($2::uuid[],$3::text[]) v(id,amount)
 JOIN expenses e ON e.id=v.id
 JOIN investor_pay_weeks w ON w.week_start=$1::date AND w.truck_id=e.truck_id AND w.owner_id=e.owner_id AND w.finalized
 LEFT JOIN expense_reimbursement_payments p ON p.expense_id=e.id AND p.week_start=w.week_start
 WHERE NOT coalesce((to_jsonb(e)->>'owner_recovery')::boolean,false)
 AND lower(btrim(e.covered_by))='truck owner' AND e.owner_id<>'`+CompanyOwnerID+`'
 AND v.amount::numeric<>coalesce(p.amount,0))`, week, ids, amounts).Scan(&frozen)
	if err != nil {
		return err
	}
	if frozen {
		return chargeInvalid("Reopen the truck owner's Investor Pay statement before changing this reimbursement")
	}
	return nil
}

func (r *ExpenseRepository) loadReimbursementBalances(ctx context.Context, values []Expense) error {
	ids := []string{}
	accessorialIDs := []string{}
	for i, e := range values {
		if (e.OwnerRecovery || normalizedExpenseText(e.PaymentType) == "driver payroll") && normalizedExpenseText(e.CoveredBy) == "truck owner" {
			values[i].OwnerCollectionPaid, values[i].OwnerCollectionRemaining = e.PaidAmount, e.RemainingAmount
		}
		if normalizedExpenseText(e.PaymentType) == "driver payroll" {
			accessorialIDs = append(accessorialIDs, e.ID)
		}
		if e.PaymentType != nil && strings.EqualFold(strings.TrimSpace(*e.PaymentType), "reimburse") {
			ids = append(ids, e.ID)
		}
	}
	if err := r.loadAccessorialBalances(ctx, values, accessorialIDs); err != nil {
		return err
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
	rows, err := r.pool.Query(ctx, `SELECT e.id::text,(expense_external_total(e.id)+coalesce(sum(p.amount),0))::text,(e.amount-expense_external_total(e.id)-coalesce(sum(p.amount),0))::text,
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
				values[i].DriverPayoutPaid, values[i].DriverPayoutRemaining = &paid, &left
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
	if rows, ok := payrollBatchedDeductions(ctx, tx, drivers, week, "reimburse"); ok {
		return rows, nil
	}
	result := map[string][]ExpenseDeduction{}
	available, err := reimbursementLedgerAvailable(ctx, tx)
	if err != nil || !available {
		return result, err
	}
	rows, err := tx.Query(ctx, `WITH relevant AS MATERIALIZED (
 SELECT e.*,expense_external_total(e.id) external_paid FROM expenses e WHERE accounting_live_source('expense',e.id,e.expense_date) AND driver_id=ANY($1::uuid[]) AND lower(btrim(payment_type))='reimburse' AND expense_date<$2::date+10 AND amount>=0
 ), payments AS (SELECT p.expense_id,sum(p.amount) FILTER(WHERE p.week_start<>$2::date) other,sum(p.amount) FILTER(WHERE p.week_start<$2::date) prior FROM expense_reimbursement_payments p JOIN relevant e ON e.id=p.expense_id GROUP BY p.expense_id)
 SELECT e.driver_id::text,e.id,coalesce(nullif(btrim(e.expense_type),''),e.category),e.category,e.expense_date::text,e.amount::text,
 (e.amount-e.external_paid-coalesce(p.other,0))::text,coalesce(w.amount,e.amount-e.external_paid-coalesce(p.other,0))::text,
 (e.amount-e.external_paid-coalesce(p.prior,0)-coalesce(w.amount,0))::text,(e.amount-e.external_paid-coalesce(p.prior,0))::text,e.balance_version,w.expense_id IS NOT NULL
 FROM relevant e LEFT JOIN payments p ON p.expense_id=e.id LEFT JOIN expense_reimbursement_payments w ON w.expense_id=e.id AND w.week_start=$2::date
 WHERE w.expense_id IS NOT NULL OR e.amount-e.external_paid>coalesce(p.other,0) ORDER BY e.expense_date,e.id`, drivers, week)
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
