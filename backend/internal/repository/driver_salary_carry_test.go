package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDriverSalaryCarryDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		const driver = "18200000-0000-0000-0000-000000005001"
		latest, _ := chargeWeek(PayrollLatestWeek())
		first := latest.AddDate(0, 0, -28)
		week := func(i int) time.Time { return first.AddDate(0, 0, i*7) }
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash)
 VALUES('Salary carry tester',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,active,pay_type,pay_rate)
 VALUES($1,'Salary Carry Driver','salary carry driver',true,'cpm',0.75)`, driver); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,report)
 VALUES($1::uuid,$2::date,jsonb_build_object('id',$1::uuid::text,'fullName','Salary Carry Driver',
 'edits',jsonb_build_object('driverId',$1::uuid::text,'weekStart',$2::date,
 'generatedCharges',jsonb_build_array(jsonb_build_object('name','Insurance min','amount','-150.00')))))`, driver, week(0)); err != nil {
			t.Fatal(err)
		}
		for i, credit := range map[int]string{2: "60.00", 3: "120.00"} {
			if _, err := pool.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,adjustments)
 VALUES($1,$2,jsonb_build_array(jsonb_build_object('kind','addition','name','Test wages','amount',$3::text)))`, driver, week(i), credit); err != nil {
				t.Fatal(err)
			}
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(true)
		repo.ConfigurePreparedFinalization(true)
		repo.resourceConcurrency = true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		for i := 0; i <= 4; i++ {
			if err := repo.RequestPayrollPreparation(ctx, week(i), false); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
		}
		read := func(i int, carry string, net int64) DriverPayWeek {
			t.Helper()
			w := week(i)
			page, err := repo.PreparedPayroll(ctx, "driver", w, PayPageQuery{ID: driver}, false)
			if err != nil {
				t.Fatal(w, err)
			}
			live, err := repo.GetDriverWeek(ctx, w, driver)
			if err != nil {
				t.Fatal(w, err)
			}
			for _, report := range []DriverPayWeek{page, live} {
				if len(report.Drivers) != 1 || report.Drivers[0].SalaryCarry != carry || report.Drivers[0].NetPayCents() != net {
					t.Fatalf("week %s: expected carry %q and net %d, got %+v", w.Format(time.DateOnly), carry, net, report.Drivers)
				}
			}
			return page
		}
		seq := 5000
		settle := func(page DriverPayWeek, reopen bool) error {
			seq++
			w, _ := chargeWeek(page.WeekStart)
			d := page.Drivers[0]
			_, err := repo.Settle(WithPayrollReceipt(ctx), w, driver, page.Revision, actor, "", reopen, false, PayrollSettlementCommand{
				RequestID: fmt.Sprintf("18200000-0000-0000-0000-%012d", seq), ExpectedStatementRevision: d.StatementRevision, ExpenseRevision: d.ExpenseRevision,
			})
			return err
		}
		read(1, "150.00", -15000)
		read(4, "150.00", -15000)
		if err := settle(read(2, "150.00", -9000), false); err != nil {
			t.Fatal(err)
		}
		if err := settle(read(0, "", -15000), true); err == nil || !strings.Contains(err.Error(), "later statement") {
			t.Fatalf("reopened a shortfall already used by later frozen payroll: %v", err)
		}
		if err := settle(read(3, "90.00", 3000), false); err != nil {
			t.Fatal(err)
		}
		page, err := repo.PreparedPayroll(ctx, "driver", week(4), PayPageQuery{ID: driver}, false)
		if err != nil || len(page.Drivers) != 0 {
			t.Fatalf("settled balance carried again: %+v %v", page, err)
		}
		if err := settle(read(3, "90.00", 3000), true); err != nil {
			t.Fatal(err)
		}
		read(4, "90.00", -9000)
	})
}
