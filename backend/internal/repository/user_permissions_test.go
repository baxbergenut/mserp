package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEffectiveUserPermissions(t *testing.T) {
	role := []string{"drivers.read", "drivers.private.read", "drivers.private.edit", "loads.read"}
	got := EffectivePermissions(role, PermissionOverrides{"drivers.read": "deny", "trucks.read": "allow"})
	if !slices.Equal(got, []string{"trucks.read", "loads.read"}) {
		t.Fatalf("unexpected effective permissions: %v", got)
	}
	if got := EffectivePermissions(nil, PermissionOverrides{"drivers.private.edit": "allow"}); len(got) != 0 {
		t.Fatalf("allowed action without prerequisites: %v", got)
	}
	if !slices.Equal(EffectivePermissions(role, nil), role) {
		t.Fatal("inheritance changed role grants")
	}
	for _, overrides := range []PermissionOverrides{{"unknown": "allow"}, {"loads.read": "inherit"}, {"loads.read": ""}} {
		if validUserOverrides(overrides, nil) {
			t.Fatalf("accepted invalid overrides: %v", overrides)
		}
	}
	if validUserOverrides(nil, ExpenseCategoryOverrides{"id": {"unknown": "allow"}}) {
		t.Fatal("accepted invalid category action")
	}
	if validUserOverrides(nil, ExpenseCategoryOverrides{"id": nil}) {
		t.Fatal("accepted null category")
	}
	category := effectiveCategoryAccess(ExpenseCategoryAccess{CanView: true, CanCreate: true, CanEdit: true, CanDelete: true}, PermissionOverrides{"canView": "deny"})
	if category.CanView || category.CanCreate || category.CanEdit || category.CanDelete {
		t.Fatal("category deny did not suppress actions")
	}
}

func TestUserPermissionOverridesDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable local MSERP_ACCESS_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	migration, err := os.ReadFile("../../sql/217_user_permission_overrides.sql")
	if err != nil {
		t.Fatal(err)
	}
	initSQL := strings.ReplaceAll(string(source), "\r\n", "\n")
	migrationSQL := strings.TrimSpace(strings.ReplaceAll(string(migration), "\r\n", "\n"))
	if !strings.Contains(initSQL, migrationSQL) {
		t.Fatal("fresh and upgrade schemas differ")
	}
	for _, mode := range []string{"fresh", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("user_overrides_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := db.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public; CREATE SCHEMA " + quoted + "; SET search_path TO " + quoted + ",public")
			defer db.Exec(context.Background(), "SET search_path TO public; DROP SCHEMA "+quoted+" CASCADE")
			if mode == "upgrade" {
				exec(strings.Replace(initSQL, migrationSQL, "", 1))
				exec(migrationSQL)
			} else {
				exec(initSQL)
			}
			exec("GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO mserp_app")
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, app)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewAuthRepository(pool)
			var adminID, roleID, categoryID string
			if err = db.QueryRow(ctx, `INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Override Admin','override-admin@example.test','$2-fixture',id FROM app_roles WHERE system_role RETURNING id::text`).Scan(&adminID); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow(ctx, `INSERT INTO app_roles(name,permissions) VALUES('Override Role',ARRAY['drivers.read','drivers.edit','loads.read']) RETURNING id::text`).Scan(&roleID); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow(ctx, `SELECT id::text FROM expense_settings WHERE kind='category' LIMIT 1`).Scan(&categoryID); err != nil {
				t.Fatal(err)
			}
			input := ManagedUser{Username: "Override User", FirstName: "Override", LastName: "User", Email: "overrides@example.test", RoleID: roleID, Active: true, PermissionOverrides: PermissionOverrides{"drivers.read": "deny", "trucks.read": "allow"}, ExpenseCategoryOverrides: ExpenseCategoryOverrides{categoryID: {"canView": "allow", "canCreate": "allow"}}}
			id, err := repo.SaveUser(ctx, adminID, input, "$2-fixture")
			if err != nil {
				t.Fatal(err)
			}
			readUser := func() ManagedUser {
				t.Helper()
				data, err := repo.AccessData(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, u := range data.Users {
					if u.ID == id {
						return u
					}
				}
				t.Fatal("user missing")
				return ManagedUser{}
			}
			input = readUser()
			if input.PermissionOverrides["drivers.read"] != "deny" || input.ExpenseCategoryOverrides[categoryID]["canCreate"] != "allow" {
				t.Fatal("overrides not persisted")
			}
			session := func() AuthSession {
				t.Helper()
				exec(`INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES($1,'override-token','csrf',now()+interval '1 hour') ON CONFLICT(token_hash) DO NOTHING`, id)
				s, err := repo.FindSessionByTokenHash(ctx, "override-token")
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			s := session()
			if HasPermission(s.User, "drivers.read") || HasPermission(s.User, "drivers.edit") || !HasPermission(s.User, "trucks.read") || !HasPermission(s.User, "loads.read") || !HasPermission(s.User, "expenses.write") {
				t.Fatalf("incorrect effective access: %v", s.User.Permissions)
			}
			// Role changes immediately affect inherited permissions; overrides remain.
			exec(`UPDATE app_roles SET permissions=ARRAY['drivers.read','drivers.edit','fuel.read'],version=version+1 WHERE id=$1`, roleID)
			if updated := session(); HasPermission(updated.User, "loads.read") || !HasPermission(updated.User, "fuel.read") || HasPermission(updated.User, "drivers.read") || updated.User.AccessRevision == s.User.AccessRevision {
				t.Fatal("role update not reflected")
			}
			// Omitting fields in legacy account updates must retain the exceptions.
			input.PermissionOverrides = nil
			input.ExpenseCategoryOverrides = nil
			if _, err = repo.SaveUser(ctx, adminID, input, ""); err != nil {
				t.Fatal(err)
			}
			if _, err = repo.FindSessionByTokenHash(ctx, "override-token"); !errors.Is(err, ErrAuthRecordNotFound) {
				t.Fatal("save did not revoke session")
			}
			input = readUser()
			if input.PermissionOverrides["drivers.read"] != "deny" {
				t.Fatal("legacy update erased overrides")
			}
			// Stale edits and malformed category identifiers must roll back.
			stale := input
			stale.Version--
			if _, err = repo.SaveUser(ctx, adminID, stale, ""); !errors.Is(err, ErrAccessConflict) {
				t.Fatal("stale update accepted")
			}
			invalid := input
			invalid.ExpenseCategoryOverrides = ExpenseCategoryOverrides{"not-a-category": {"canView": "allow"}}
			if _, err = repo.SaveUser(ctx, adminID, invalid, ""); err == nil {
				t.Fatal("unknown category accepted")
			}
			// Explicit per-user access.manage grants must pass the transactional recheck.
			input.PermissionOverrides = PermissionOverrides{"access.manage": "allow"}
			input.ExpenseCategoryOverrides = ExpenseCategoryOverrides{categoryID: {"canView": "deny", "canCreate": "allow"}}
			if _, err = repo.SaveUser(ctx, adminID, input, ""); err != nil {
				t.Fatal(err)
			}
			if err = repo.SaveRole(ctx, id, AccessRole{Name: "Created through override", Permissions: []string{}}); err != nil {
				t.Fatal(err)
			}
			if HasPermission(session().User, "expenses.read") || HasPermission(session().User, "expenses.write") {
				t.Fatal("category denial ignored")
			}
			// Conversely a role grant cannot bypass an explicit user denial.
			exec(`UPDATE app_roles SET permissions=ARRAY['access.manage'] WHERE id=$1`, roleID)
			input = readUser()
			input.PermissionOverrides = PermissionOverrides{"access.manage": "deny"}
			if _, err = repo.SaveUser(ctx, adminID, input, ""); err != nil {
				t.Fatal(err)
			}
			if err = repo.SaveRole(ctx, id, AccessRole{Name: "Must not create", Permissions: []string{}}); !errors.Is(err, ErrAccessConflict) {
				t.Fatal("transaction bypassed user denial")
			}
			// Empty objects restore inheritance, without retaining dormant grants.
			input = readUser()
			input.PermissionOverrides = PermissionOverrides{}
			input.ExpenseCategoryOverrides = ExpenseCategoryOverrides{}
			if _, err = repo.SaveUser(ctx, adminID, input, ""); err != nil {
				t.Fatal(err)
			}
			if !HasPermission(session().User, "access.manage") {
				t.Fatal("reset did not restore role")
			}
			// Administrator full access is protected, including role transitions.
			var adminRoleID string
			if err = db.QueryRow(ctx, "SELECT id::text FROM app_roles WHERE system_role").Scan(&adminRoleID); err != nil {
				t.Fatal(err)
			}
			input = readUser()
			input.RoleID = adminRoleID
			input.PermissionOverrides = PermissionOverrides{"loads.read": "deny"}
			if _, err = repo.SaveUser(ctx, adminID, input, ""); err == nil {
				t.Fatal("Administrator override accepted")
			}
			input.PermissionOverrides = nil
			input.ExpenseCategoryOverrides = nil
			if _, err = repo.SaveUser(ctx, adminID, input, ""); err != nil {
				t.Fatal(err)
			}
			if adminSession := session(); !adminSession.User.Administrator || !HasPermission(adminSession.User, "loads.read") {
				t.Fatal("Administrator access changed")
			}
			if len(readUser().PermissionOverrides) != 0 {
				t.Fatal("Administrator retained dormant overrides")
			}
			var auditCount int
			if err = db.QueryRow(ctx, `SELECT count(*) FROM access_audit WHERE target_id=$1 AND details->'permissionOverrides'->>'drivers.read'='deny'`, id).Scan(&auditCount); err != nil || auditCount == 0 {
				t.Fatal("override audit missing", err)
			}
		})
	}
}
