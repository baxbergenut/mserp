package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollExpenseVerificationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor, id string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Verification metadata',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_date,amount,expense_type,driver_id,charge_driver_id,covered_by)
 SELECT id,name,'2026-10-05',25,'Verification metadata',$1,$1,'Driver' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id::text`, projectionDriver).Scan(&id); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.SwitchPayrollProtocol(ctx, "resources", 1); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			t.Helper()
			var value string
			if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
 'balance',(SELECT balance_version FROM expenses WHERE id=$1),
 'cache',(SELECT jsonb_agg(to_jsonb(r) ORDER BY table_name) FROM payroll_cache_revisions r WHERE table_name IN (SELECT table_name FROM payroll_projection_sources WHERE classification IN ('financial','display'))),
 'resources',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_key) FROM payroll_resource_revisions r),
 'changes',(SELECT count(*) FROM payroll_projection_changes),
 'versions',(SELECT count(*) FROM payroll_statement_versions))::text`, id).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := snapshot()
		repo := NewExpenseRepository(pool)
		entry, err := repo.GetExpense(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		stale := entry.UpdatedAt
		hold, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Rollback(ctx)
		if _, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`); err != nil {
			t.Fatal(err)
		}
		bounded, cancel := context.WithTimeout(WithPayrollPublication(ctx), 2*time.Second)
		defer cancel()
		user := AuthUser{ID: actor, Username: "Verification metadata", Administrator: true}
		entry, err = repo.VerifyExpense(bounded, id, entry.CategoryID, "accountant", entry.UpdatedAt, user)
		if err != nil {
			t.Fatal("verification waited for payroll gate", err)
		}
		if !entry.AccountingVerified || len(entry.Verifications) != 1 {
			t.Fatal("verification receipt missing")
		}
		entry, err = repo.UnverifyExpense(bounded, id, entry.CategoryID, entry.Verifications[0].ID, user)
		if err != nil {
			t.Fatal("unverification waited for payroll gate", err)
		}
		if entry.AccountingVerified || len(entry.Verifications) != 0 {
			t.Fatal("unverification receipt remained active")
		}
		if before != snapshot() {
			t.Fatal("verification changed payroll financial revisions or prepared statements")
		}
		if _, err = repo.VerifyExpense(bounded, id, entry.CategoryID, "accountant", stale, user); !errors.Is(err, ErrAccessConflict) {
			t.Fatal("stale edit fence lost", err)
		}
		_ = hold.Rollback(ctx)
		for _, sql := range []string{
			`UPDATE expenses SET accounting_verified=true,amount=amount+1 WHERE id=$1`,
			`UPDATE expenses SET updated_at=now() WHERE id=$1`,
			`UPDATE expenses SET accounting_verified=true,charge_driver_id=NULL WHERE id=$1`,
			`DELETE FROM expenses WHERE id=$1`,
		} {
			tx, e := repo.beginVerification(ctx)
			if e != nil {
				t.Fatal(e)
			}
			_, e = tx.Exec(ctx, sql, id)
			_ = tx.Rollback(ctx)
			if e == nil {
				t.Fatal("verification marker permitted financial write", sql)
			}
		}
		if before != snapshot() {
			t.Fatal("rejected financial changes escaped rollback")
		}
		if _, err = pool.Exec(ctx, `UPDATE expenses SET updated_at=now() WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if before == snapshot() {
			t.Fatal("ordinary collection touch lost financial revision")
		}
	})
}
