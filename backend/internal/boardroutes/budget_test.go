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

type budgetFunc func(context.Context, Options) (string, error)

func (f budgetFunc) Reserve(ctx context.Context, o Options) (string, error) { return f(ctx, o) }

func TestProviderOrder(t *testing.T) {
	o := Options{TomTomAPIKey: "tom", APIKey: "google", TomTomMonthlyFree: 20000, GoogleMonthlyFree: 10000, PaidProvider: "tomtom"}
	for _, tc := range []struct {
		tom, google int64
		provider    string
		paid        bool
	}{
		{0, 0, "tomtom", false}, {19999, 0, "tomtom", false}, {20000, 0, "google", false},
		{20000, 9999, "google", false}, {20000, 10000, "tomtom", true}, {25000, 10000, "tomtom", true},
	} {
		p, paid := chooseProvider(o, map[string]int64{"tomtom": tc.tom, "google": tc.google})
		if p != tc.provider || paid != tc.paid {
			t.Fatal(tc, p, paid)
		}
	}
	o.PaidProvider = "off"
	if p, _ := chooseProvider(o, map[string]int64{"tomtom": 20000, "google": 10000}); p != "" {
		t.Fatal("overage without opt-in", p)
	}
	o.PaidProvider = "google"
	if p, paid := chooseProvider(o, map[string]int64{"tomtom": 20000, "google": 10000}); p != "google" || !paid {
		t.Fatal("paid provider choice", p, paid)
	}
}

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
	o := Options{TomTomAPIKey: "tom", APIKey: "google", TomTomMonthlyFree: 20, GoogleMonthlyFree: 10, PaidProvider: "tomtom", TomTomBillingTimezone: "UTC"}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := NewPostgresBudget(pool).reserve(ctx, o, now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var requests, paid int
	for _, tc := range []struct {
		p              string
		requests, paid int
	}{{"tomtom", 50, 30}, {"google", 10, 0}} {
		if err = pool.QueryRow(ctx, `SELECT requests,paid_requests FROM board_route_usage WHERE provider=$1`, tc.p).Scan(&requests, &paid); err != nil || requests != tc.requests || paid != tc.paid {
			t.Fatal("concurrent reservations", tc, requests, paid, err)
		}
	}
	o.PaidProvider = "off"
	if _, err = NewPostgresBudget(pool).reserve(ctx, o, now); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatal("restart lost counters", err)
	}
	// TomTom resets at UTC midnight; Google remains in November until Pacific midnight.
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if p, err := NewPostgresBudget(pool).reserve(ctx, o, dec); err != nil || p != "tomtom" {
		t.Fatal("TomTom month reset", p, err)
	}
	o.TomTomMonthlyFree = 0
	if _, err = NewPostgresBudget(pool).reserve(ctx, o, dec); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatal("Google reset too early", err)
	}
	if p, err := NewPostgresBudget(pool).reserve(ctx, o, dec.Add(8*time.Hour)); err != nil || p != "google" {
		t.Fatal("Google month reset", p, err)
	}
	// Exercise the real client and ledger together across process-style restarts.
	// Even an HTTP failure consumes a reservation; no same-call provider replay.
	if _, err = pool.Exec(ctx, `TRUNCATE board_route_usage`); err != nil {
		t.Fatal(err)
	}
	o.TomTomMonthlyFree, o.GoogleMonthlyFree, o.PaidProvider = 2, 1, "tomtom"
	var sequence []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider := "tomtom"
		if r.Method == http.MethodPost {
			provider = "google"
		}
		sequence = append(sequence, provider)
		if len(sequence) == 1 {
			w.WriteHeader(500)
			return
		}
		if provider == "google" {
			_, _ = w.Write([]byte(`{"routes":[{"duration":"60s","distanceMeters":1000}]}`))
		} else {
			_, _ = w.Write([]byte(`{"routes":[{"summary":{"travelTimeInSeconds":60,"lengthInMeters":1000}}]}`))
		}
	}))
	defer server.Close()
	for i := 0; i < 4; i++ {
		client := NewClient(o)
		client.URL, client.TomTomURL, client.Budget = server.URL, server.URL, NewPostgresBudget(pool)
		_, err := client.Route(ctx, 40, -74, 41, -73)
		if (err != nil) != (i == 0) {
			t.Fatal("routing result", i, err)
		}
	}
	if strings.Join(sequence, ",") != "tomtom,tomtom,google,tomtom" {
		t.Fatal("provider sequence", sequence)
	}
	if err = pool.QueryRow(ctx, `SELECT sum(requests),sum(paid_requests) FROM board_route_usage`).Scan(&requests, &paid); err != nil || requests != 4 || paid != 1 {
		t.Fatal("HTTP accounting", requests, paid, err)
	}
	// A no-route fallback has two separately committed attempts; exhausted Google
	// allowance may not silently turn paid-TomTom permission into paid Google.
	if _, err = pool.Exec(ctx, `TRUNCATE board_route_usage`); err != nil {
		t.Fatal(err)
	}
	fallbackCalls := []string{}
	fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fallbackCalls = append(fallbackCalls, "tomtom")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"detailedError":{"code":"NO_ROUTE_FOUND"}}`))
			return
		}
		fallbackCalls = append(fallbackCalls, "google")
		_, _ = w.Write([]byte(`{"routes":[{"duration":"60s","distanceMeters":1000}]}`))
	}))
	defer fallbackServer.Close()
	o.TomTomMonthlyFree, o.GoogleMonthlyFree, o.PaidProvider, o.RequestsPerMinute = 1, 1, "tomtom", 120
	for i := 0; i < 2; i++ {
		client := NewClient(o)
		client.URL, client.TomTomURL, client.Budget = fallbackServer.URL, fallbackServer.URL, NewPostgresBudget(pool)
		_, routeErr := client.Route(ctx, 40, -74, 41, -73)
		if i == 0 && routeErr != nil {
			t.Fatal("counted fallback failed", routeErr)
		}
		if i == 1 && (!errors.Is(routeErr, ErrBudgetExhausted) || !errors.Is(routeErr, ErrTomTomNoRoute)) {
			t.Fatal("Google exhausted fallback policy", routeErr)
		}
	}
	if strings.Join(fallbackCalls, ",") != "tomtom,google,tomtom" {
		t.Fatal("unmetered or disallowed fallback", fallbackCalls)
	}
	for _, want := range []struct {
		provider       string
		requests, paid int
	}{{"tomtom", 2, 1}, {"google", 1, 0}} {
		if err = pool.QueryRow(ctx, `SELECT requests,paid_requests FROM board_route_usage WHERE provider=$1`, want.provider).Scan(&requests, &paid); err != nil || requests != want.requests || paid != want.paid {
			t.Fatal("fallback ledger", want, requests, paid, err)
		}
	}
	if _, err = pool.Exec(ctx, `DROP TABLE board_route_usage`); err != nil {
		t.Fatal(err)
	}
	if _, err = NewPostgresBudget(pool).Reserve(ctx, o); err == nil {
		t.Fatal("missing ledger failed open")
	}
}
