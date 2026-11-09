package boardroutes

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrReverseGeocodeBudgetExhausted = errors.New("monthly reverse geocoding request limit reached")

func (b *PostgresBudget) ReserveReverseGeocode(ctx context.Context, o Options) error {
	err := b.reserveReverseGeocode(ctx, o, time.Time{})
	if err != nil && !errors.Is(err, ErrReverseGeocodeBudgetExhausted) {
		return errors.New("reverse geocoding usage accounting unavailable")
	}
	return err
}

// This is an operational request cap, not an assertion of an account's free
// allowance. Actual search billing is separate from the routing SKU.
func (b *PostgresBudget) reserveReverseGeocode(ctx context.Context, o Options, testNow time.Time) error {
	if o.ReverseGeocodeMonthlyLimit <= 0 || o.ReverseGeocodeMonthlyLimit > 1000 || o.TomTomAPIKey == "" {
		return ErrReverseGeocodeBudgetExhausted
	}
	zone, err := time.LoadLocation(o.TomTomBillingTimezone)
	if err != nil {
		return err
	}
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(718504116)`); err != nil {
		return err
	}
	now := testNow
	if now.IsZero() {
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
	}
	month := now.In(zone).Format("2006-01") + "-01"
	var requests int64
	err = tx.QueryRow(ctx, `SELECT requests FROM board_route_usage WHERE provider='tomtom_reverse' AND month_start=$1::date`, month).Scan(&requests)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if requests >= int64(o.ReverseGeocodeMonthlyLimit) {
		return ErrReverseGeocodeBudgetExhausted
	}
	// paid_requests is unknown for this separate SKU and is not used to classify
	// reverse calls. Every dispatched attempt is counted irrespective of outcome.
	_, err = tx.Exec(ctx, `INSERT INTO board_route_usage(provider,month_start,requests,paid_requests,last_attempt_at)
 VALUES('tomtom_reverse',$1::date,1,0,$2) ON CONFLICT(provider,month_start) DO UPDATE
 SET requests=board_route_usage.requests+1,last_attempt_at=EXCLUDED.last_attempt_at`, month, now)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
