package repository

import (
	"context"
	"errors"
	"math/big"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Positive amounts are deductions. Suggested rows are read-only until Apply is
// submitted; a saved zero explicitly defers the expense for that week.
type ExpenseDeduction struct {
	ExpenseID      string `json:"expenseId"`
	Name           string `json:"name"`
	ExpenseDate    string `json:"expenseDate"`
	Total          string `json:"total"`
	Available      string `json:"available"`
	Amount         string `json:"amount"`
	Remaining      string `json:"remaining"`
	OpeningBalance string `json:"openingBalance"`
	Version        int    `json:"version"`
	Saved          bool   `json:"saved"`
	Apply          bool   `json:"apply,omitempty"`
}

func expenseDeductions(ctx context.Context, tx pgx.Tx, driver, week string) ([]ExpenseDeduction, error) {
	rows, err := tx.Query(ctx, `SELECT e.id, concat_ws(' · ',e.category,coalesce(nullif(e.expense_type,''),nullif(e.description,''),e.reference_number)),
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
 WHERE e.charge_driver_id=$1 AND NOT e.driver_settled
 AND e.expense_date<$2::date+7 AND e.amount>=0
 AND (w.expense_id IS NOT NULL OR e.amount>coalesce(p.other,0))
 ORDER BY e.expense_date,e.id`, driver, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExpenseDeduction{}
	for rows.Next() {
		var d ExpenseDeduction
		if err := rows.Scan(&d.ExpenseID, &d.Name, &d.ExpenseDate, &d.Total, &d.Available, &d.Amount, &d.Remaining, &d.OpeningBalance, &d.Version, &d.Saved); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func saveExpenseDeductions(ctx context.Context, tx pgx.Tx, driver, week, actor string, input []ExpenseDeduction) ([]ExpenseDeduction, error) {
	// Match the expense editor's row lock. Stable order also avoids deadlocks when
	// two requests overlap multiple expenses across different report weeks.
	sorted := append([]ExpenseDeduction(nil), input...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ExpenseID < sorted[j].ExpenseID })
	seen := map[string]bool{}
	for _, item := range sorted {
		if seen[item.ExpenseID] {
			return nil, chargeInvalid("Duplicate expense deduction")
		}
		seen[item.ExpenseID] = true
		if !item.Apply {
			continue
		}
		var version int
		err := tx.QueryRow(ctx, `SELECT balance_version FROM expenses WHERE id=$1 AND charge_driver_id=$2 FOR UPDATE`, item.ExpenseID, driver).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && version != item.Version) {
			return nil, ErrDriverPayConflict
		}
		if err != nil {
			return nil, err
		}
	}
	current, err := expenseDeductions(ctx, tx, driver, week)
	if err != nil {
		return nil, err
	}
	byID := map[string]ExpenseDeduction{}
	for _, item := range current {
		byID[item.ExpenseID] = item
	}
	for _, item := range sorted {
		if !item.Apply {
			continue
		}
		old, ok := byID[item.ExpenseID]
		if !ok {
			return nil, ErrDriverPayConflict
		}
		amount, valid := new(big.Rat).SetString(item.Amount)
		limit, _ := new(big.Rat).SetString(old.Available)
		if !valid || amount.Sign() < 0 || amount.Cmp(limit) > 0 || new(big.Rat).Mul(amount, big.NewRat(100, 1)).Denom().Cmp(big.NewInt(1)) != 0 {
			return nil, chargeInvalid("Expense deduction must be between zero and the available balance (%s)", old.Available)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO expense_payments(expense_id,week_start,amount,updated_by)
   VALUES($1,$2::date,$3::numeric,nullif($4,'')::uuid)
   ON CONFLICT(expense_id,week_start) DO UPDATE SET amount=excluded.amount,updated_by=excluded.updated_by,updated_at=now()`, item.ExpenseID, week, item.Amount, actor); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE expenses SET updated_at=now() WHERE id=$1`, item.ExpenseID); err != nil {
			return nil, err
		}
	}
	return expenseDeductions(ctx, tx, driver, week)
}
