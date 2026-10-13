package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDispatcherExtensionDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_EXTENSION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set a disposable local MSERP_EXTENSION_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/091_dispatcher_extension.sql")
	if err != nil {
		t.Fatal(err)
	}
	updaterMigration, err := os.ReadFile("../../sql/092_updater_extension.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migrated"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("extension_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, q, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { admin.Exec(context.Background(), `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "fresh" {
				exec(string(source))
			} else {
				before, _, ok := strings.Cut(string(source), "CREATE TABLE dispatcher_extension_access")
				if !ok {
					t.Fatal("missing boundary")
				}
				exec(before + "COMMIT;")
				exec(string(migration))
				exec(string(updaterMigration))
			}
			const id = "11111111-1111-4111-8111-111111111111"
			// Older baseline tables get their production grants during provisioning.
			// Do not grant the new table: its migration must own that permission.
			exec(`GRANT SELECT,UPDATE ON dispatchers TO mserp_app; GRANT SELECT ON app_users,app_roles TO mserp_app`)
			const actor = "22222222-2222-4222-8222-222222222222"
			exec(`INSERT INTO dispatchers(id,full_name,normalized_name) VALUES($1,'Test Dispatcher','test dispatcher')`, id)
			exec(`INSERT INTO app_users(id,username,password_hash,role_id) SELECT $1,'test-admin','$2-test-fixture',id FROM app_roles WHERE system_role`, actor)
			appCfg := cfg.Copy()
			appCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appCfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, appCfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			repo := NewAuthRepository(pool)
			a, e := repo.DispatcherExtensionAccess(ctx, id)
			if e != nil || a.Version != 0 || a.Enabled {
				t.Fatalf("initial: %+v %v", a, e)
			}
			issue := func(code string) {
				t.Helper()
				a, e := repo.DispatcherExtensionAccess(ctx, id)
				if e != nil {
					t.Fatal(e)
				}
				if e = repo.SetDispatcherExtensionAccess(ctx, id, actor, "reference", code, a.Version, true); e != nil {
					t.Fatal(e)
				}
			}
			issue("code-one")
			if e = repo.SetDispatcherExtensionAccess(ctx, id, actor, "", "stale", 0, true); !errors.Is(e, ErrAccessConflict) {
				t.Fatal("stale write", e)
			}
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, e := repo.ActivateDispatcherExtension(ctx, "code-one", "token-one")
					results <- e
				}()
			}
			wg.Wait()
			close(results)
			successes := 0
			for e := range results {
				if e == nil {
					successes++
				} else if !errors.Is(e, pgx.ErrNoRows) {
					t.Fatal(e)
				}
			}
			if successes != 1 {
				t.Fatal("code not single-use", successes)
			}
			if _, e = repo.CheckDispatcherExtension(ctx, "token-one"); e != nil {
				t.Fatal(e)
			}
			issue("code-two")
			if _, e = repo.CheckDispatcherExtension(ctx, "token-one"); !errors.Is(e, pgx.ErrNoRows) {
				t.Fatal("replacement did not revoke")
			}
			exec(`UPDATE dispatcher_extension_access SET activation_expires_at=now()-interval '1 second' WHERE dispatcher_id=$1`, id)
			if _, e = repo.ActivateDispatcherExtension(ctx, "code-two", "invalid"); !errors.Is(e, pgx.ErrNoRows) {
				t.Fatal("expired activation accepted")
			}
			issue("code-three")
			if _, e = repo.ActivateDispatcherExtension(ctx, "code-three", "token-three"); e != nil {
				t.Fatal(e)
			}
			exec(`UPDATE dispatcher_extension_access SET token_expires_at=now()-interval '1 second' WHERE dispatcher_id=$1`, id)
			if _, e = repo.CheckDispatcherExtension(ctx, "token-three"); !errors.Is(e, pgx.ErrNoRows) {
				t.Fatal("expired token accepted")
			}
			issue("code-four")
			if _, e = repo.ActivateDispatcherExtension(ctx, "code-four", "token-four"); e != nil {
				t.Fatal(e)
			}
			exec(`UPDATE dispatchers SET active=false WHERE id=$1`, id)
			exec(`UPDATE dispatchers SET active=true WHERE id=$1`, id)
			if _, e = repo.CheckDispatcherExtension(ctx, "token-four"); !errors.Is(e, pgx.ErrNoRows) {
				t.Fatal("reactivation resurrected token")
			}
			a, e = repo.DispatcherExtensionAccess(ctx, id)
			if e != nil || a.Enabled {
				t.Fatal("deactivation did not revoke")
			}
			testUpdaterExtensionLifecycle(t, ctx, repo, exec, actor)
			exec(`UPDATE app_users SET active=false WHERE id=$1`, actor)
			if e = repo.SetDispatcherExtensionAccess(ctx, id, actor, "", "bad", a.Version, true); !errors.Is(e, ErrAccessConflict) {
				t.Fatal("revoked administrator accepted", e)
			}
			var audits int
			if e = admin.QueryRow(ctx, `SELECT count(*) FROM access_audit WHERE action='dispatcher_extension_access' AND details::text NOT LIKE '%token%' AND details::text NOT LIKE '%code%'`).Scan(&audits); e != nil || audits != 4 {
				t.Fatal("audit", audits, e)
			}
		})
	}
}

