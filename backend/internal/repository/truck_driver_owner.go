package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Selecting a driver as owner creates/reuses their ownership identity in the
// same transaction as the truck. The driver_id unique key prevents duplicates.
func resolveTruckDriverOwner(ctx context.Context, tx pgx.Tx, input *TruckInput) error {
	if input.OwnerDriverID == nil {
		return nil
	}
	if input.OwnerID != nil {
		return chargeInvalid("Choose one truck owner")
	}
	var owner string
	err := tx.QueryRow(ctx, `INSERT INTO investors(full_name,driver_id,email,phone)
 SELECT full_name,id,email,phone FROM drivers WHERE id=$1
 ON CONFLICT(driver_id) DO UPDATE SET active=true
 RETURNING id::text`, *input.OwnerDriverID).Scan(&owner)
	if err != nil {
		return mapNotFound(err)
	}
	input.OwnerID = &owner
	return nil
}
