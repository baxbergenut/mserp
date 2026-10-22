package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs for fresh and upgraded schemas as the real application database role.
func testExpenseRecordCreatorRoles(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var role, creator, category, expense, work string
	query := func(sql string, dest *string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	query(`INSERT INTO app_roles(name,permissions) VALUES('Peer record fixtures','{}') RETURNING id::text`, &role)
	query(`INSERT INTO app_users(username,password_hash,role_id) VALUES('Record creator',crypt('fixture-password',gen_salt('bf')),$1) RETURNING id::text`, &creator, role)
	query(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`, &category)
	query(`INSERT INTO expenses(category_id,category,expense_date,amount,created_by,created_by_name) VALUES($1,'Other','2026-10-22',1,$2,'Record creator') RETURNING id::text`, &expense, category, creator)
	query(`INSERT INTO maintenance_records(status,work_status,created_by,created_by_name) VALUES('needs_review','open',$1,'Record creator') RETURNING id::text`, &work, creator)
	expenses, maintenance := NewExpenseRepository(pool), NewMaintenanceRepository(pool)
	check := func(want string) {
		t.Helper()
		e, err := expenses.GetExpense(ctx, expense)
		if err != nil || e.CreatorRoleID != want {
			t.Fatalf("expense creator role %q, want %q: %v", e.CreatorRoleID, want, err)
		}
		m, err := maintenance.Get(ctx, work)
		if err != nil || m.CreatorRoleID != want || m.CategoryID == "" {
			t.Fatalf("work creator role %q, want %q: %v", m.CreatorRoleID, want, err)
		}
	}
	check(role)
	query(`UPDATE app_users SET role_id=(SELECT id FROM app_roles WHERE system_role) WHERE id=$1 RETURNING role_id::text`, &role, creator)
	check(role)
	if _, err := pool.Exec(ctx, `UPDATE app_users SET role_id=NULL WHERE id=$1`, creator); err != nil {
		t.Fatal(err)
	}
	check("")
	if _, err := pool.Exec(ctx, `DELETE FROM app_users WHERE id=$1`, creator); err != nil {
		t.Fatal(err)
	}
	check("")
}

func TestExpenseRecordEditAccess(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		view, add, edit        bool
		actorRole, creatorRole string
		want                   bool
	}{
		{"same role add", true, true, false, "fleet", "fleet", true},
		{"different role add", true, true, false, "fleet", "safety", false},
		{"same role viewer", true, false, false, "fleet", "fleet", false},
		{"add without view", false, true, false, "fleet", "fleet", false},
		{"unknown creator", true, true, false, "fleet", "", false},
		{"no roles", true, true, false, "", "", false},
		{"category editor", true, false, true, "fleet", "safety", true},
		{"editor of imported record", true, false, true, "fleet", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := AuthUser{RoleID: tc.actorRole, ExpenseCategoryAccess: []ExpenseCategoryAccess{{CategoryID: "category", CanView: tc.view, CanCreate: tc.add, CanEdit: tc.edit}}}
			if got := CanEditExpenseRecord(user, "category", tc.creatorRole); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if CanEditExpenseRecord(user, "other category", tc.creatorRole) {
				t.Fatal("must not grant access to another category")
			}
		})
	}
}
