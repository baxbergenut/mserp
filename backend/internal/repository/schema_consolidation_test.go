package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseConsolidationUpgrade(t *testing.T) {
	dsn := os.Getenv("MSERP_CUTOVER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable database required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	schema := fmt.Sprintf("consolidation_%d", time.Now().UnixNano())
	exec := func(sql string) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE SCHEMA " + schema + ";SET search_path TO " + schema + ",public")
	defer conn.Exec(ctx, "RESET ROLE;SET search_path TO public;DROP SCHEMA "+schema+" CASCADE")
	init := migrationSQLForTest(t, "init.sql")
	retire := migrationSQLForTest(t, "223_retire_unused_tables.sql")
	presence := migrationSQLForTest(t, "224_board_presence.sql")
	days := migrationSQLForTest(t, "225_provider_sync_days.sql")
	for _, m := range []string{retire, presence, days} {
		if strings.Count(init, m) != 1 {
			t.Fatal("fresh schema and upgrade differ")
		}
		init = strings.Replace(init, m, "", 1)
	}
	exec(init)
	capture := "INSERT INTO payroll_projection_changes(table_name,operation,source_revision) VALUES('accounting_cutover','UPDATE',7)"
	for _, check := range []struct{ setup, want string }{
		{"SET LOCAL session_replication_role=replica; UPDATE accounting_cutover SET draft='{\"edited\":true}';SET LOCAL session_replication_role=origin", "singleton differs"},
		{"INSERT INTO accounting_legacy_sources VALUES('expense',gen_random_uuid())", "Review retained records"},
		{capture, "Drain pending payroll captures"},
		{"CREATE VIEW keep_cutover AS SELECT id FROM accounting_cutover", "other objects depend on"},
	} {
		exec("BEGIN;" + check.setup)
		body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(retire), "BEGIN;"), "COMMIT;")
		if _, err := conn.Exec(ctx, body); err == nil || !strings.Contains(err.Error(), check.want) {
			t.Fatalf("expected %q, got %v", check.want, err)
		}
		exec("ROLLBACK")
	}
	exec(capture + ";UPDATE payroll_projection_changes SET dispatched_at=now() WHERE table_name='accounting_cutover'")
	exec(retire)
	exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('10000000-0000-4000-8000-000000000001','Cursor','cursor','cpm',1);
 INSERT INTO app_users(id,username,password_hash,role_id) SELECT '10000000-0000-4000-8000-000000000002','Cursor user',crypt('Cursor-local-fixture',gen_salt('bf')),id FROM app_roles WHERE system_role;
 INSERT INTO auth_sessions(token_hash,user_id,csrf_token,expires_at) VALUES(repeat('f',64),'10000000-0000-4000-8000-000000000002',repeat('a',43),now()+interval '1 hour');
 INSERT INTO driver_board_presence SELECT '10000000-0000-4000-8000-000000000003',user_id,token_hash,'10000000-0000-4000-8000-000000000001','notes',now()+interval '1 hour' FROM auth_sessions;
 INSERT INTO gross_board_presence SELECT client_id,user_id,session_hash,driver_id,DATE '2026-11-02',NULL,0,'balance',expires_at FROM driver_board_presence;
 INSERT INTO relay_fuel_sync_days VALUES('production','2026-11-02',7,'2026-11-03 01:00Z');
 INSERT INTO prepass_toll_sync_days VALUES('production','2026-11-02',5,'2026-11-03 02:00Z');
 INSERT INTO weighmytruck_sync_days VALUES('2026-11-02','2026-11-03 03:00Z',0)`)
	exec(presence)
	exec(days)
	var ok bool
	err = conn.QueryRow(ctx, `SELECT
 (SELECT count(*)=2 AND count(DISTINCT client_id)=1 FROM board_presence)
 AND EXISTS (SELECT FROM board_presence WHERE board='gross' AND week_start='2026-11-02' AND column_key='balance')
 AND EXISTS (SELECT FROM board_presence WHERE board='status' AND week_start IS NULL AND column_key='notes')
 AND EXISTS (SELECT FROM payroll_projection_changes WHERE table_name='accounting_cutover' AND source_revision=7 AND dispatched_at IS NOT NULL)
 AND (SELECT array_agg(transaction_count ORDER BY fetched_at)=ARRAY[7,5,0] AND count(DISTINCT provider)=3
 AND min(fetched_at)='2026-11-03 01:00Z' AND max(fetched_at)='2026-11-03 03:00Z' FROM provider_sync_days)`).Scan(&ok)
	if err != nil || !ok {
		t.Fatalf("lost migrated evidence: %v", err)
	}
	exec("GRANT USAGE ON SCHEMA " + schema + " TO mserp_app")
	cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	fuel, tolls := NewFuelRepository(pool), NewTollRepository(pool)
	day := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	// An empty provider day must finish even while a different writer owns the payroll gate.
	exec("BEGIN; SELECT pg_advisory_xact_lock(736281940)")
	defer conn.Exec(ctx, "ROLLBACK")
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err = fuel.UpsertDay(bounded, "staging", day, nil, day, false); err != nil {
		t.Fatal(err)
	}
	if err = fuel.UpsertDay(bounded, "staging", day, nil, day, true); err != nil {
		t.Fatal(err)
	}
	if _, err = tolls.UpsertDay(bounded, "production", day, nil, day, false); err != nil {
		t.Fatal(err)
	}
	completion, err := tolls.CompletedDays(bounded, "production", day, day)
	if err != nil || len(completion) != 0 {
		t.Fatal("cross-provider day or premature completion", completion, err)
	}
	if _, err = tolls.UpsertDay(bounded, "production", day, nil, day, true); err != nil {
		t.Fatal(err)
	}
	if _, err = tolls.UpsertDay(bounded, "production", day, nil, day, true); err != nil {
		t.Fatal(err)
	}
	completion, err = tolls.CompletedDays(bounded, "production", day, day)
	if err != nil || len(completion) != 1 {
		t.Fatal("completed toll day", completion, err)
	}
	completion, err = fuel.CompletedDays(bounded, "production", day, day)
	if err != nil || len(completion) != 0 {
		t.Fatal("cross-environment day", completion, err)
	}
	completion, err = fuel.CompletedDays(bounded, "staging", day, day)
	if err != nil || len(completion) != 1 {
		t.Fatal("completed fuel day", completion, err)
	}
}
