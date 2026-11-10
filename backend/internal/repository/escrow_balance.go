package repository

import (
	"context"
	"strings"
)

type EscrowBalanceInput struct {
	Balance string `json:"balance"`
	Version int    `json:"version"`
	Reason  string `json:"reason"`
}

func (r *EscrowRepository) ReduceBalance(ctx context.Context, id, actor string, input EscrowBalanceInput) error {
	balance, err := chargeCents(input.Balance)
	reason := strings.TrimSpace(input.Reason)
	if err != nil || balance < 0 || input.Version < 1 || reason == "" || len([]rune(reason)) > 5000 || strings.ContainsRune(reason, 0) {
		return chargeInvalid("Enter a nonnegative escrow balance and a correction reason")
	}
	tx, err := beginPayrollMutation(ctx, r.pool)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var driver string
	if err = tx.QueryRow(ctx, `SELECT coalesce(driver_id::text,'') FROM driver_escrows WHERE id=$1`, id).Scan(&driver); err != nil {
		return err
	}
	if driver == "" {
		return chargeInvalid("This escrow no longer has a driver")
	}
	if err = lockChargeDrivers(ctx, tx, []string{driver}); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRow(ctx, `SELECT balance_version FROM driver_escrows WHERE id=$1 AND driver_id=$2 FOR UPDATE`, id, driver).Scan(&version); err != nil {
		return err
	}
	if version != input.Version {
		return ErrEscrowReleaseConflict
	}
	var heldAmount string
	var managedInSheets bool
	if err = tx.QueryRow(ctx, `SELECT held_amount::text,managed_in_sheets FROM (`+escrowBalancesSQL+`) balances WHERE id=$1`, id).Scan(&heldAmount, &managedInSheets); err != nil {
		return err
	}
	if managedInSheets {
		return chargeInvalid("This escrow is managed in Sheets")
	}
	held, err := chargeCents(heldAmount)
	if err != nil {
		return err
	}
	if balance >= held {
		return chargeInvalid("Enter a balance below the current escrow balance")
	}
	_, err = tx.Exec(ctx, `INSERT INTO driver_escrow_releases(escrow_id,week_start,amount,updated_by,correction_reason)
 VALUES($1,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,$2::numeric,$3,$4)`, id, chargeMoney(held-balance), actor, reason)
	if err != nil {
		return err
	}
	return commitPayrollMutation(ctx, tx)
}
