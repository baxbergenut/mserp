package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ExpenseBalanceCorrection struct {
	ID        string    `json:"id"`
	Remaining string    `json:"remaining"`
	Previous  string    `json:"previous,omitempty"`
	Reason    string    `json:"reason"`
	Version   int       `json:"version"`
	Actor     string    `json:"actor,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

func (r *ExpenseRepository) ExpenseBalanceHistory(ctx context.Context, id string) ([]ExpenseBalanceCorrection, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.id::text,c.remaining::text,c.previous::text,c.reason,c.version,u.username,c.created_at FROM expense_balance_corrections c JOIN app_users u ON u.id=c.actor_id WHERE c.expense_id=$1 ORDER BY c.created_at DESC,c.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ExpenseBalanceCorrection{}
	for rows.Next() {
		var c ExpenseBalanceCorrection
		if err = rows.Scan(&c.ID, &c.Remaining, &c.Previous, &c.Reason, &c.Version, &c.Actor, &c.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (r *ExpenseRepository) CorrectExpenseBalance(ctx context.Context, id, actor string, c ExpenseBalanceCorrection) error {
	n, err := chargeCents(c.Remaining)
	if err != nil || n < 0 || c.Version < 1 || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 2000 {
		return chargeInvalid("Enter a nonnegative remaining balance, a reason and the current version")
	}
	if u, e := externalSettlementUUID(c.ID); e != nil || !u.Valid {
		return chargeInvalid("Provide a unique request ID")
	}
	c.Remaining = chargeMoney(n)
	c.Reason = strings.TrimSpace(c.Reason)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`); err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT expense_id=$2::uuid AND remaining=$3::numeric AND reason=$4 AND actor_id=$5::uuid AND version=$6 FROM expense_balance_corrections WHERE id=$1`, c.ID, id, c.Remaining, c.Reason, actor, c.Version).Scan(&same)
	if err == nil {
		if !same {
			return ErrDriverPayConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var version int
	if err = tx.QueryRow(ctx, `SELECT balance_version FROM expenses WHERE id=$1 FOR UPDATE`, id).Scan(&version); err != nil {
		return err
	}
	if version != c.Version {
		return ErrDriverPayConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO expense_balance_corrections(id,expense_id,remaining,previous,reason,version,actor_id) VALUES($1,$2,$3::numeric,0,$4,$5,$6)`, c.ID, id, c.Remaining, c.Reason, c.Version, actor)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
