package repository

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPermissionsV2Migration(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires local disposable MSERP_ACCESS_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/112_permissions_v2.sql")
	if err != nil {
		t.Fatal(err)
	}
	before, after, ok := strings.Cut(string(source), "-- Permissions v2 (migration 112).")
	if !ok || strings.TrimSpace(after) != strings.TrimSpace(string(migration)) {
		t.Fatal("fresh schema and migration differ")
	}
	for _, mode := range []string{"fresh", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("permission_v2_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := db.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + quoted + "; SET search_path TO " + quoted + ",public")
			defer db.Exec(context.Background(), "SET search_path TO public; DROP SCHEMA "+quoted+" CASCADE")
			if mode == "fresh" {
				exec(string(source))
			} else {
				exec(before)
				exec(`INSERT INTO app_roles(name,permissions) VALUES ('Ops',ARRAY['fleet.read','fleet.write']),('Broken',ARRAY['fleet.write','payroll.write','escrow.write']),('Finance',ARRAY['payroll.read','payroll.write','payroll.finalize','charges.read','charges.write','escrow.read','escrow.write']),('Accountant',ARRAY[]::text[])`)
				exec(`INSERT INTO app_users(username,password_hash,role_id) SELECT 'cutover',crypt('test-fixture-only',gen_salt('bf')),id FROM app_roles WHERE name='Ops'; INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) SELECT id,repeat('a',64),repeat('b',43),now()+interval '1 day' FROM app_users WHERE username='cutover'`)
				exec(string(migration))
				for _, name := range []string{"Ops", "Broken", "Finance", "Accountant"} {
					var keys []string
					if err := db.QueryRow(ctx, "SELECT permissions FROM app_roles WHERE name=$1", name).Scan(&keys); err != nil {
						t.Fatal(err)
					}
					if !ValidPermissions(keys) {
						t.Fatalf("invalid migrated %s: %v", name, keys)
					}
					for _, key := range keys {
						if strings.Contains(key, "private") || strings.Contains(key, "documents") {
							t.Fatalf("new sensitive grant %s", key)
						}
					}
					if name == "Ops" && (!slices.Contains(keys, "drivers.edit") || slices.Contains(keys, "driver_compensation.read")) {
						t.Fatal("incorrect operational mapping")
					}
					if name == "Broken" && len(keys) != 0 {
						t.Fatal("write-without-view expanded access")
					}
					if name == "Finance" && (!slices.Contains(keys, "investor_pay.reopen") || !slices.Contains(keys, "investor_terms.edit") || !slices.Contains(keys, "escrow.create") || !slices.Contains(keys, "escrow.edit") || !slices.Contains(keys, "escrow.delete")) {
						t.Fatal("financial mapping missing")
					}
					if name == "Accountant" && !slices.Contains(keys, "expenses.accounting.verify") {
						t.Fatal("verification grant missing")
					}
				}
				var sessions, audits int
				if err := db.QueryRow(ctx, "SELECT (SELECT count(*) FROM auth_sessions),(SELECT count(*) FROM access_audit WHERE action='permissions_v2_migration')").Scan(&sessions, &audits); err != nil || sessions != 0 || audits < 4 {
					t.Fatalf("cutover sessions/audit %d/%d %v", sessions, audits, err)
				}
			}
			exec("GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO mserp_app; SET ROLE mserp_app")
			var column string
			if err := db.QueryRow(ctx, "SELECT column_name FROM information_schema.columns WHERE table_schema=$1 AND table_name='files' AND column_name='uploaded_by'", schema).Scan(&column); err != nil {
				t.Fatal(err)
			}
			exec("RESET ROLE")

			// File IDs are not bearer credentials. Check ownership before and after attachment.
			appConfig := cfg.Copy()
			appConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appConfig.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, appConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			var ownerID, otherID, driverID string
			for i, name := range []string{"File owner", "Other user"} {
				var id string
				if err := db.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES($1,crypt('fixture-password',gen_salt('bf'))) RETURNING id::text`, name).Scan(&id); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					ownerID = id
				} else {
					otherID = id
				}
			}
			files := NewFileRepository(pool)
			file, err := files.CreateFile(ctx, "private.pdf", "application/pdf", []byte("fixture"), ownerID)
			if err != nil {
				t.Fatal(err)
			}
			check := func(user AuthUser, kind string, want bool) {
				t.Helper()
				allowed, err := files.CanAccessFile(ctx, file.ID, user, kind)
				if err != nil || allowed != want {
					t.Fatalf("file access %v want %v: %v", allowed, want, err)
				}
			}
			check(AuthUser{ID: ownerID}, "", true)
			check(AuthUser{ID: otherID, Permissions: PermissionKeys()}, "", false)
			check(AuthUser{ID: otherID, Administrator: true}, "", true)
			if err := db.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,cdl_file_id) VALUES('Private Driver','private driver','cpm',0.75,$1) RETURNING id::text`, file.ID).Scan(&driverID); err != nil {
				t.Fatal(err)
			}
			check(AuthUser{ID: ownerID, Permissions: []string{"drivers.read"}}, "", false)
			check(AuthUser{ID: otherID, Permissions: []string{"drivers.read", "drivers.documents.read"}}, "drivers", true)
			check(AuthUser{ID: otherID, Permissions: []string{"trucks.read", "trucks.documents.read"}}, "trucks", false)
		})
	}
}

func TestPermissionPrerequisites(t *testing.T) {
	for _, p := range Permissions {
		keys := append([]string{p.Key}, p.Requires...)
		// Add transitive requirements exactly as Settings exposes them.
		for _, dependency := range Permissions {
			if slices.Contains(keys, dependency.Key) {
				for _, key := range dependency.Requires {
					if !slices.Contains(keys, key) {
						keys = append(keys, key)
					}
				}
			}
		}
		if !ValidPermissions(keys) {
			t.Errorf("catalog has impossible prerequisites: %s", p.Key)
		}
		if len(p.Requires) > 0 && ValidPermissions([]string{p.Key}) {
			t.Errorf("accepted action without prerequisite: %s", p.Key)
		}
	}
	for _, key := range []string{"fleet.read", "fleet.write", "payroll.read", "payroll.write", "unknown"} {
		if ValidPermissions([]string{key}) {
			t.Errorf("accepted obsolete permission %s", key)
		}
	}
}
