package boardroutes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReverseGeocodeUsageDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("requires disposable local _test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("board_reverse_budget_%d", time.Now().UnixNano())
	q := pgx.Identifier{schema}.Sanitize()
	_, err = admin.Exec(ctx, `CREATE SCHEMA `+q+`; SET search_path TO `+q+`; GRANT USAGE ON SCHEMA `+q+` TO mserp_app;
 CREATE TABLE payroll_projection_sources(table_name text,classification text); CREATE TABLE payroll_cache_revisions(table_name text,revision bigint);
 CREATE TABLE driver_board_automation(driver_id text PRIMARY KEY); CREATE TABLE driver_board(driver_id text,status text,destination text);`)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), `DROP SCHEMA `+q+` CASCADE`)
	for _, path := range []string{"../../sql/197_board_route_usage.sql", "../../sql/207_ready_location.sql"} {
		migration, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	o := Options{TomTomAPIKey: "test", TomTomBillingTimezone: "UTC", ReverseGeocodeMonthlyLimit: 3, PaidProvider: "tomtom"}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := NewPostgresBudget(pool).reserveReverseGeocode(ctx, o, now)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrReverseGeocodeBudgetExhausted) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 3 {
		t.Fatal("hard cap overshoot", accepted.Load())
	}
	var requests int
	if err = pool.QueryRow(ctx, `SELECT requests FROM board_route_usage WHERE provider='tomtom_reverse'`).Scan(&requests); err != nil || requests != 3 {
		t.Fatal(requests, err)
	}
	if err = NewPostgresBudget(pool).reserveReverseGeocode(ctx, o, now); !errors.Is(err, ErrReverseGeocodeBudgetExhausted) {
		t.Fatal("restart/paid routing bypassed cap", err)
	}
	december := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if err = NewPostgresBudget(pool).reserveReverseGeocode(ctx, o, december); err != nil {
		t.Fatal("monthly reset", err)
	}
	o.TomTomMonthlyFree = 1
	if provider, err := NewPostgresBudget(pool).reserve(ctx, o, now); err != nil || provider != "tomtom" {
		t.Fatal("reverse usage consumed routing allowance", provider, err)
	}
}
