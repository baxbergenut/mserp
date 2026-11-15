package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrDriverIdentityConflict = errors.New("another driver profile has this name and phone or email. Open the existing profile to review it before continuing")

type driverIdentity struct {
	name  string
	phone string
	email string
}

// Compare the stored values so name parts and phone normalization are already
// applied. Callers hold the fleet status lock through the end of the write.
const driverIdentityFields = "normalized_name, coalesce(phone,''), lower(btrim(coalesce(email,'')))"

func checkDriverIdentity(ctx context.Context, tx pgx.Tx, id string, activeOnly bool) error {
	var duplicate bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM drivers d JOIN drivers other ON other.id<>d.id
		 AND other.normalized_name=d.normalized_name
		WHERE d.id=$1 AND d.normalized_name<>'' AND (NOT $2 OR other.active)
		  AND ((nullif(d.phone,'') IS NOT NULL AND d.phone=other.phone)
		    OR (nullif(btrim(d.email),'') IS NOT NULL AND lower(btrim(d.email))=lower(btrim(other.email))))
	)`, id, activeOnly).Scan(&duplicate)
	if err != nil {
		return err
	}
	if duplicate {
		return ErrDriverIdentityConflict
	}
	return nil
}
