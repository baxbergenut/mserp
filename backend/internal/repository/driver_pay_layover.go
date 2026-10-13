package repository

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

type DriverPaySettings struct {
	LayoverDailyAmount string `json:"layoverDailyAmount"`
	Version            int    `json:"version"`
}

type DriverPayLayover struct {
	Date   string `json:"date"`
	Amount string `json:"amount"`
}

func (r *DriverPayRepository) Settings(ctx context.Context) (DriverPaySettings, error) {
	var s DriverPaySettings
	err := r.pool.QueryRow(ctx, `SELECT layover_daily_amount::text,version FROM driver_pay_settings WHERE singleton`).Scan(&s.LayoverDailyAmount, &s.Version)
	return s, err
}

func (r *DriverPayRepository) SaveSettings(ctx context.Context, s DriverPaySettings, actor string) (DriverPaySettings, error) {
	cents, err := chargeCents(s.LayoverDailyAmount)
	if err != nil || cents < 0 || cents > 99999999999 || s.Version < 1 || s.Version > 2147483646 {
		return s, chargeInvalid("Enter a nonnegative daily layover amount with at most two decimals and a valid version")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return s, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`); err != nil {
		return s, err
	}
	err = tx.QueryRow(ctx, `UPDATE driver_pay_settings SET layover_daily_amount=$1::numeric,version=version+1,updated_at=now(),updated_by=nullif($3,'')::uuid WHERE singleton AND version=$2 RETURNING layover_daily_amount::text,version`, s.LayoverDailyAmount, s.Version, actor).Scan(&s.LayoverDailyAmount, &s.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrDriverPayConflict
	}
	if err != nil {
		return s, err
	}
	return s, tx.Commit(ctx)
}

// The source is a dated Gross Board status, never the persistent Status Board
// field. DISTINCT prevents extra slots from paying the same calendar day twice.
func driverPayLayovers(ctx context.Context, tx pgx.Tx, week string) (map[string][]DriverPayLayover, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT e.driver_id::text,e.service_date::text,s.layover_daily_amount::text
 FROM `+grossBoardEntriesSQL+` e CROSS JOIN driver_pay_settings s
 WHERE NOT e.deleted AND e.day_status='SHOP' AND e.service_date >= $1::date AND e.service_date < $1::date+7
 AND s.singleton AND s.layover_daily_amount>0 ORDER BY 1,2`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]DriverPayLayover{}
	for rows.Next() {
		var id string
		var item DriverPayLayover
		if err = rows.Scan(&id, &item.Date, &item.Amount); err != nil {
			return nil, err
		}
		result[id] = append(result[id], item)
	}
	return result, rows.Err()
}

func validateLayoverExclusions(week string, dates []string) error {
	start, err := time.Parse(time.DateOnly, week)
	if err != nil || len(dates) > 7 {
		return chargeInvalid("Invalid layover removal dates")
	}
	seen := map[string]bool{}
	for _, date := range dates {
		day, err := time.Parse(time.DateOnly, date)
		if err != nil || day.Before(start) || !day.Before(start.AddDate(0, 0, 7)) || seen[date] {
			return chargeInvalid("Layover removal dates must be unique dates in the selected week")
		}
		seen[date] = true
	}
	return nil
}

func driverLayoverTotal(d DriverPayDriver) int64 {
	var total int64
	for _, row := range d.Layovers {
		if !slices.Contains(d.Edits.LayoverExcludedDates, row.Date) {
			n, _ := chargeCents(row.Amount)
			total += n
		}
	}
	return total
}
