package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const expenseOwnerExternalSQL = `CASE WHEN lower(btrim(e.payment_type))='reimburse' AND coalesce((to_jsonb(e)->>'owner_recovery')::boolean,false) THEN 0 ELSE expense_external_total(e.id) END`

func expenseEffectKey(row ExpenseDeduction) string {
	if row.Source == "" {
		return row.ExpenseID
	}
	return row.Source + ":" + row.ExpenseID
}

func draftExpenseEffect(row ExpenseDeduction) bool {
	return row.Source == "" || row.Source == "accessorial"
}

func accessorialLedgerAvailable(ctx context.Context, tx pgx.Tx) (bool, error) {
	var available bool
	err := tx.QueryRow(ctx, `SELECT to_regclass('expense_accessorial_payments') IS NOT NULL`).Scan(&available)
	return available, err
}

func expenseAccessorialCredits(ctx context.Context, tx pgx.Tx, drivers []string, week string) (map[string][]ExpenseDeduction, error) {
	result := map[string][]ExpenseDeduction{}
	available, err := accessorialLedgerAvailable(ctx, tx)
	if err != nil || !available {
		return result, err
	}
	body, err := json.Marshal([]payrollBalanceScope{{Week: week, Drivers: drivers}})
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, payrollExpenseBalanceBatchSQL("accessorial"), body)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var w, id string
		row := ExpenseDeduction{Source: "accessorial"}
		if err = rows.Scan(&w, &id, &row.ExpenseID, &row.Name, &row.Category, &row.ExpenseDate, &row.Total, &row.Available, &row.Amount, &row.Remaining, &row.OpeningBalance, &row.Version, &row.Saved); err != nil {
			return nil, err
		}
		result[id] = append(result[id], row)
	}
	return result, rows.Err()
}

func reverseAccessorialPayments(ctx context.Context, tx pgx.Tx, week string, cards []DriverPayDriver) error {
	for _, d := range cards {
		for _, row := range d.Edits.ExpenseDeductions {
			if row.Source != "accessorial" {
				continue
			}
			tag, err := tx.Exec(ctx, `DELETE FROM expense_accessorial_payments WHERE expense_id=$1 AND week_start=$2::date AND driver_id=$3 AND amount=$4::numeric`, row.ExpenseID, week, d.ID, row.Amount)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrDriverPayConflict
			}
		}
	}
	return nil
}

func (r *ExpenseRepository) loadAccessorialBalances(ctx context.Context, values []Expense, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.pool.Query(ctx, `SELECT e.id::text, coalesce(sum(p.amount),0)::text,(e.amount-coalesce(sum(p.amount),0))::text
 FROM expenses e LEFT JOIN expense_accessorial_payments p ON p.expense_id=e.id
 WHERE e.id=ANY($1::uuid[]) GROUP BY e.id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, paid, left string
		if err = rows.Scan(&id, &paid, &left); err != nil {
			return err
		}
		for i := range values {
			if values[i].ID == id {
				values[i].DriverPayoutPaid, values[i].DriverPayoutRemaining = &paid, &left
				values[i].PaidAmount, values[i].RemainingAmount = &paid, &left
				break
			}
		}
	}
	return rows.Err()
}

func createExpenseOnce(ctx context.Context, tx pgx.Tx, in ExpenseInput) (string, bool, error) {
	if in.AccessorialRequestID == "" {
		id, err := insertExpense(ctx, tx, in)
		return id, false, err
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return "", false, err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(payload))
	var id, existing string
	err = tx.QueryRow(ctx, `SELECT id::text,accessorial_request_hash FROM expenses WHERE accessorial_request_id=$1`, in.AccessorialRequestID).Scan(&id, &existing)
	if err == nil {
		if existing != fingerprint {
			return "", false, ErrExpenseSettingConflict
		}
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	id, err = insertExpense(ctx, tx, in)
	if err != nil {
		return "", false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE expenses SET accessorial_request_id=$1,accessorial_request_hash=$2 WHERE id=$3 AND payment_type='Driver payroll'`, in.AccessorialRequestID, fingerprint, id)
	if err == nil && tag.RowsAffected() != 1 {
		err = ErrExpensePaymentMethodInvalid
	}
	return id, false, err
}
