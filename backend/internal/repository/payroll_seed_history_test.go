package repository

import (
	"context"
	"encoding/json"
	"mserp/internal/db"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollSparseHistoricalPreparationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		_, err := pool.Exec(ctx, `INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,payment_type)
 SELECT '18200000-0000-0000-0000-000000009801',s.id,s.name,'Historical invoice','0205-09-30',100,
 '18200000-0000-0000-0000-000000000001','18200000-0000-0000-0000-000000000001',''
 FROM expense_settings s WHERE s.kind='category' AND s.name='Other';
 INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES('18200000-0000-0000-0000-000000000001',date_trunc('week','2300-01-01'::date));
 INSERT INTO expense_payments(expense_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000009801','2026-08-03',10);
 INSERT INTO payroll_projection_jobs(week_start,priority)
 SELECT d::date,10 FROM generate_series(date_trunc('week','0206-01-01'::date),'0207-01-01'::date,interval '7 days') d;
 INSERT INTO payroll_projection_jobs(week_start,priority,completed_generation) VALUES
 ('1900-01-01',100,0),('1900-01-08',10,1),('1900-01-15',10,0),('1900-01-22',10,0);
 UPDATE payroll_projection_jobs SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '1 hour' WHERE week_start='1900-01-15';
 UPDATE payroll_projection_jobs SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()-interval '1 hour' WHERE week_start='1900-01-22'`)
		if err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		// Model the old backfill's already-materialized empty calendar gaps.
		emptyWeek, _ := chargeWeek("2001-01-01")
		if err = repo.RequestPayrollPreparation(ctx, emptyWeek, false); err != nil {
			t.Fatal(err)
		}
		var emptyJob payrollProjectionJob
		err = pool.QueryRow(ctx, `UPDATE payroll_projection_jobs SET lease_token=gen_random_uuid(),lease_until=now()+interval '1 minute' WHERE week_start=$1 RETURNING week_start,requested_generation,lease_token::text,impact_roots`, emptyWeek).Scan(&emptyJob.Week, &emptyJob.Generation, &emptyJob.Token, &emptyJob.Roots)
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.preparePayrollProjection(ctx, emptyJob); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO payroll_projection_generations(id,week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 SELECT gen_random_uuid(),d::date,1,g.input_snapshot,g.calendar_week,g.calculator_version,g.schema_version,g.dependencies,
 jsonb_set(g.driver_header,'{weekStart}',to_jsonb(d::date::text)),jsonb_set(g.investor_header,'{weekStart}',to_jsonb(d::date::text))
 FROM payroll_projection_generations g CROSS JOIN generate_series('2001-01-08'::date,'2005-12-26'::date,interval '7 days') d WHERE g.week_start='2001-01-01';
 INSERT INTO payroll_projection_weeks SELECT week_start,id FROM payroll_projection_generations WHERE week_start BETWEEN '2001-01-08' AND '2005-12-26';
 INSERT INTO payroll_projection_jobs(week_start,priority,completed_generation) SELECT week_start,10,1 FROM payroll_projection_weeks WHERE week_start BETWEEN '2001-01-08' AND '2005-12-26';
 UPDATE payroll_projection_jobs SET priority=10 WHERE week_start='2001-01-01'`)
		if err != nil {
			t.Fatal(err)
		}
		// Seed a pre-policy future projection through the worker's internal entry.
		current, _ := chargeWeek(ChargeCurrentWeek())
		future := current.AddDate(0, 0, 7)
		if _, err = pool.Exec(ctx, `INSERT INTO payroll_projection_jobs(week_start,priority) VALUES($1,100)`, future); err != nil {
			t.Fatal(err)
		}
		var job payrollProjectionJob
		err = pool.QueryRow(ctx, `UPDATE payroll_projection_jobs SET lease_token=gen_random_uuid(),lease_until=now()+interval '1 minute' WHERE week_start=$1 RETURNING week_start,requested_generation,lease_token::text,impact_roots`, future).Scan(&job.Week, &job.Generation, &job.Token, &job.Roots)
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.preparePayrollProjection(ctx, job); err != nil {
			t.Fatal(err)
		}
		var retained int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_generations`).Scan(&retained); err != nil {
			t.Fatal(err)
		}
		if retained < 250 {
			t.Fatal("historical fan-out fixture is too small", retained)
		}
		week, _ := chargeWeek("2026-10-05")
		before, err := repo.PayrollPreview(ctx, "driver", week)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err = repo.SeedPayrollPreparation(ctx); err != nil {
				t.Fatal(err)
			}
		}
		var jobs, kept, removed int
		err = pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE week_start IN (
 date_trunc('week','0205-09-30'::date),'2026-08-03','1900-01-01','1900-01-15')),
 count(*) FILTER(WHERE week_start BETWEEN '0206-01-01' AND '0207-01-01' OR week_start IN ('1900-01-08','1900-01-22'))
 FROM payroll_projection_jobs`).Scan(&jobs, &kept, &removed)
		if err != nil || jobs >= 30 || kept != 4 || removed != 0 {
			t.Fatal("retain real source/saved periods, explicit past requests and active leases; retire empty calendar gaps", jobs, kept, removed, err)
		}
		var clean bool
		err = pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM payroll_projection_weeks WHERE week_start>$1::date OR week_start BETWEEN '2001-01-01' AND '2005-12-26')
 AND NOT EXISTS(SELECT 1 FROM payroll_projection_jobs WHERE week_start>$1::date)
 AND NOT EXISTS(SELECT 1 FROM payroll_statement_state WHERE week_start>$1::date)
 AND (SELECT count(*) FROM payroll_projection_generations)=$2
 AND EXISTS(SELECT 1 FROM driver_pay_weeks WHERE week_start=date_trunc('week','2300-01-01'::date))`, current, retained).Scan(&clean)
		if err != nil || !clean {
			t.Fatal("retirement lost immutable history/financial records or retained future/empty projections", clean, err)
		}
		// The worker itself must also skip a future request, even before seeding.
		if err = repo.RequestPayrollPreparation(ctx, future, false); err == nil {
			t.Fatal("accepted future preparation request")
		}
		if _, err = pool.Exec(ctx, `INSERT INTO payroll_projection_jobs(week_start,priority) VALUES($1,100)`, future); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `UPDATE payroll_projection_jobs SET completed_generation=requested_generation WHERE week_start<=$1`, current)
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || worked {
			t.Fatal("worker prepared a future payroll week", worked, err)
		}
		if future.Format(time.DateOnly) <= ChargeCurrentWeek() {
			t.Fatal("future fixture crossed calendar boundary")
		}
		after, err := repo.PayrollPreview(ctx, "driver", week)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(before)
		b, _ := json.Marshal(after)
		if string(a) != string(b) {
			t.Fatal("derived seeding changed the financial report")
		}
		var unchanged bool
		err = pool.QueryRow(ctx, `SELECT expense_date='0205-09-30' AND amount=100 AND
 (SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id)=10
 FROM expenses e WHERE id='18200000-0000-0000-0000-000000009801'`).Scan(&unchanged)
		if err != nil || !unchanged {
			t.Fatal("historical source date, invoice or payment changed", err)
		}
		// An ordinary expense save must not reconstruct the retired centuries.
		if err = repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if err = repo.RequestPayrollPreparation(ctx, week, true); err != nil {
			t.Fatal(err)
		}
		if err = repo.prioritizePayrollWeek(ctx, week); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		var category, actor string
		if err = pool.QueryRow(ctx, `SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`).Scan(&category); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('retirement expense',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		expenses := NewExpenseRepository(pool)
		input := ExpenseInput{CategoryID: category, ExpenseDate: week, DriverID: payTestString(projectionDriver), Amount: "20", ExpenseType: payTestString("Save after retirement"), CoveredBy: payTestString("Driver")}
		entry, err := expenses.CreateExpense(WithPayrollPublication(ctx), input)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"edit", "verify"} {
			measured, metrics := db.WithMetrics(WithPayrollPublication(ctx))
			started := time.Now()
			if action == "edit" {
				input.Amount = "35"
				entry, err = expenses.UpdateExpense(measured, entry.ID, input)
			} else {
				entry, err = expenses.VerifyExpense(measured, entry.ID, category, "accountant", entry.UpdatedAt, AuthUser{ID: actor, Username: "retirement expense", Administrator: true})
			}
			if err != nil {
				t.Fatal(action, err)
			}
			calculations := metrics.Events()["payroll_calculation"]
			if calculations > 2 {
				t.Fatal("expense save reconstructed retired/future periods", action, calculations)
			}
			t.Logf("expense %s: %s, %d payroll calculations", action, time.Since(started), calculations)
			read, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			full, err := readPayrollProjectionReference(ctx, read, week, true)
			_ = read.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for i, kind := range []string{"driver", "investor"} {
				page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
				a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
				b, _ := json.Marshal(page.Report)
				if err != nil || page.State != "ready" || string(a) != string(b) {
					t.Fatal("expense save did not atomically publish exact totals", action, kind, page.State, err)
				}
			}
		}
	})
}
