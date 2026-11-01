package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollRetirementDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		_, err := pool.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,fuel_override) VALUES('18200000-0000-0000-0000-000000000001','2026-10-05',0);
 INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits) VALUES('18200000-0000-0000-0000-000000000002','2026-10-05','18200000-0000-0000-0000-000000000003','{}');
 INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'Retained invoice','2026-10-05',100,'18200000-0000-0000-0000-000000000001','Driver' FROM expense_settings WHERE kind='category' AND name='Other';
 INSERT INTO expense_payments(expense_id,week_start,amount) SELECT id,'2026-10-05',30 FROM expenses WHERE expense_type='Retained invoice';`)
		if err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err = repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		old, _ := chargeWeek("2026-10-05")
		if err = repo.RequestPayrollPreparation(ctx, old, false); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		var retained int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_statement_versions`).Scan(&retained); err != nil {
			t.Fatal(err)
		}
		result, err := repo.RetirePayrollBefore(ctx, "2026-10-19")
		if err != nil || result.DriverWeeks != 1 || result.InvestorWeeks != 1 || result.VerifiedWeeks < 1 {
			t.Fatal(result, err)
		}
		var versions, statements, payments, receipts int
		if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM payroll_statement_versions),(SELECT count(*) FROM payroll_statement_state WHERE week_start<'2026-10-19'),(SELECT count(*) FROM expense_payments WHERE amount=30),(SELECT count(*) FROM payroll_retirement_receipts)`).Scan(&versions, &statements, &payments, &receipts); err != nil || versions != retained || statements != 0 || payments != 1 || receipts != 1 {
			t.Fatal("retirement lost history or collections", versions, statements, payments, receipts, err)
		}
		if err = repo.SeedPayrollPreparation(ctx); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_jobs WHERE week_start<'2026-10-19'`).Scan(&statements); err != nil || statements != 0 {
			t.Fatal("retired periods were seeded", statements, err)
		}
		if err = repo.RequestPayrollPreparation(ctx, old, false); err == nil {
			t.Fatal("retired week was reopened")
		}
		if _, err = pool.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES('18200000-0000-0000-0000-000000000001','2026-10-05')`); err == nil {
			t.Fatal("database accepted a retired statement")
		}
	})
}
