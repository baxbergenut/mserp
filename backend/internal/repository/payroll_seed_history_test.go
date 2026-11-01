package repository

import (
	"context"
	"encoding/json"
	"testing"

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
 date_trunc('week','0205-09-30'::date),date_trunc('week','2300-01-01'::date),
 '2026-08-03','1900-01-01','1900-01-08','1900-01-15')),
 count(*) FILTER(WHERE week_start BETWEEN '0206-01-01' AND '0207-01-01' OR week_start='1900-01-22')
 FROM payroll_projection_jobs`).Scan(&jobs, &kept, &removed)
		if err != nil || jobs >= 30 || kept != 6 || removed != 0 {
			t.Fatal("sparse sources, saved future weeks, explicit requests, completed jobs and active leases must survive; empty automatic history must not", jobs, kept, removed, err)
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
	})
}
