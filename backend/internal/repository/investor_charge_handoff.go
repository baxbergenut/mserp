package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func handoffInvestorDriverCharges(ctx context.Context, tx pgx.Tx, driver, actor string) error {
	_, err := tx.Exec(ctx, `SELECT handoff_investor_driver_charges($1::uuid,nullif($2,'')::uuid)`, driver, actor)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" {
		return chargeInvalid("%s", pgErr.Message)
	}
	return err
}
