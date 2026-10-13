package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrExpenseVerificationForbidden = errors.New("you can only remove your own verification or one made by the same or a lower verification role")

func CanRevokeExpenseVerification(actor AuthUser, verification ExpenseVerification) bool {
	return verification.UserID != nil && *verification.UserID == actor.ID || actor.Administrator ||
		verification.Role == "accountant" && strings.EqualFold(strings.TrimSpace(actor.RoleName), "Accountant")
}

// Lock the expense first for every operation. Approval checks the displayed
// content version; cancellation targets an immutable approval ID so it cannot
// accidentally remove a newer approval or touch the other role's check.
func (r *ExpenseRepository) VerifyExpense(ctx context.Context, id, categoryID, role string, updatedAt time.Time, actor AuthUser) (Expense, error) {
	if role != "admin" && role != "accountant" {
		return Expense{}, ErrExpenseVerificationForbidden
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Expense{}, err
	}
	defer tx.Rollback(ctx)
	var lockedID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM expenses WHERE id=$1 AND category_id=$2 AND updated_at=$3
 AND NOT CASE WHEN $4='admin' THEN manager_verified ELSE accounting_verified END FOR UPDATE`, id, categoryID, updatedAt, role).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, ErrAccessConflict
	}
	if err != nil {
		return Expense{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO expense_verifications(expense_id,verifier_role,user_id,user_name,verified_at) VALUES($1,$2,$3,$4,now())`, id, role, actor.ID, actor.Username)
	if err != nil {
		return Expense{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE expenses SET manager_verified=manager_verified OR $2='admin',accounting_verified=accounting_verified OR $2='accountant',updated_at=now() WHERE id=$1`, id, role)
	if err != nil {
		return Expense{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Expense{}, err
	}
	return r.GetExpense(ctx, id)
}

func (r *ExpenseRepository) UnverifyExpense(ctx context.Context, id, categoryID, verificationID string, actor AuthUser) (Expense, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Expense{}, err
	}
	defer tx.Rollback(ctx)
	var lockedID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM expenses WHERE id=$1 AND category_id=$2 FOR UPDATE`, id, categoryID).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, ErrAccessConflict
	}
	if err != nil {
		return Expense{}, err
	}
	var verification ExpenseVerification
	err = tx.QueryRow(ctx, `SELECT id::text,verifier_role,user_id::text FROM expense_verifications WHERE id=$1 AND expense_id=$2 AND revoked_at IS NULL FOR UPDATE`, verificationID, id).Scan(&verification.ID, &verification.Role, &verification.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, ErrAccessConflict
	}
	if err != nil {
		return Expense{}, err
	}
	if !CanRevokeExpenseVerification(actor, verification) {
		return Expense{}, ErrExpenseVerificationForbidden
	}
	_, err = tx.Exec(ctx, `UPDATE expense_verifications SET revoked_at=now(),revoked_by=$2,revoked_by_name=$3 WHERE id=$1`, verification.ID, actor.ID, actor.Username)
	if err != nil {
		return Expense{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE expenses SET manager_verified=manager_verified AND $2<>'admin',accounting_verified=accounting_verified AND $2<>'accountant',updated_at=now() WHERE id=$1`, id, verification.Role)
	if err != nil {
		return Expense{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Expense{}, err
	}
	return r.GetExpense(ctx, id)
}
