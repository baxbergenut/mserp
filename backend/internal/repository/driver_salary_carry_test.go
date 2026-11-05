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
		const legacy = "18200000-0000-0000-0000-000000005002"
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
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,active,pay_type,pay_rate)
 VALUES($1,'Legacy Carry Driver','legacy carry driver',true,'cpm',0.75)`, legacy); err != nil {
			t.Fatal(err)
		}
		// The intermediate week closed after the receiving week, so it could
		// not have supplied the carry that was captured in the older snapshot.
		if _, err := pool.Exec(ctx, `INSERT INTO payroll_settlements(driver_id,week_start,finalized_at,report)
 SELECT $1::uuid,v.week_start,v.finalized_at,jsonb_build_object('id',$1::uuid::text,
 'fullName','Legacy Carry Driver','salaryCarry',v.carry,
 'edits',jsonb_build_object('driverId',$1::uuid::text,'weekStart',v.week_start,
 'adjustments',jsonb_build_array(jsonb_build_object('kind',v.kind,'amount',v.amount))))
 FROM (VALUES ($2::date,now(),'deduction','150.00',''),($3::date,now()+interval '1 second','addition','10.00',''),
 ($4::date,now(),'addition','150.00','150.00')) v(week_start,finalized_at,kind,amount,carry)`, legacy, week(0), week(1), week(2)); err != nil {
			t.Fatal(err)
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
		legacyPage, err := repo.PreparedPayroll(ctx, "driver", week(2), PayPageQuery{ID: legacy}, false)
		if err != nil || len(legacyPage.Drivers) != 1 || legacyPage.Drivers[0].SalaryCarryWeek != week(0).Format(time.DateOnly) {
			t.Fatalf("legacy frozen carry lost its source week: %+v %v", legacyPage, err)
		}
		var stored bool
		if err = pool.QueryRow(ctx, `SELECT report ? 'salaryCarryWeek' FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2`, legacy, week(2)).Scan(&stored); err != nil || stored {
			t.Fatalf("legacy frozen snapshot was rewritten: %t %v", stored, err)
		}
		read := func(i, source int, carry string, net int64) DriverPayWeek {
			t.Helper()
			w := week(i)
			sourceWeek := ""
			if source >= 0 {
				sourceWeek = week(source).Format(time.DateOnly)
			}
			page, err := repo.PreparedPayroll(ctx, "driver", w, PayPageQuery{ID: driver}, false)
			if err != nil {
				t.Fatal(w, err)
			}
			live, err := repo.GetDriverWeek(ctx, w, driver)
			if err != nil {
				t.Fatal(w, err)
			}
			for _, report := range []DriverPayWeek{page, live} {
				if len(report.Drivers) != 1 || report.Drivers[0].SalaryCarry != carry || report.Drivers[0].SalaryCarryWeek != sourceWeek || report.Drivers[0].NetPayCents() != net {
					t.Fatalf("week %s: expected carry %q from %q and net %d, got %+v", w.Format(time.DateOnly), carry, sourceWeek, net, report.Drivers)
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
		read(1, 0, "150.00", -15000)
		read(4, 0, "150.00", -15000)
		if err := settle(read(2, 0, "150.00", -9000), false); err != nil {
			t.Fatal(err)
		}
		read(2, 0, "150.00", -9000)
		if err := settle(read(0, -1, "", -15000), true); err == nil || !strings.Contains(err.Error(), "later statement") {
			t.Fatalf("reopened a shortfall already used by later frozen payroll: %v", err)
		}
		if err := settle(read(3, 2, "90.00", 3000), false); err != nil {
			t.Fatal(err)
		}
		page, err := repo.PreparedPayroll(ctx, "driver", week(4), PayPageQuery{ID: driver}, false)
		if err != nil || len(page.Drivers) != 0 {
			t.Fatalf("settled balance carried again: %+v %v", page, err)
		}
		if err := settle(read(3, 2, "90.00", 3000), true); err != nil {
			t.Fatal(err)
		}
		read(4, 2, "90.00", -9000)
	})
}
