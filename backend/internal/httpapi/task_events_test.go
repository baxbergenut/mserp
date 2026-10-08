package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	// NOTIFY channels are database-wide, not schema-scoped. Repository tests
	// running in another package must not look like rollback notifications here.
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	database := fmt.Sprintf("mserp_task_events_%d_test", time.Now().UnixNano())
	quoted := pgx.Identifier{database}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	}()
	cfg = cfg.Copy()
	cfg.ConnConfig.Database = database
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

// An active event stream must not consume the HTTP server's shutdown deadline.
func TestTaskEventsDrainOnServerShutdown(t *testing.T) {
	h := newTaskEvents(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// No database notifications are needed to exercise the open HTTP stream.
	h.cancel = func() {}
	server := httptest.NewUnstartedServer(h.serve(nil))
	server.Config.RegisterOnShutdown(h.shutdown)
	server.Start()
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test"})
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("stream did not open")
	}
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, response.Body); close(drained) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Config.Shutdown(ctx); err != nil {
		t.Fatalf("event stream blocked shutdown: %v", err)
	}
	select {
	case <-drained:
	case <-ctx.Done():
		t.Fatal("stream did not close")
	}
	h.shutdown() // Safe when shutdown hooks are invoked more than once.
}