func testUpdaterExtensionLifecycle(t *testing.T, ctx context.Context, repo *AuthRepository, exec func(string, ...any), actor string) {
	t.Helper()
	const id = "33333333-3333-4333-8333-333333333333"
	exec(`INSERT INTO updaters(id,full_name,normalized_name,shift) VALUES($1,'Test Updater','test updater','main')`, id)
	a, err := repo.UpdaterExtensionAccess(ctx, id)
	if err != nil || a.Version != 0 || a.Enabled || a.UpdaterID != id || a.DispatcherID != "" {
		t.Fatalf("updater initial: %+v %v", a, err)
	}
	issue := func(code string) {
		t.Helper()
		a, err := repo.UpdaterExtensionAccess(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.SetUpdaterExtensionAccess(ctx, id, actor, "Roy", code, a.Version, true); err != nil {
			t.Fatal(err)
		}
	}
	issue("updater-code-one")
	if err = repo.SetUpdaterExtensionAccess(ctx, id, actor, "", "stale", 0, true); !errors.Is(err, ErrAccessConflict) {
		t.Fatal("stale updater write accepted", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ActivateDispatcherExtension(ctx, "updater-code-one", "updater-token-one")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("updater code not single use")
	}
	if name, err := repo.CheckDispatcherExtension(ctx, "updater-token-one"); err != nil || name != "Test Updater" {
		t.Fatal("updater token rejected", name, err)
	}
	issue("updater-code-two")
	if _, err := repo.CheckDispatcherExtension(ctx, "updater-token-one"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("updater replacement did not revoke", err)
	}
	exec(`UPDATE updater_extension_access SET activation_expires_at=now()-interval '1 second' WHERE updater_id=$1`, id)
	if _, err := repo.ActivateDispatcherExtension(ctx, "updater-code-two", "invalid"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("expired updater code accepted", err)
	}
	issue("updater-code-three")
	if _, err := repo.ActivateDispatcherExtension(ctx, "updater-code-three", "updater-token-three"); err != nil {
		t.Fatal(err)
	}
	a, err = repo.UpdaterExtensionAccess(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SetUpdaterExtensionAccess(ctx, id, actor, "", "", a.Version, false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CheckDispatcherExtension(ctx, "updater-token-three"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("revoked updater token accepted", err)
	}
	issue("updater-code-four")
	if _, err := repo.ActivateDispatcherExtension(ctx, "updater-code-four", "updater-token-four"); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE updater_extension_access SET token_expires_at=now()-interval '1 second' WHERE updater_id=$1`, id)
	if _, err := repo.CheckDispatcherExtension(ctx, "updater-token-four"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("expired updater token accepted", err)
	}
	issue("updater-code-five")
	if _, err := repo.ActivateDispatcherExtension(ctx, "updater-code-five", "updater-token-five"); err != nil {
		t.Fatal(err)
	}
	exec(`DELETE FROM updaters WHERE id=$1`, id)
	if _, err := repo.CheckDispatcherExtension(ctx, "updater-token-five"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("deleted updater token accepted", err)
	}
	if err := repo.SetUpdaterExtensionAccess(ctx, id, actor, "", "bad", 0, true); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("deleted updater enabled", err)
	}
}
