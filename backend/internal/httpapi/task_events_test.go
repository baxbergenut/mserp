package httpapi

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTaskEventsCoalesce(t *testing.T) {
	a, b := make(chan struct{}, 1), make(chan struct{}, 1)
	h := &taskEvents{clients: map[chan struct{}]struct{}{a: {}, b: {}}}
	for range 100 {
		h.publish()
	}
	if len(a) != 1 || len(b) != 1 {
		t.Fatal("slow subscribers should coalesce without blocking")
	}
}

func TestTaskEventsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ACCESS_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("disposable local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	h := newTaskEvents(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	a, unsubscribe := h.subscribe()
	defer unsubscribe()
	b, unsubscribeB := h.subscribe()
	defer unsubscribeB()
	receive := func(ch chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatal("notification missing")
		}
	}
	receive(a)
	receive(b) // LISTEN is active before accepting mutations.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_notify('mserp_tasks','')`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a:
		t.Fatal("rollback notified clients")
	case <-time.After(100 * time.Millisecond):
	}
	if _, err = pool.Exec(ctx, `SELECT pg_notify('mserp_tasks','')`); err != nil {
		t.Fatal(err)
	}
	receive(a)
	receive(b)
	// A forced listener disconnect must reconnect and reconcile missed events.
	if _, err = pool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND query='LISTEN mserp_tasks' AND pid<>pg_backend_pid()`); err != nil {
		t.Fatal(err)
	}
	receive(a)
	receive(b)
}
