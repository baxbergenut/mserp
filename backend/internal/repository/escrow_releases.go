package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrEscrowReleaseConflict = errors.New("Escrow changed; refresh and try again")

type EscrowRelease struct {
	ID        string `json:"id"`
	WeekStart string `json:"weekStart"`
	Amount    string `json:"amount"`
	Cancelled bool   `json:"cancelled"`
	Version   int    `json:"version"`
	Editable  bool   `json:"editable"`
}

type EscrowReleaseInput struct {
	ID            string `json:"id"`
	WeekStart     string `json:"weekStart"`
	Amount        string `json:"amount"`
	Version       int    `json:"version"`
	EscrowVersion int    `json:"escrowVersion"`
	Cancelled     bool   `json:"cancelled"`
}

func (r *EscrowRepository) SaveRelease(ctx context.Context, escrow string, input EscrowReleaseInput, actor string) error {
	if _, err := chargeWeek(input.WeekStart); err != nil {
		return err
	}
	amount, amountErr := chargeCents(input.Amount)
	if amountErr != nil || amount <= 0 || amount > 99999999999999 || input.EscrowVersion < 1 || input.WeekStart < ChargeCurrentWeek() {
		return chargeInvalid("Enter a positive release amount and select the current or a future week")
	}
	if input.ID == "" || input.Version < 0 {
		return chargeInvalid("Invalid release identity")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var driver string
	if err = tx.QueryRow(ctx, `SELECT coalesce(driver_id::text,'') FROM driver_escrows WHERE id=$1`, escrow).Scan(&driver); err != nil {
		return err
	}
	if driver == "" {
		return chargeInvalid("This escrow no longer has a driver")
	}
	if err = lockChargeDrivers(ctx, tx, []string{driver}); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRow(ctx, `SELECT balance_version FROM driver_escrows WHERE id=$1 AND driver_id=$2 FOR UPDATE`, escrow, driver).Scan(&version); err != nil {
		return err
	}
	if version != input.EscrowVersion {
		return ErrEscrowReleaseConflict
	}
	if err = assertPayrollOpen(ctx, tx, driver, input.WeekStart); err != nil {
		return err
	}
	if input.Version == 0 {
		if input.Cancelled {
			return chargeInvalid("A new release cannot be cancelled")
		}
		tag, e := tx.Exec(ctx, `INSERT INTO driver_escrow_releases(id,escrow_id,week_start,amount,updated_by) VALUES($1,$2,$3::date,$4::numeric,nullif($5,'')::uuid) ON CONFLICT(id) DO NOTHING`, input.ID, escrow, input.WeekStart, input.Amount, actor)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrEscrowReleaseConflict
		}
	} else {
		tag, e := tx.Exec(ctx, `UPDATE driver_escrow_releases SET week_start=$3::date,amount=$4::numeric,cancelled=$5,updated_by=nullif($6,'')::uuid WHERE id=$1 AND escrow_id=$2 AND version=$7`, input.ID, escrow, input.WeekStart, input.Amount, input.Cancelled, actor, input.Version)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrEscrowReleaseConflict
		}
	}
	return tx.Commit(ctx)
}

func escrowReleaseCredits(ctx context.Context, tx pgx.Tx, driver, week string) ([]PayAutoCharge, error) {
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.amount::text FROM driver_escrow_releases r JOIN driver_escrows e ON e.id=r.escrow_id WHERE e.driver_id=$1 AND r.week_start=$2::date AND NOT r.cancelled ORDER BY r.id`, driver, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PayAutoCharge{}
	for rows.Next() {
		var id, amount string
		if err = rows.Scan(&id, &amount); err != nil {
			return nil, err
		}
		result = append(result, PayAutoCharge{Name: "Escrow release", Amount: amount, Source: "escrow-release:" + id})
	}
	return result, rows.Err()
}
