package repository

import (
	"context"
	"regexp"
	"strings"
)

var escrowBalanceDecimal = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,2})?$`)

type EscrowBalanceInput struct {
	Balance string `json:"balance"`
	Version int    `json:"version"`
	Reason  string `json:"reason"`
}

func (r *EscrowRepository) SaveBalance(ctx context.Context, id, actor string, input EscrowBalanceInput) error {
	reason := strings.TrimSpace(input.Reason)
	if !escrowBalanceDecimal.MatchString(input.Balance) || input.Version < 1 || reason == "" || len([]rune(reason)) > 5000 || strings.ContainsRune(reason, 0) {
		return chargeInvalid("Enter a nonnegative escrow balance and a correction reason")
	}
	balance, err := decimalToCents(input.Balance)
	if err != nil {
		return err
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
	if driver != "" {
		if err = lockChargeDrivers(ctx, tx, []string{driver}); err != nil {
			return err
		}
	}
	var version int
	if err = tx.QueryRow(ctx, `SELECT balance_version FROM driver_escrows WHERE id=$1 AND driver_id IS NOT DISTINCT FROM nullif($2,'')::uuid FOR UPDATE`, id, driver).Scan(&version); err != nil {
		return err
	}
	if version != input.Version {
		return ErrEscrowReleaseConflict
	}
	var heldAmount, requiredAmount string
	var managedInSheets bool
	if err = tx.QueryRow(ctx, `SELECT held_amount::text,amount::text,managed_in_sheets FROM (`+escrowBalancesSQL+`) balances WHERE id=$1`, id).Scan(&heldAmount, &requiredAmount, &managedInSheets); err != nil {
		return err
	}
	if managedInSheets {
		return chargeInvalid("This escrow is managed in Sheets")
	}
	held, err := decimalToCents(heldAmount)
	if err != nil {
		return err
	}
	required, err := decimalToCents(requiredAmount)
	if err != nil {
		return err
	}
	if balance > required {
		return chargeInvalid("The new balance cannot exceed the required escrow amount")
	}
	if balance == held {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO driver_escrow_releases(escrow_id,week_start,amount,updated_by,correction_reason)
 VALUES($1,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,$2::numeric,$3,$4)`, id, formatCents(held-balance), actor, reason)
	if err != nil {
		return err
	}
	return commitPayrollMutation(ctx, tx)
}
