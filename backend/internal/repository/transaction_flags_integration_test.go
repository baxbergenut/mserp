package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Opt-in read-only smoke test against an existing populated MSERP database.
// Exercises the real page queries, date scans, and coverage enrichment together.
func TestTransactionFlagsDatabaseSmoke(t *testing.T) {
	dsn := os.Getenv("MSERP_FLAG_SMOKE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_FLAG_SMOKE_DATABASE_URL to test a populated database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	counts := map[string]int{}
	check := func(flag *TransactionFlag) {
		if flag == nil {
			counts["covered_or_excluded"]++
			return
		}
		counts[flag.Status]++
		if flag.Reason == "" || flag.BufferDays != 1 {
			t.Fatalf("invalid flag: %+v", flag)
		}
		if flag.Status == "review" && (flag.PreviousLoad == nil || flag.LoadsSyncedAt == nil) {
			t.Fatal("review without history/freshness evidence")
		}
	}
	fuel, err := NewFuelRepository(pool).ListTransactionsPage(ctx, FuelPageQuery{Pagination: Pagination{Page: 1, PageSize: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fuel.Items) == 0 {
		t.Fatal("smoke test requires fuel data")
	}
	for _, item := range fuel.Items {
		check(item.Flag)
	}
	t.Logf("fuel page: %v", counts)
	counts = map[string]int{}
	tolls, err := NewTollRepository(pool).ListTollsPage(ctx, TollPageQuery{Pagination: Pagination{Page: 1, PageSize: 100}, Unit: os.Getenv("MSERP_FLAG_SMOKE_UNIT")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tolls.Items) == 0 {
		t.Fatal("smoke test requires toll data")
	}
	for _, item := range tolls.Items {
		check(item.Flag)
	}
	t.Logf("toll page: %v", counts)
}
