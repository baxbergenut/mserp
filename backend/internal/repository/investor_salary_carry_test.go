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

func TestInvestorSalaryCarryDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		const driver = "18200000-0000-0000-0000-000000005001"
		const owner = "18200000-0000-0000-0000-000000005002"
		latest, _ := chargeWeek(PayrollLatestWeek())
		first := latest.AddDate(0, 0, -28)
		week := func(i int) time.Time { return first.AddDate(0, 0, i*7) }
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash)
 VALUES('Salary carry tester',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}

		if _, err := pool.Exec(ctx, `INSERT INTO investors(id,full_name) VALUES($1,'Carry Investor')`, owner); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO trucks(id,unit_number,owner_id) VALUES($1,'CARRY-TRUCK',$2)`, driver, owner); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report)
 VALUES($1,$2,$3,'{}',true,jsonb_build_object('id',$1::uuid::text,'truckId',$1::uuid::text,'investorId',$3::uuid::text,'fullName','Carry Investor',
 'settlement',jsonb_build_object('finalized',true),
 'edits',jsonb_build_object('driverId',$1::uuid::text,'weekStart',$2::date,
 'adjustments',jsonb_build_array(jsonb_build_object('kind','deduction','name','Legacy deduction','amount','150.00')))))`, driver, week(0), owner); err != nil {
			t.Fatal(err)
		}
		for i, credit := range map[int]string{2: "60.00", 3: "120.00"} {
			if _, err := pool.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits)
 VALUES($1,$2,$3,jsonb_build_object('driverId',$1::uuid::text,'weekStart',$2::date,'adjustments',jsonb_build_array(jsonb_build_object('kind','addition','name','Test earnings','amount',$4::text))))`, driver, week(i), owner, credit); err != nil {
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
		read := func(i, source int, carry string, net int64) DriverPayWeek {
			t.Helper()
			w := week(i)
			sourceWeek := ""
			if source >= 0 {
				sourceWeek = week(source).Format(time.DateOnly)
			}
			page, err := repo.PreparedPayroll(ctx, "investor", w, PayPageQuery{ID: driver}, false)
			if err != nil {
				t.Fatal(w, err)
			}
			live, err := repo.InvestorStatement(ctx, w, driver)
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
			_, err := repo.SettleInvestor(WithPayrollReceipt(ctx), w, driver, page.Revision, actor, "", reopen, false, PayrollSettlementCommand{
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
		page, err := repo.PreparedPayroll(ctx, "investor", week(4), PayPageQuery{ID: driver}, false)
		if err != nil || len(page.Drivers) != 0 {
			t.Fatalf("settled balance carried again: %+v %v", page, err)
		}
		if err := settle(read(3, 2, "90.00", 3000), true); err != nil {
			t.Fatal(err)
		}
		read(4, 2, "90.00", -9000)

		var newOwner string
		if err := pool.QueryRow(ctx, `INSERT INTO investors(full_name) VALUES('New Owner') RETURNING id::text`).Scan(&newOwner); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trucks SET owner_id=$2 WHERE id=$1`, driver, newOwner); err != nil {
			t.Fatal(err)
		}
		after, err := repo.InvestorStatement(ctx, week(4), driver)
		if err != nil || len(after.Drivers) != 0 {
			t.Fatalf("debt transferred to a new investor: %+v %v", after, err)
		}
		var mutated bool
		if err := pool.QueryRow(ctx, `SELECT report ? 'salaryCarry' FROM investor_pay_weeks WHERE truck_id=$1 AND week_start=$2`, driver, week(0)).Scan(&mutated); err != nil || mutated {
			t.Fatalf("legacy snapshot was rewritten: %v %v", mutated, err)
		}
	})
}
