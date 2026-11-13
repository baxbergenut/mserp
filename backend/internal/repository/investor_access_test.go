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
)

func TestInvestorPermissionDependencies(t *testing.T) {
	for _, key := range []string{"investor_pay.read", "investor_pay.edit", "investor_pay.finalize", "investor_pay.reopen", "investor_terms.read", "investor_terms.edit", "investors.create", "investors.edit"} {
		grants := []string{key, "investor_pay.read", "investor_terms.read"}
		if HasPermission(AuthUser{Permissions: grants}, key) {
			t.Fatalf("%s bypasses investors.read", key)
		}
		if !HasPermission(AuthUser{Permissions: append(grants, "investors.read")}, key) {
			t.Fatalf("%s unavailable with prerequisites", key)
		}
	}
	for _, p := range Permissions {
		if p.Key == "investors.read" && !p.Sensitive {
			t.Fatal("Investors must be labelled Sensitive")
		}
	}
}

func TestSensitiveInvestorAccessMigration(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated local MSERP_ACCESS_TEST_DATABASE_URL")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.Database, "_test") || (cfg.Host != "127.0.0.1" && cfg.Host != "localhost") {
		t.Fatal("requires localhost _test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	schema := pgx.Identifier{fmt.Sprintf("investor_access_%d", time.Now().UnixNano())}.Sanitize()
	exec := func(sql string) {
		t.Helper()
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema + ",public")
	defer db.Exec(context.Background(), "SET search_path TO public; DROP SCHEMA "+schema+" CASCADE")
	exec(`CREATE TABLE app_roles(id uuid DEFAULT gen_random_uuid(),name text,permissions text[] DEFAULT '{}',system_role boolean DEFAULT false,version int DEFAULT 1);
 CREATE TABLE access_audit(actor_id uuid,action text,target_id uuid,details jsonb);
 CREATE TABLE auth_sessions(id int);
 INSERT INTO auth_sessions VALUES(1);
 INSERT INTO app_roles(name,permissions) VALUES
 ('Dispatcher',ARRAY['trucks.read','investors.read','investors.edit','investor_pay.read','investor_pay.edit','investor_terms.read','investor_terms.edit']),
 ('Accountant',ARRAY['driver_pay.read','investor_pay.read','investor_pay.edit','investor_terms.read']),
 ('Accountants',ARRAY['trucks.read']),('Custom Admin',ARRAY['access.manage','investors.read']),('No access','{}');
 INSERT INTO app_roles(name,system_role) VALUES('Administrator',true);`)
	migration, err := os.ReadFile("../../sql/217_sensitive_investor_access.sql")
	if err != nil {
		t.Fatal(err)
	}
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ReplaceAll(string(init), "\r\n", "\n"), strings.TrimSpace(strings.ReplaceAll(string(migration), "\r\n", "\n"))) {
		t.Fatal("init.sql lacks matching cutover")
	}
	exec(string(migration))
	rows, err := db.Query(ctx, "SELECT name,permissions,version FROM app_roles WHERE NOT system_role")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		var grants []string
		var version int
		if err = rows.Scan(&name, &grants, &version); err != nil {
			t.Fatal(err)
		}
		accountant := name == "Accountant" || name == "Accountants"
		if slices.Contains(grants, "investors.read") != accountant || slices.Contains(grants, "investor_pay.read") != accountant || !ValidPermissions(grants) || version != 2 {
			t.Fatalf("%s: %v version %d", name, grants, version)
		}
		if name == "Dispatcher" && !slices.Contains(grants, "trucks.read") {
			t.Fatal("unrelated grant lost")
		}
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	var sessions, audits int
	var marker bool
	if err = db.QueryRow(ctx, `SELECT (SELECT count(*) FROM auth_sessions),(SELECT count(*) FROM access_audit),investor_identity_access_v1()`).Scan(&sessions, &audits, &marker); err != nil || sessions != 0 || audits != 6 || !marker {
		t.Fatalf("sessions=%d audits=%d marker=%v err=%v", sessions, audits, marker, err)
	}
}
