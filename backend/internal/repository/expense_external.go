package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type ExternalExpenseSettlement struct {
	ID         string    `json:"id"`
	Amount     string    `json:"amount"`
	SettledOn  string    `json:"settledOn"`
	Reference  string    `json:"reference"`
	ReversesID string    `json:"reversesId,omitempty"`
	Version    int       `json:"version"`
	Actor      string    `json:"actor,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
}

func (r *ExpenseRepository) ExternalSettlements(ctx context.Context, id string) ([]ExternalExpenseSettlement, error) {
	rows, err := r.pool.Query(ctx, `SELECT x.id::text,x.amount::text,x.settled_on::text,x.reference,coalesce(x.reverses_id::text,''),coalesce(u.username,''),x.created_at FROM expense_external_settlements x LEFT JOIN app_users u ON u.id=x.actor_id WHERE expense_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExternalExpenseSettlement{}
	for rows.Next() {
		var x ExternalExpenseSettlement
		if err = rows.Scan(&x.ID, &x.Amount, &x.SettledOn, &x.Reference, &x.ReversesID, &x.Actor, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *ExpenseRepository) RecordExternalSettlement(ctx context.Context, id, actor string, x ExternalExpenseSettlement) error {
	if _, err := cutoverUUID(x.ID); err != nil {
		return chargeInvalid("Provide a unique request ID")
	}
	n, err := chargeCents(x.Amount)
	if err != nil || n <= 0 || strings.TrimSpace(x.Reference) == "" || len(x.Reference) > 2000 || x.Version < 1 {
		return chargeInvalid("Provide a positive amount, reference and current expense version")
	}
	if _, err = time.Parse(time.DateOnly, x.SettledOn); err != nil {
		return chargeInvalid("Provide the actual settlement date")
	}
	if x.ReversesID != "" {
		if _, err = cutoverUUID(x.ReversesID); err != nil {
			return chargeInvalid("Invalid reversal receipt")
		}
	}
	x.Amount = chargeMoney(n)
	x.Reference = strings.TrimSpace(x.Reference)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`); err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT expense_id=$2::uuid AND amount=$3::numeric AND settled_on=$4::date AND reference=$5 AND coalesce(reverses_id::text,'')=$6 FROM expense_external_settlements WHERE id=$1`, x.ID, id, x.Amount, x.SettledOn, x.Reference, x.ReversesID).Scan(&same)
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
	if version != x.Version {
		return ErrDriverPayConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO expense_external_settlements(id,expense_id,amount,settled_on,reference,reverses_id,actor_id) VALUES($1,$2,$3::numeric,$4::date,$5,nullif($6,'')::uuid,$7)`, x.ID, id, x.Amount, x.SettledOn, x.Reference, x.ReversesID, actor)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
