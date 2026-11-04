package boardroutes

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBudgetExhausted = errors.New("monthly routing allowances exhausted")

// Budget chooses a provider and durably counts the attempt before HTTP dispatch.
type Budget interface {
	Reserve(context.Context, Options) (string, error)
}

type PostgresBudget struct{ pool *pgxpool.Pool }

func NewPostgresBudget(pool *pgxpool.Pool) *PostgresBudget { return &PostgresBudget{pool: pool} }

func (b *PostgresBudget) Reserve(ctx context.Context, o Options) (string, error) {
	provider, err := b.reserve(ctx, o, time.Time{})
	if err != nil && !errors.Is(err, ErrBudgetExhausted) {
		// Database failures must never cause an unmetered provider call.
		return "", errors.New("routing usage accounting unavailable")
	}
	return provider, err
}

// testNow is only used by database tests. Production takes the database clock
// after acquiring the lock so queued requests cross billing boundaries correctly.
func (b *PostgresBudget) reserve(ctx context.Context, o Options, testNow time.Time) (string, error) {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(718504116)`); err != nil {
		return "", err
	}
	now := testNow
	if now.IsZero() {
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return "", err
		}
	}
	googleZone, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return "", err
	}
	tomtomZone, err := time.LoadLocation(o.TomTomBillingTimezone)
	if err != nil {
		return "", err
	}
	months := map[string]string{"google": now.In(googleZone).Format("2006-01") + "-01", "tomtom": now.In(tomtomZone).Format("2006-01") + "-01"}
	usage := map[string]int64{}
	for _, provider := range []string{"tomtom", "google"} {
		var n int64
		err = tx.QueryRow(ctx, `SELECT requests FROM board_route_usage WHERE provider=$1 AND month_start=$2::date`, provider, months[provider]).Scan(&n)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		usage[provider] = n
	}
	provider, paid := chooseProvider(o, usage)
	if provider == "" {
		return "", ErrBudgetExhausted
	}
	paidCount := 0
	if paid {
		paidCount = 1
	}
	_, err = tx.Exec(ctx, `INSERT INTO board_route_usage(provider,month_start,requests,paid_requests,last_attempt_at)
	 VALUES($1,$2::date,1,$3,$4) ON CONFLICT(provider,month_start) DO UPDATE
	 SET requests=board_route_usage.requests+1,paid_requests=board_route_usage.paid_requests+EXCLUDED.paid_requests,last_attempt_at=EXCLUDED.last_attempt_at`, provider, months[provider], paidCount, now)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return provider, nil
}

func chooseProvider(o Options, usage map[string]int64) (string, bool) {
	if o.TomTomAPIKey != "" && usage["tomtom"] < int64(o.TomTomMonthlyFree) {
		return "tomtom", false
	}
	if o.APIKey != "" && usage["google"] < int64(o.GoogleMonthlyFree) {
		return "google", false
	}
	// Operator selects the cheaper provider from verified account pricing.
	if o.PaidProvider == "tomtom" && o.TomTomAPIKey != "" {
		return "tomtom", true
	}
	if o.PaidProvider == "google" && o.APIKey != "" {
		return "google", true
	}
	return "", false
}
