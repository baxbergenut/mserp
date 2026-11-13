package boardroutes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type budgetFunc func(context.Context, Options) error

func (f budgetFunc) Reserve(ctx context.Context, o Options) error { return f(ctx, o) }

func TestRoutingUsageDatabase(t *testing.T) {
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
	schema := fmt.Sprintf("board_route_budget_%d", time.Now().UnixNano())
	q := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+q+`; SET search_path TO `+q+`; GRANT USAGE ON SCHEMA `+q+` TO mserp_app;
	 CREATE TABLE payroll_projection_sources(table_name text,classification text); CREATE TABLE payroll_cache_revisions(table_name text,revision bigint);`); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), `DROP SCHEMA `+q+` CASCADE`)
	migration, err := os.ReadFile("../../sql/197_board_route_usage.sql")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil || !strings.Contains(strings.ReplaceAll(string(source), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n")) {
		t.Fatal("migration/fresh schema mismatch", err)
	}
	if _, err = admin.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	var owner, classification string
	if err = admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename='board_route_usage'`, schema).Scan(&owner); err != nil || owner != "mserp_app" {
		t.Fatal("owner", owner, err)
	}
	if err = admin.QueryRow(ctx, `SELECT classification FROM payroll_projection_sources WHERE table_name='board_route_usage'`).Scan(&classification); err != nil || classification != "unrelated" {
		t.Fatal("payroll classification", classification, err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	o := Options{TomTomAPIKey: "tom", TomTomMonthlyFree: 20, PaidProvider: "tomtom", TomTomBillingTimezone: "UTC"}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := NewPostgresBudget(pool).reserve(ctx, o, now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var requests, paid int
	if err = pool.QueryRow(ctx, `SELECT requests,paid_requests FROM board_route_usage WHERE provider='tomtom'`).Scan(&requests, &paid); err != nil || requests != 60 || paid != 40 {
		t.Fatal("concurrent reservations", requests, paid, err)
	}
	o.PaidProvider = "off"
	if err = NewPostgresBudget(pool).reserve(ctx, o, now); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatal("restart lost counters", err)
	}
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if err = NewPostgresBudget(pool).reserve(ctx, o, dec); err != nil {
		t.Fatal("TomTom month reset", err)
	}

	// Exercise the real client and ledger together across process-style restarts.
	if _, err = pool.Exec(ctx, `TRUNCATE board_route_usage`); err != nil {
		t.Fatal(err)
	}
	o.TomTomMonthlyFree, o.PaidProvider = 2, "tomtom"
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assertTruckProfile(t, r)
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"routes":[{"summary":{"travelTimeInSeconds":60,"lengthInMeters":1000}}]}`))
	}))
	defer server.Close()
	for i := 0; i < 4; i++ {
		client := NewClient(o)
		client.TomTomURL, client.Budget = server.URL, NewPostgresBudget(pool)
		_, err := client.Route(ctx, 40, -74, 41, -73)
		if (err != nil) != (i == 0) {
			t.Fatal("routing result", i, err)
		}
	}
	if err = pool.QueryRow(ctx, `SELECT requests,paid_requests FROM board_route_usage WHERE provider='tomtom'`).Scan(&requests, &paid); err != nil || calls != 4 || requests != 4 || paid != 2 {
		t.Fatal("HTTP accounting", calls, requests, paid, err)
	}
	if _, err = pool.Exec(ctx, `DROP TABLE board_route_usage`); err != nil {
		t.Fatal(err)
	}
	if err = NewPostgresBudget(pool).Reserve(ctx, o); err == nil {
		t.Fatal("missing ledger failed open")
	}
}
