package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPaidExpenseDeletionPublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Delete publication',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		expenses := NewExpenseRepository(pool)
		category, err := NewMaintenanceRepository(pool).CategoryID(ctx)
		if err != nil {
			t.Fatal(err)
		}
		driver, truck := projectionDriver, projectionTruck
		e, err := expenses.CreateExpense(ctx, ExpenseInput{CategoryID: category, TruckID: &truck, DriverID: &driver, ExpenseDate: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), Amount: "100", CoveredBy: payTestString("Driver"), ExpenseType: payTestString("Road service")})
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO expense_external_settlements(id,expense_id,amount,settled_on,reference,actor_id) VALUES(gen_random_uuid(),$1,25,'2026-10-06','Paid test',$2)`, e.ID, actor)
		exec(`UPDATE expenses SET accounting_verified=true WHERE id=$1`, e.ID)
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `CREATE FUNCTION fail_expense_delete_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced publication failure'; END $$; CREATE TRIGGER fail_expense_delete_publication BEFORE INSERT ON payroll_projection_generations FOR EACH ROW EXECUTE FUNCTION fail_expense_delete_publication()`); err != nil {
			t.Fatal(err)
		}
		if err := expenses.DeleteExpense(WithPayrollPublication(ctx), e.ID, "Tester"); err == nil {
			t.Fatal("deletion committed without publication")
		}
		var unchanged bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expenses WHERE id=$1) AND EXISTS(SELECT 1 FROM maintenance_records WHERE expense_id=$1 AND status<>'voided') AND EXISTS(SELECT 1 FROM expense_external_settlements WHERE expense_id=$1 AND expense_snapshot IS NULL)`, e.ID).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("failed publication did not roll back void and receipt archival", err)
		}
		if _, err := admin.Exec(ctx, `DROP TRIGGER fail_expense_delete_publication ON payroll_projection_generations; DROP FUNCTION fail_expense_delete_publication()`); err != nil {
			t.Fatal(err)
		}
		if err := expenses.DeleteExpense(WithPayrollPublication(ctx), e.ID, "Tester"); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, tx, week, true)
		_ = tx.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			page, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if err != nil || page.State != "ready" || string(a) != string(b) {
				t.Fatal("expense deletion publication differs from calculator", kind, err)
			}
		}
	})
}
