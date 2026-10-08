package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPayrollSnapshotCacheDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated test database required")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("test database required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	repo := NewDriverPayRepository(pool)
	week := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	fills := 0
	read := func() (DriverPayWeek, error) {
		fills++
		return DriverPayWeek{WeekStart: "2026-10-05", Drivers: []DriverPayDriver{{ID: "fixture", FullName: "original"}}}, nil
	}
	begin := func() pgx.Tx {
		tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			t.Fatal(e)
		}
		return tx
	}
	get := func(tx pgx.Tx) DriverPayWeek {
		v, e := repo.cachedWeek(ctx, tx, "driver", week, read)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	tx := begin()
	defer tx.Rollback(ctx)
	a := get(tx)
	a.Drivers[0].FullName = "mutated editor"
	b := get(tx)
	if fills != 1 || b.Drivers[0].FullName != "original" {
		t.Fatal("cache did not isolate editors or reuse snapshot")
	}
	writer, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Rollback(ctx)
	var xid string
	if e = writer.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&xid); e != nil {
		t.Fatal(e)
	}
	pending := begin()
	_ = get(pending)
	before := fills
	if e = writer.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	_ = get(pending)
	if fills != before {
		t.Fatal("repeatable snapshot changed after concurrent commit")
	}
	_ = pending.Rollback(ctx)
	fresh := begin()
	defer fresh.Rollback(ctx)
	_ = get(fresh)
	if fills != before+1 {
		t.Fatal("commit failed to invalidate cached input snapshot")
	}
}
