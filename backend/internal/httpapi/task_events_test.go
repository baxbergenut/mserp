package httpapi

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mserp/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLiveSnapshotSharingAndShutdown(t *testing.T) {
	h := newDatabaseEvents(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "test", "driver_board.read")
	h.snapshotEvent = "board"
	calls := 0
	h.snapshot = func(context.Context) ([]byte, error) { calls++; return []byte(`{"entries":[]}`), nil }
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			if frame := h.frame(context.Background(), "changed"); !strings.Contains(frame, `data: {"entries":[]}`) {
				t.Error("missing pushed data", frame)
			}
		}()
	}
	group.Wait()
	if calls != 1 {
		t.Fatal("subscribers did not share snapshot", calls)
	}
	h.publish()
	h.frame(context.Background(), "changed")
	if calls != 2 {
		t.Fatal("committed revision did not refresh snapshot", calls)
	}
	started := make(chan struct{})
	h.snapshot = func(ctx context.Context) ([]byte, error) { close(started); <-ctx.Done(); return nil, ctx.Err() }
	h.publish()
	finished := make(chan struct{})
	go func() { defer close(finished); h.frame(context.Background(), "changed") }()
	<-started
	h.shutdown()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("snapshot blocked server shutdown")
	}
}

func TestLiveSnapshotProjectsEachSubscriber(t *testing.T) {
	h := newDatabaseEvents(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "test", "driver_board.read")
	defer h.shutdown()
	h.snapshotEvent = "board"
	h.snapshot = func(context.Context) ([]byte, error) {
		return []byte(`{"drivers":[{"fullName":"Example","payType":"cpm","payRate":0.5,"address":"Private","cdlFileId":"secret"}],"originalGross":1234.56}`), nil
	}
	frame := func(permissions ...string) string {
		return h.frame(context.WithValue(context.Background(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{Permissions: permissions}}), "changed")
	}
	viewer := frame("driver_board.read")
	for _, field := range []string{"payType", "payRate", "address", "cdlFileId"} {
		if strings.Contains(viewer, field) {
			t.Fatalf("viewer received %s", field)
		}
	}
	if !strings.Contains(viewer, "1234.56") {
		t.Fatal("operational amount hidden")
	}
	financial := frame("driver_board.read", "driver_compensation.read")
	if !strings.Contains(financial, "payRate") || strings.Contains(financial, "address") {
		t.Fatal("incorrect compensation projection")
	}
	if strings.Contains(frame("driver_board.read"), "payRate") {
		t.Fatal("shared cache leaked previous subscriber data")
	}
}

func TestBoardEventStreamPresenceAndRevocation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	board := newDatabaseEvents(nil, logger, "mserp_driver_board", "driver_board.read")
	presence := newDatabaseEvents(nil, logger, "mserp_board_presence", "driver_board.read")
	board.cancel = func() {}
	presence.cancel = func() {}
	board.snapshotEvent = "board"
	var sourceRevision atomic.Int32
	board.snapshot = func(context.Context) ([]byte, error) {
		return []byte(fmt.Sprintf(`{"entries":[],"sourceRevision":%d}`, sourceRevision.Load())), nil
	}
	presence.snapshotEvent = "peers"
	presence.snapshot = func(context.Context) ([]byte, error) { return []byte(`[]`), nil }
	defer board.shutdown()
	defer presence.shutdown()
	hash := hashToken("board-session")
	store := &fakeAuthStore{sessions: map[string]repository.AuthSession{hash: {User: repository.AuthUser{Permissions: []string{"driver_board.read"}}}}}
	server := httptest.NewServer(board.serve(newAuthHandler(logger, store, AuthOptions{}), presence))
	defer server.Close()
	request, _ := http.NewRequest("GET", server.URL, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "board-session"})
	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("proxy buffering must be disabled")
	}
	scanner := bufio.NewScanner(response.Body)
	event := func(want string) {
		t.Helper()
		for scanner.Scan() {
			if scanner.Text() == "event: "+want {
				return
			}
		}
		t.Fatal("missing stream event", want, scanner.Err())
	}
	event("board")
	event("peers")
	presence.publish()
	event("peers")
	sourceRevision.Add(1)
	board.publish()
	event("board")
	// Publish synchronizes the revocation with the handler's next auth read.
	delete(store.sessions, hash)
	board.publish()
	event("unauthorized")
}

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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	for _, channel := range []string{"mserp_tasks", "mserp_driver_board", "mserp_board_presence", "mserp_gross_board", "mserp_gross_presence"} {
		h := newDatabaseEvents(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), channel, "driver_board.read")
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
		if _, err = tx.Exec(ctx, `SELECT pg_notify($1,'')`, channel); err != nil {
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
		if _, err = pool.Exec(ctx, `SELECT pg_notify($1,'')`, channel); err != nil {
			t.Fatal(err)
		}
		receive(a)
		receive(b)
		// A forced listener disconnect must reconnect and reconcile missed events.
		if _, err = pool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND query=$1 AND pid<>pg_backend_pid()`, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		receive(a)
		receive(b)
		unsubscribe()
		unsubscribeB()
		h.shutdown()
	}
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
