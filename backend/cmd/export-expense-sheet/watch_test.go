package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestWatchCommittedEntriesAndRecovery(t *testing.T) {
	dsn := os.Getenv("MSERP_EXPENSE_SHEET_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_EXPENSE_SHEET_TEST_DATABASE_URL")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	schema := fmt.Sprintf("sheet_watch_%d", time.Now().UnixNano())
	exec := func(sql string) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema + ",public")
	defer conn.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	exec(`CREATE TABLE expense_sheet_export_config(enabled boolean,singleton boolean); INSERT INTO expense_sheet_export_config VALUES(true,true); CREATE TABLE expense_sheet_exports(id int,delivered_at timestamptz)`)
	migration, err := os.ReadFile("../../sql/107_expense_sheet_notify.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	exec("INSERT INTO expense_sheet_exports VALUES(1,NULL)") // saved before worker starts
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	calls := make(chan struct{}, 10)
	done := make(chan error, 1)
	go func() {
		done <- watchExports(ctx, u.String(), time.Hour, func(ctx context.Context) error {
			// Independent connection because the test holds source transactions open.
			c, err := pgx.Connect(ctx, u.String())
			if err != nil {
				return err
			}
			defer c.Close(context.Background())
			_, err = c.Exec(ctx, "UPDATE expense_sheet_exports SET delivered_at=now() WHERE delivered_at IS NULL")
			calls <- struct{}{}
			return err
		})
	}()
	wait := func() {
		t.Helper()
		select {
		case <-calls:
		case err := <-done:
			t.Fatalf("worker stopped: %v", err)
		case <-ctx.Done():
			t.Fatal("worker did not deliver")
		}
	}
	quiet := func() {
		t.Helper()
		select {
		case <-calls:
			t.Fatal("uncommitted/rolled-back entry woke worker")
		case <-time.After(100 * time.Millisecond):
		}
	}
	wait() // startup backlog
	exec("BEGIN; INSERT INTO expense_sheet_exports VALUES(2,NULL)")
	quiet()
	exec("COMMIT")
	wait()
	exec("BEGIN; INSERT INTO expense_sheet_exports VALUES(3,NULL); ROLLBACK")
	quiet()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// A failed attempt retries even without another save or notification.
	retryCtx, retryCancel := context.WithTimeout(context.Background(), time.Second)
	defer retryCancel()
	attempts := 0
	err = watchExports(retryCtx, u.String(), 10*time.Millisecond, func(context.Context) error {
		attempts++
		if attempts == 2 {
			retryCancel()
		}
		return errors.New("temporary outage")
	})
	if err != nil || attempts != 2 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPacingHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", "https://example.test", nil)
	p := pacedTransport{gate: &requestGate{next: time.Now().Add(time.Hour)}, base: transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("canceled request sent"); return nil, nil })}
	if _, err := p.RoundTrip(r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
