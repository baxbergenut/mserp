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

func TestRefresherRetirementDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_ACCESS_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	read := func(name string) string {
		t.Helper()
		body, err := os.ReadFile("../../sql/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	for _, mode := range []string{"fresh", "migrated"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("retire_refresher_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(query string) {
				t.Helper()
				if _, err := db.Exec(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			defer db.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			exec(read("init.sql"))
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,UPDATE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			exec(`INSERT INTO dispatchers(id,full_name,normalized_name,extension) VALUES ('11111111-1111-4111-8111-111111111111','Dispatcher','dispatcher',106);
			INSERT INTO updaters(id,full_name,normalized_name,shift,extension) VALUES ('22222222-2222-4222-8222-222222222222','Updater','updater','main',107);
			INSERT INTO dispatcher_updaters(dispatcher_id,shift,updater_id) VALUES ('11111111-1111-4111-8111-111111111111','main','22222222-2222-4222-8222-222222222222');
			INSERT INTO access_audit(action,target_id) VALUES ('historical_access','11111111-1111-4111-8111-111111111111')`)
			if mode == "migrated" {
				exec(read("091_dispatcher_extension.sql"))
				exec(read("092_updater_extension.sql"))
				exec(`INSERT INTO dispatcher_extension_access(dispatcher_id,enabled,activation_hash,token_hash) VALUES ('11111111-1111-4111-8111-111111111111',true,'synthetic-dispatch-code','synthetic-dispatch-token');
				INSERT INTO updater_extension_access(updater_id,enabled,activation_hash,token_hash) VALUES ('22222222-2222-4222-8222-222222222222',true,'synthetic-updater-code','synthetic-updater-token')`)
			}
			migration := read("098_retire_dispatcher_extension.sql")
			exec(migration)
			exec(migration) // Safe when already retired, including fresh installations.
			var removed bool
			if err := db.QueryRow(ctx, `SELECT to_regclass('dispatcher_extension_access') IS NULL
			AND to_regclass('updater_extension_access') IS NULL
			AND to_regprocedure('revoke_inactive_dispatcher_extension()') IS NULL
			AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='dispatchers'::regclass AND tgname='dispatcher_extension_deactivation')
			AND NOT EXISTS(SELECT 1 FROM payroll_cache_revisions WHERE table_name IN ('dispatcher_extension_access','updater_extension_access','dispatcher_extension_events'))`).Scan(&removed); err != nil || !removed {
				t.Fatalf("legacy credentials or database hooks remain: %v", err)
			}
			var audits int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM access_audit`).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "migrated" {
				want = 3
			}
			if audits != want {
				t.Fatalf("audit history lost or retirement duplicated: %d, want %d", audits, want)
			}
			// Deactivation must keep working after the extension's trigger is gone.
			exec(`SET ROLE mserp_app; UPDATE dispatchers SET active=false WHERE normalized_name='dispatcher'; RESET ROLE`)
			var preserved bool
			if err := db.QueryRow(ctx, `SELECT d.extension=106 AND u.extension=107 AND NOT d.active
			FROM dispatchers d JOIN dispatcher_updaters du ON du.dispatcher_id=d.id JOIN updaters u ON u.id=du.updater_id`).Scan(&preserved); err != nil || !preserved {
				t.Fatalf("phone extensions or updater assignments changed: %v", err)
			}
		})
	}
}
