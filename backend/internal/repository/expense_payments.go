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
	Source         string `json:"source,omitempty"`
	ExpenseID      string `json:"expenseId"`
	Name           string `json:"name"`
	Category       string `json:"category,omitempty"`
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
	all, err := expenseDeductionsBulk(ctx, tx, []string{driver}, week)
	values := all[driver]
	if values == nil {
		values = []ExpenseDeduction{}
	}
	return values, err
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
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT balance_version FROM driver_escrows WHERE id=$1 AND driver_id=$2 FOR UPDATE`, item.ExpenseID, driver).Scan(&version)
		}
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
		if old.Source == "escrow" {
			if _, err = tx.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount,updated_by)
    VALUES($1,$2::date,$3::numeric,nullif($4,'')::uuid)
    ON CONFLICT(escrow_id,week_start) DO UPDATE SET amount=excluded.amount,updated_by=excluded.updated_by,updated_at=now()`, item.ExpenseID, week, item.Amount, actor); err != nil {
				return nil, err
			}
			continue
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
