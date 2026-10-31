package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPayrollCacheSchemaCoverageDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated test database required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.Database, "_test") || (cfg.Host != "localhost" && cfg.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	name := fmt.Sprintf("payroll_coverage_%d", time.Now().UnixNano())
	schema := pgx.Identifier{name}.Sanitize()
	exec := func(sql string) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE SCHEMA ` + schema + `; SET search_path TO ` + schema + `,public`)
	defer func() { _, _ = conn.Exec(ctx, `RESET ROLE; SET search_path TO public; DROP SCHEMA `+schema+` CASCADE`) }()
	body, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(body))
	exec(`GRANT USAGE ON SCHEMA ` + schema + ` TO mserp_app`)
	check := func(t *testing.T) {
		t.Helper()
		var missing []string
		err := conn.QueryRow(ctx, `SELECT coalesce(array_agg(t.tablename ORDER BY t.tablename),'{}') FROM pg_tables t
 WHERE t.schemaname=current_schema() AND t.tablename NOT IN ('payroll_cache_revisions','auth_sessions','user_login_activity','schema_migrations')
 AND (NOT EXISTS(SELECT 1 FROM payroll_cache_revisions r WHERE r.table_name=t.tablename)
 OR (NOT t.tablename=ANY($1::text[]) AND EXISTS(
 SELECT 1 FROM (VALUES(4),(8),(16),(32)) required(op)
 WHERE NOT EXISTS(SELECT 1 FROM pg_trigger g JOIN pg_proc p ON p.oid=g.tgfoid WHERE g.tgrelid=(quote_ident(t.schemaname)||'.'||quote_ident(t.tablename))::regclass AND p.proname LIKE 'invalidate_payroll%' AND g.tgenabled='O' AND (g.tgtype & required.op)=required.op))))`, payrollUnrelatedTables).Scan(&missing)
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) > 0 {
			t.Fatalf("unregistered/untracked payroll inputs: %v", missing)
		}
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SET LOCAL ROLE mserp_app`); err != nil {
			t.Fatal(err)
		}
		snapshot, err := payrollCacheSnapshot(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(snapshot, "snapshot:") {
			t.Fatal("current schema disables compact payroll")
		}
		if strings.Contains(snapshot, "status_color_") {
			t.Fatal("retired colors invalidate payroll")
		}
	}
	t.Run("fresh", func(t *testing.T) { check(t) })
	t.Run("incremental", func(t *testing.T) {
		for _, table := range []string{"status_color_settings", "status_color_history"} {
			for _, op := range []string{"insert", "update", "delete", "truncate"} {
				exec(`DROP TRIGGER payroll_cache_` + op + ` ON ` + table)
			}
		}
		exec(`DELETE FROM payroll_cache_revisions WHERE table_name IN ('status_color_settings','status_color_history')`)
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := payrollCacheSnapshot(ctx, tx)
		_ = tx.Rollback(ctx)
		if err != nil || !strings.HasPrefix(snapshot, "snapshot:") {
			t.Fatalf("missing input must fall back: %q %v", snapshot, err)
		}
		migration, err := os.ReadFile("../../sql/179_payroll_cache_coverage.sql")
		if err != nil {
			t.Fatal(err)
		}
		exec(string(migration))
		check(t)
	})
	t.Run("missing_event", func(t *testing.T) {
		exec(`DROP TRIGGER payroll_cache_delete ON gross_board_transfers; DROP TRIGGER payroll_cache_truncate ON payroll_load_placements`)
		migration, err := os.ReadFile("../../sql/180_payroll_revision_triggers.sql")
		if err != nil {
			t.Fatal(err)
		}
		exec(string(migration))
		check(t)
	})
	t.Run("load_revision_upgrade", func(t *testing.T) {
		exec(`DROP TRIGGER payroll_cache_update ON loads; DROP FUNCTION invalidate_payroll_load_cache();
 CREATE TRIGGER payroll_cache_update AFTER UPDATE ON loads REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION invalidate_payroll_cache();
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(18199,'UPGRADE','delivered',1,1,1,'{}')`)
		migration, err := os.ReadFile("../../sql/181_payroll_load_revision.sql")
		if err != nil {
			t.Fatal(err)
		}
		exec(string(migration))
		var before, after int64
		if err = conn.QueryRow(ctx, `SELECT revision FROM payroll_cache_revisions WHERE table_name='loads'`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE loads SET synced_at=now() WHERE id=18199`)
		if err = conn.QueryRow(ctx, `SELECT revision FROM payroll_cache_revisions WHERE table_name='loads'`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Fatal("upgraded trigger invalidates timestamp-only sync")
		}
		check(t)
	})
}
