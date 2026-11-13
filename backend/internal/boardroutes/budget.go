package boardroutes

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBudgetExhausted = errors.New("monthly truck routing allowance exhausted")

// Budget durably counts the attempt before HTTP dispatch.
type Budget interface {
	Reserve(context.Context, Options) error
}

type PostgresBudget struct{ pool *pgxpool.Pool }

func NewPostgresBudget(pool *pgxpool.Pool) *PostgresBudget { return &PostgresBudget{pool: pool} }

func (b *PostgresBudget) Reserve(ctx context.Context, o Options) error {
	err := b.reserve(ctx, o, time.Time{})
	if err != nil && !errors.Is(err, ErrBudgetExhausted) {
		return errors.New("routing usage accounting unavailable")
	}
	return err
}

func (b *PostgresBudget) reserve(ctx context.Context, o Options, testNow time.Time) error {
	return b.reserveTomTom(ctx, o, testNow, "tomtom", o.TomTomMonthlyFree, o.PaidProvider == "tomtom", ErrBudgetExhausted)
}

// Test clocks let billing-boundary tests exercise the same transaction. Production
// reads the database clock after the lock so queued requests use the correct month.
// Routing and each geocoding operation keep independent counters in the same ledger.
func (b *PostgresBudget) reserveTomTom(ctx context.Context, o Options, testNow time.Time, provider string, monthlyLimit int, allowPaid bool, exhausted error) error {
	if o.TomTomAPIKey == "" || (monthlyLimit <= 0 && !allowPaid) {
		return exhausted
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
	err = tx.QueryRow(ctx, `SELECT requests FROM board_route_usage WHERE provider=$1 AND month_start=$2::date`, provider, month).Scan(&requests)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	paid := 0
	if requests >= int64(monthlyLimit) {
		if !allowPaid {
			return exhausted
		}
		paid = 1
	}
	_, err = tx.Exec(ctx, `INSERT INTO board_route_usage(provider,month_start,requests,paid_requests,last_attempt_at)
	 VALUES($1,$2::date,1,$3,$4) ON CONFLICT(provider,month_start) DO UPDATE
	 SET requests=board_route_usage.requests+1,paid_requests=board_route_usage.paid_requests+EXCLUDED.paid_requests,last_attempt_at=EXCLUDED.last_attempt_at`, provider, month, paid, now)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
