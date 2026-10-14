package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpenseVerificationCancellationRoles(t *testing.T) {
	owner := "owner"
	for _, tc := range []struct {
		name  string
		actor AuthUser
		role  string
		want  bool
	}{
		{"own after role change", AuthUser{ID: owner}, "admin", true},
		{"same accountant role", AuthUser{ID: "other", RoleName: "Accountant"}, "accountant", true},
		{"accountant cannot remove admin", AuthUser{ID: "other", RoleName: "Accountant"}, "admin", false},
		{"admin removes accountant", AuthUser{ID: "other", Administrator: true}, "accountant", true},
		{"admin removes admin", AuthUser{ID: "other", Administrator: true}, "admin", true},
		{"ordinary viewer", AuthUser{ID: "other"}, "accountant", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanRevokeExpenseVerification(tc.actor, ExpenseVerification{UserID: &owner, Role: tc.role}); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExpenseVerificationDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ACCESS_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("a local disposable _test database is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/085_expense_verification.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("expense_verification_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, q, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			defer func() {
				_, _ = admin.Exec(context.Background(), `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			}()
			if mode == "migration" {
				before, _, ok := strings.Cut(string(source), "-- Expense verification attribution and cancellation (migration 085).")
				if !ok {
					t.Fatal("missing migration boundary")
				}
				exec(before)
				exec(`INSERT INTO expenses(company,category_id,category,expense_date,amount,expense_type,manager_verified) SELECT 'MS Express',id,name,'2026-10-12',25,'Legacy check',true FROM expense_settings WHERE kind='category' AND name='Other'`)
				exec(string(migration))
				for _, path := range []string{"../../sql/087_retire_expense_company.sql", "../../sql/088_drop_expense_company.sql", "../../sql/089_expense_efs.sql", "../../sql/095_expense_cost_breakdown.sql"} {
					next, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					exec(string(next))
				}
				var unknown bool
				if e := admin.QueryRow(ctx, `SELECT user_id IS NULL AND user_name='' AND verified_at IS NULL FROM expense_verifications`).Scan(&unknown); e != nil || !unknown {
					t.Fatalf("invented legacy attribution: %v", e)
				}
			} else {
				exec(string(source))
			}
			var runtimeOwner bool
			if e := admin.QueryRow(ctx, `SELECT tableowner = 'mserp_app' FROM pg_tables WHERE schemaname = $1 AND tablename = 'expense_verifications'`, schema).Scan(&runtimeOwner); e != nil || !runtimeOwner {
				t.Fatalf("verification table must belong to the runtime role before test grants: %v", e)
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			testCfg := cfg.Copy()
			testCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			testCfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, testCfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			actors := []AuthUser{{Username: "Admin", Administrator: true}, {Username: "Accountant One", RoleName: "Accountant"}, {Username: "Accountant Two", RoleName: "Accountant"}, {Username: "Viewer"}}
			for i := range actors {
				if e = admin.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES($1,crypt('fixture-password-only',gen_salt('bf'))) RETURNING id::text`, actors[i].Username).Scan(&actors[i].ID); e != nil {
					t.Fatal(e)
				}
			}
			var id string
			if e = admin.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_date,amount,expense_type) SELECT id,name,'2026-10-12',25,'Review check' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id::text`).Scan(&id); e != nil {
				t.Fatal(e)
			}
			repo := NewExpenseRepository(pool)
			entry, e := repo.GetExpense(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			stale := entry.UpdatedAt
			entry, e = repo.VerifyExpense(ctx, id, entry.CategoryID, "accountant", entry.UpdatedAt, actors[1])
			if e != nil {
				t.Fatal(e)
			}
			if len(entry.Verifications) != 1 || entry.Verifications[0].UserName != actors[1].Username || entry.Verifications[0].VerifiedAt == nil {
				t.Fatal("missing verification attribution")
			}
			first := entry.Verifications[0].ID
			if _, e = repo.VerifyExpense(ctx, id, entry.CategoryID, "admin", stale, actors[0]); !errors.Is(e, ErrAccessConflict) {
				t.Fatalf("stale verification: %v", e)
			}
			entry, e = repo.VerifyExpense(ctx, id, entry.CategoryID, "admin", entry.UpdatedAt, actors[0])
			if e != nil {
				t.Fatal(e)
			}
			var adminStamp string
			for _, v := range entry.Verifications {
				if v.Role == "admin" {
					adminStamp = v.ID
				}
			}
			for _, bad := range []struct {
				actor AuthUser
				stamp string
			}{{actors[1], adminStamp}, {actors[3], first}} {
				if _, e = repo.UnverifyExpense(ctx, id, entry.CategoryID, bad.stamp, bad.actor); !errors.Is(e, ErrExpenseVerificationForbidden) {
					t.Fatalf("cancellation permission: %v", e)
				}
			}
			entry, e = repo.UnverifyExpense(ctx, id, entry.CategoryID, first, actors[2])
			if e != nil {
				t.Fatal(e)
			}
			if entry.AccountingVerified || !entry.ManagerVerified {
				t.Fatal("cancellation touched the other role")
			}
			entry, e = repo.VerifyExpense(ctx, id, entry.CategoryID, "accountant", entry.UpdatedAt, actors[1])
			if e != nil {
				t.Fatal(e)
			}
			if _, e = repo.UnverifyExpense(ctx, id, entry.CategoryID, first, actors[1]); !errors.Is(e, ErrAccessConflict) {
				t.Fatalf("old undo removed newer verification: %v", e)
			}
			var current string
			for _, v := range entry.Verifications {
				if v.Role == "accountant" {
					current = v.ID
				}
			}
			entry, e = repo.UnverifyExpense(ctx, id, entry.CategoryID, current, actors[0])
			if e != nil {
				t.Fatal(e)
			}
			entry, e = repo.UnverifyExpense(ctx, id, entry.CategoryID, adminStamp, AuthUser{ID: actors[0].ID, Username: actors[0].Username})
			if e != nil {
				t.Fatal(e)
			}
			if entry.ManagerVerified || entry.AccountingVerified || len(entry.Verifications) != 0 {
				t.Fatal("checks remain after cancellation")
			}
			var count int
			if e = admin.QueryRow(ctx, `SELECT count(*) FROM expense_verifications WHERE expense_id=$1 AND revoked_at IS NOT NULL AND revoked_by IS NOT NULL`, id).Scan(&count); e != nil || count != 3 {
				t.Fatalf("cancellation audit: %d %v", count, e)
			}
			exec(`INSERT INTO expenses(category_id,category,expense_date,amount,expense_type,accounting_verified,manager_verified)
 SELECT s.id,s.name,v.day,v.amount,'Filter fixture',v.accountant,v.admin
 FROM expense_settings s CROSS JOIN (VALUES (date '2026-10-12',10,false,false),(date '2026-10-12',20,true,false),(date '2026-10-12',30,false,true),(NULL::date,40,true,true)) v(day,amount,accountant,admin)
 WHERE s.kind='category' AND s.name IN ('Other','Maintenance')`)
			boolPtr := func(v bool) *bool { return &v }
			for _, tc := range []struct {
				accountant, administrator *bool
				total                     int
				amount                    string
				incomplete                int
			}{
				{nil, nil, 4, "100.00", 1}, {boolPtr(false), nil, 2, "40.00", 0}, {boolPtr(true), nil, 2, "60.00", 1},
				{nil, boolPtr(false), 2, "30.00", 0}, {nil, boolPtr(true), 2, "70.00", 1},
				{boolPtr(true), boolPtr(false), 1, "20.00", 0}, {boolPtr(false), boolPtr(true), 1, "30.00", 0},
				{boolPtr(true), boolPtr(true), 1, "40.00", 1}, {boolPtr(false), boolPtr(false), 1, "10.00", 0},
			} {
				page, err := repo.ListExpensesPage(ctx, ExpensePageQuery{Search: "Filter fixture", VisibleCategoryIDs: []string{entry.CategoryID}, AccessibleCategoryIDs: []string{entry.CategoryID}, AccountantVerified: tc.accountant, AdminVerified: tc.administrator, Pagination: Pagination{Page: 99, PageSize: 1}})
				if err != nil {
					t.Fatal(err)
				}
				if page.Total != tc.total || page.Summary.Amount != tc.amount || page.Summary.IncompleteCount != tc.incomplete || page.Page.Page != tc.total || len(page.Items) != 1 {
					t.Fatalf("verification filtering/pagination/category scope mismatch: %+v", page)
				}
				for _, item := range page.Items {
					if tc.accountant != nil && item.AccountingVerified != *tc.accountant || tc.administrator != nil && item.ManagerVerified != *tc.administrator {
						t.Fatal("row does not match verification filters")
					}
				}
			}
		})
	}
}
