package repository

import (
	"context"
	"strings"
)

type EscrowOpeningInput struct {
	OpeningPaid string `json:"openingPaid"`
	Version     int    `json:"version"`
	Reason      string `json:"reason"`
}

func (r *EscrowRepository) SaveOpening(ctx context.Context, id, actor string, input EscrowOpeningInput) error {
	amount, err := chargeCents(input.OpeningPaid)
	input.Reason = strings.TrimSpace(input.Reason)
	if err != nil || amount < 0 || amount > 99999999999999 || input.Version < 1 || input.Reason == "" || len([]rune(input.Reason)) > 5000 || strings.ContainsRune(input.Reason, 0) {
		return chargeInvalid("Enter a nonnegative previously paid amount and a correction reason")
	}
	tx, err := r.pool.Begin(ctx)
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
	var before string
	var version int
	if err = tx.QueryRow(ctx, `SELECT opening_paid::text,balance_version FROM driver_escrows WHERE id=$1 FOR UPDATE`, id).Scan(&before, &version); err != nil {
		return err
	}
	if version != input.Version {
		return ErrEscrowReleaseConflict
	}
	beforeCents, _ := chargeCents(before)
	if beforeCents == amount {
		return nil
	}
	// Existing funding/collection triggers reject corrections that invalidate
	// saved payments or releases. Finalized statement snapshots stay unchanged.
	if _, err = tx.Exec(ctx, `UPDATE driver_escrows SET opening_paid=$2::numeric,balance_version=balance_version+1,updated_at=now() WHERE id=$1`, id, input.OpeningPaid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO driver_escrow_opening_events(escrow_id,actor_id,before_amount,after_amount,reason) VALUES($1,$2,$3::numeric,$4::numeric,$5)`, id, actor, before, input.OpeningPaid, input.Reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
