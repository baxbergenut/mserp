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
		{"own after role change", AuthUser{ID: owner}, "admin", false},
		{"same accountant role", AuthUser{ID: "other", RoleName: "Renamed finance", Permissions: []string{"expenses.accounting.verify"}}, "accountant", false},
		{"accountant cannot remove admin", AuthUser{ID: "other", RoleName: "Renamed finance", Permissions: []string{"expenses.accounting.verify"}}, "admin", false},
		{"admin removes accountant", AuthUser{ID: "other", Administrator: true}, "accountant", true},
		{"retired manager verification cannot be removed", AuthUser{ID: "other", Administrator: true}, "admin", false},
		{"own with explicit grant", AuthUser{ID: owner, Permissions: []string{"expenses.accounting.verify"}}, "accountant", true},
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
	migration, err := os.ReadFile("../../sql/151_retire_expense_manager_verification.sql")
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
				before, _, ok := strings.Cut(string(source), "-- Retire expense manager verification (migration 151).")
				if !ok {
					t.Fatal("missing migration boundary")
				}
				exec(before)
				exec(`INSERT INTO expenses(category_id,category,expense_date,amount,expense_type,manager_verified,accounting_verified) SELECT id,name,'2026-10-12',25,'Legacy check',true,false FROM expense_settings WHERE kind='category' AND name='Other'`)
				exec(`INSERT INTO expense_verifications(expense_id,verifier_role,user_name,verified_at,revoked_at)
 SELECT id,'admin','Former manager','2026-10-12 12:00:00Z',NULL FROM expenses WHERE expense_type='Legacy check';
 INSERT INTO expense_verifications(expense_id,verifier_role,user_name,verified_at,revoked_at)
 SELECT id,'admin','Former manager','2026-10-11 12:00:00Z','2026-10-11 13:00:00Z' FROM expenses WHERE expense_type='Legacy check';
 INSERT INTO expenses(category_id,category,expense_date,amount,expense_type,accounting_verified)
 SELECT id,name,'2026-10-12',40,'Accounting legacy check',true FROM expense_settings WHERE kind='category' AND name='Other';
 INSERT INTO expense_verifications(expense_id,verifier_role,user_name,verified_at)
 SELECT id,'accountant','Saved accountant','2026-10-12 14:00:00Z' FROM expenses WHERE expense_type='Accounting legacy check';
 INSERT INTO expense_sheet_exports(expense_id,spreadsheet_id,sheet_id,payload,delivered_at)
 SELECT id,'fixture',7,jsonb_build_object('expense',to_jsonb(e),'nested',jsonb_build_array(jsonb_build_object('managerVerified',true,'accountingVerified',true))),'2026-10-12 15:00:00Z'
 FROM expenses e WHERE expense_type='Legacy check'`)
				exec(string(migration))
				var remaining int
				if err := admin.QueryRow(ctx, `SELECT count(*) FROM expense_verifications WHERE verifier_role='admin'`).Scan(&remaining); err != nil || remaining != 0 {
					t.Fatal("historical manager approvals remain", err)
				}
				var accounting bool
				if err := admin.QueryRow(ctx, `SELECT accounting_verified FROM expenses WHERE expense_type='Legacy check'`).Scan(&accounting); err != nil || accounting {
					t.Fatal("manager approval became accounting approval", err)
				}
				var preserved bool
				if err := admin.QueryRow(ctx, `SELECT e.accounting_verified AND e.amount=40 AND v.user_name='Saved accountant' AND v.verified_at='2026-10-12 14:00:00Z' AND v.revoked_at IS NULL FROM expenses e JOIN expense_verifications v ON v.expense_id=e.id WHERE e.expense_type='Accounting legacy check'`).Scan(&preserved); err != nil || !preserved {
					t.Fatal("accounting verification changed during removal", err)
				}
				if err := admin.QueryRow(ctx, `SELECT NOT (payload->'expense' ? 'manager_verified') AND NOT (payload->'nested'->0 ? 'managerVerified') AND (payload->'nested'->0->>'accountingVerified')::boolean AND (payload->'expense'->>'amount')::numeric=25 AND spreadsheet_id='fixture' AND sheet_id=7 AND delivered_at='2026-10-12 15:00:00Z' FROM expense_sheet_exports`).Scan(&preserved); err != nil || !preserved {
					t.Fatal("export snapshot cleanup lost accounting data or delivery receipts", err)
				}
			} else {
				exec(string(source))
			}

			var runtimeOwner bool
			if e := admin.QueryRow(ctx, `SELECT tableowner = 'mserp_app' FROM pg_tables WHERE schemaname = $1 AND tablename = 'expense_verifications'`, schema).Scan(&runtimeOwner); e != nil || !runtimeOwner {
				t.Fatalf("verification table must belong to the runtime role before test grants: %v", e)
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			applyAccountingExpenseTestSchema(t, ctx, admin)
			testCfg := cfg.Copy()
			testCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			testCfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, testCfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			actors := []AuthUser{{Username: "Admin", Administrator: true}, {Username: "Accountant One", RoleName: "Renamed finance", Permissions: []string{"expenses.accounting.verify"}}, {Username: "Accountant Two", RoleName: "Renamed finance", Permissions: []string{"expenses.accounting.verify"}}, {Username: "Viewer"}}
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
			if _, e = repo.VerifyExpense(ctx, id, entry.CategoryID, "accountant", stale, actors[0]); !errors.Is(e, ErrAccessConflict) {
				t.Fatalf("stale verification: %v", e)
			}
			if _, e = repo.VerifyExpense(ctx, id, entry.CategoryID, "admin", entry.UpdatedAt, actors[0]); !errors.Is(e, ErrExpenseVerificationForbidden) {
				t.Fatalf("retired manager verification accepted: %v", e)
			}
			for _, actor := range []AuthUser{actors[2], actors[3]} {
				if _, e = repo.UnverifyExpense(ctx, id, entry.CategoryID, first, actor); !errors.Is(e, ErrExpenseVerificationForbidden) {
					t.Fatalf("cancellation permission: %v", e)
				}
			}
			entry, e = repo.UnverifyExpense(ctx, id, entry.CategoryID, first, actors[1])
			if e != nil {
				t.Fatal(e)
			}
			if entry.AccountingVerified || len(entry.Verifications) != 0 {
				t.Fatal("cancellation left an active accounting check")
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
			if entry.AccountingVerified || len(entry.Verifications) != 0 {
				t.Fatal("checks remain after cancellation")
			}
			var count int
			if e = admin.QueryRow(ctx, `SELECT count(*) FROM expense_verifications WHERE expense_id=$1 AND revoked_at IS NOT NULL AND revoked_by IS NOT NULL`, id).Scan(&count); e != nil || count != 2 {
				t.Fatalf("cancellation audit: %d %v", count, e)
			}
			exec(`INSERT INTO expenses(category_id,category,expense_date,amount,expense_type,accounting_verified)
 SELECT s.id,s.name,v.day,v.amount,'Filter fixture',v.accountant
 FROM expense_settings s CROSS JOIN (VALUES (date '2026-10-12',10,false),(date '2026-10-12',20,true),(date '2026-10-12',30,false),(NULL::date,40,true)) v(day,amount,accountant)
 WHERE s.kind='category' AND s.name IN ('Other','Maintenance')`)
			boolPtr := func(v bool) *bool { return &v }
			for _, tc := range []struct {
				accountant *bool
				total      int
				amount     string
				incomplete int
			}{
				{nil, 4, "100.00", 1}, {boolPtr(false), 2, "40.00", 0}, {boolPtr(true), 2, "60.00", 1},
			} {
				page, err := repo.ListExpensesPage(ctx, ExpensePageQuery{Search: "Filter fixture", VisibleCategoryIDs: []string{entry.CategoryID}, AccessibleCategoryIDs: []string{entry.CategoryID}, AccountantVerified: tc.accountant, Pagination: Pagination{Page: 99, PageSize: 1}})
				if err != nil {
					t.Fatal(err)
				}
				if page.Total != tc.total || page.Summary.Amount != tc.amount || page.Summary.IncompleteCount != tc.incomplete || page.Page.Page != tc.total || len(page.Items) != 1 {
					t.Fatalf("verification filtering/pagination/category scope mismatch: %+v", page)
				}
				for _, item := range page.Items {
					if tc.accountant != nil && item.AccountingVerified != *tc.accountant {
						t.Fatal("row does not match verification filters")
					}
				}
			}
		})
	}
}
