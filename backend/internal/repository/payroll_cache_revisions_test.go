package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPayrollRevisionCacheDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated test database required")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("payroll_cache_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	exec := func(q string) {
		t.Helper()
		if _, err := admin.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; CREATE TABLE source(id int PRIMARY KEY, amount int); CREATE TABLE auth_sessions(id int); CREATE TABLE custom_tasks(id int); CREATE TABLE five_eld_locations(id int); CREATE TABLE dispatcher_extension_access(id int); INSERT INTO source VALUES(1,100);`)
	defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
	migration, err := os.ReadFile("../../sql/090_payroll_cache_revisions.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewDriverPayRepository(pool)
	fills := 0
	week := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	get := func(tx pgx.Tx) string {
		t.Helper()
		v, err := repo.cachedWeek(ctx, tx, "driver", week, func() (DriverPayWeek, error) {
			fills++
			var amount string
			err := tx.QueryRow(ctx, `SELECT amount::text FROM source WHERE id=1`).Scan(&amount)
			return DriverPayWeek{Revision: amount}, err
		})
		if err != nil {
			t.Fatal(err)
		}
		return v.Revision
	}
	begin := func() pgx.Tx {
		t.Helper()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}
	fresh := func() string { tx := begin(); defer tx.Rollback(ctx); return get(tx) }
	if fresh() != "100" || fills != 1 {
		t.Fatal("initial fill")
	}
	exec(`INSERT INTO auth_sessions VALUES(1)`)
	if fresh() != "100" || fills != 1 {
		t.Fatal("session invalidated financial cache")
	}
	exec(`INSERT INTO custom_tasks VALUES(1); INSERT INTO five_eld_locations VALUES(1); INSERT INTO dispatcher_extension_access VALUES(1)`)
	if fresh() != "100" || fills != 1 {
		t.Fatal("unrelated operational commits invalidated payroll")
	}
	exec(`UPDATE source SET amount=999 WHERE id=99`)
	if fresh() != "100" || fills != 1 {
		t.Fatal("empty worker update invalidated cache")
	}
	exec(`BEGIN;UPDATE source SET amount=200;ROLLBACK`)
	if fresh() != "100" || fills != 1 {
		t.Fatal("rollback invalidated cache")
	}
	pending := begin()
	defer pending.Rollback(ctx)
	if get(pending) != "100" {
		t.Fatal("pending read")
	}
	exec(`UPDATE source SET amount=300`)
	if get(pending) != "100" || fills != 1 {
		t.Fatal("repeatable read used future data")
	}
	if fresh() != "300" || fills != 2 {
		t.Fatal("committed write did not invalidate")
	}
	exec(`CREATE TABLE future_input(id int)`)
	_ = fresh()
	before := fills
	exec(`INSERT INTO future_input VALUES(1)`)
	_ = fresh()
	if fills != before+1 {
		t.Fatal("untracked future table failed safe fallback")
	}
}
