package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

func TestInvestorRetainedPayDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`DELETE FROM gross_board_entries; DELETE FROM loads`)
		latest, _ := chargeWeek(PayrollLatestWeek())
		first := latest.AddDate(0, 0, -28)
		week := func(i int) time.Time { return first.AddDate(0, 0, i*7) }
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Retained pay tester',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		for i, amount := range map[int]string{0: "100.00", 2: "50.00"} {
			exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits)
 VALUES($1::uuid,'18200000-0000-0000-0000-000000000003',$2::date,jsonb_build_object('driverId',$1::uuid::text,'weekStart',$2::date,'adjustments',jsonb_build_array(jsonb_build_object('kind','addition','amount',$3::text))))`, projectionTruck, week(i), amount)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(true)
		repo.ConfigurePreparedFinalization(true)
		repo.resourceConcurrency = true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`)
		for i := 0; i <= 4; i++ {
			if err := repo.RequestPayrollPreparation(ctx, week(i), false); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
		}
		read := func(i int, carry, retained string, net int64) DriverPayWeek {
			t.Helper()
			page, err := repo.PreparedPayroll(ctx, "investor", week(i), PayPageQuery{ID: projectionTruck}, false)
			if err != nil {
				t.Fatal(err)
			}
			live, err := repo.InvestorStatement(ctx, week(i), projectionTruck)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range []DriverPayWeek{page, live} {
				if len(v.Drivers) != 1 {
					t.Fatalf("week %d missing statement: %+v", i, v)
				}
				d := v.Drivers[0]
				if d.InvestorCarry != carry || d.RetainedPay != retained || d.NetPayCents() != net {
					t.Fatalf("week %d expected %s/%s/%d got carry=%s retained=%s net=%d", i, carry, retained, net, d.InvestorCarry, d.RetainedPay, d.NetPayCents())
				}
			}
			return page
		}
		seq := 6000
		settle := func(page DriverPayWeek, reopen, keep bool) (DriverPayWeek, error) {
			seq++
			w, _ := chargeWeek(page.WeekStart)
			return repo.SettleInvestor(WithPayrollReceipt(ctx), w, projectionTruck, page.Revision, actor, "", reopen, false, PayrollSettlementCommand{RequestID: fmt.Sprintf("18200000-0000-0000-0000-%012d", seq), ExpectedStatementRevision: page.Drivers[0].StatementRevision, KeepMoney: keep})
		}
		p := read(0, "", "", 10000)
		seq++
		opt := PayrollSettlementCommand{RequestID: fmt.Sprintf("18200000-0000-0000-0000-%012d", seq), ExpectedStatementRevision: p.Drivers[0].StatementRevision, KeepMoney: true}
		receipt, err := repo.SettleInvestor(WithPayrollReceipt(ctx), week(0), projectionTruck, p.Revision, actor, "", false, false, opt)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Drivers[0].NetPayCents() != 0 || receipt.Drivers[0].RetainedPay != "100.00" {
			t.Fatalf("bad retained receipt: %+v", receipt)
		}
		if _, err = repo.SettleInvestor(WithPayrollReceipt(ctx), week(0), projectionTruck, p.Revision, actor, "", false, false, opt); err != nil {
			t.Fatalf("retry: %v", err)
		}
		opt.KeepMoney = false
		if _, err = repo.SettleInvestor(WithPayrollReceipt(ctx), week(0), projectionTruck, p.Revision, actor, "", false, false, opt); err == nil {
			t.Fatal("changed retry paid retained money")
		}
		read(0, "", "100.00", 0)
		read(1, "100.00", "", 10000)
		next := read(2, "100.00", "", 15000)
		if next.Drivers[0].InvestorCarryWeek != week(0).Format(time.DateOnly) {
			t.Fatal("missing source week")
		}
		if _, err = settle(next, false, true); err != nil {
			t.Fatal(err)
		}
		read(2, "100.00", "150.00", 0)
		read(3, "150.00", "", 15000)
		if _, err = settle(read(0, "", "100.00", 0), true, false); err == nil || !strings.Contains(err.Error(), "later statement") {
			t.Fatalf("unsafe reopen: %v", err)
		}
		if _, err = settle(read(1, "", "", 0), false, true); err == nil {
			t.Fatal("out of order retention allowed")
		}
		if _, err = settle(read(3, "150.00", "", 15000), false, false); err != nil {
			t.Fatal(err)
		}
		read(3, "150.00", "", 15000)
		read(4, "", "", 0)
		if _, err = settle(read(3, "150.00", "", 15000), true, false); err != nil {
			t.Fatal(err)
		}
		read(4, "150.00", "", 15000)
		if _, err = settle(read(2, "100.00", "150.00", 0), true, false); err != nil {
			t.Fatal(err)
		}
		read(2, "100.00", "", 15000)
		read(4, "100.00", "", 10000)
		if _, err = settle(read(0, "", "100.00", 0), true, false); err != nil {
			t.Fatal(err)
		}
		read(0, "", "", 10000)
		read(4, "", "", 0)
		if _, err = settle(read(4, "", "", 0), false, true); err == nil {
			t.Fatal("zero retention allowed")
		}
	})
}

func TestInvestorRetainedPayWithoutTermsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		const owner = "18200000-0000-0000-0000-000000006003"
		latest, _ := chargeWeek(PayrollLatestWeek())
		prior := latest.AddDate(0, 0, -7)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Retained fallback tester',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO investors(id,full_name) VALUES('18200000-0000-0000-0000-000000006003','Original Investor');
 INSERT INTO trucks(id,unit_number,owner_id,active) VALUES('18200000-0000-0000-0000-000000000002','INACTIVE-KEEP','18200000-0000-0000-0000-000000006003',false)`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report) VALUES($1,$2,$3,'{}',true,jsonb_build_object('id',$1::uuid::text,'truckId',$1::uuid::text,'truckUnit','INACTIVE-KEEP','investorId',$3::uuid::text,'fullName','Original Investor','retainedPay','100.00','loads',jsonb_build_array(jsonb_build_object('fee','100.00')),'settlement',jsonb_build_object('finalized',true),'edits',jsonb_build_object('driverId',$1::uuid::text,'weekStart',$2::date)))`, projectionTruck, prior, owner); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		page, err := repo.InvestorPay(ctx, latest)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Drivers) != 1 || page.Drivers[0].InvestorCarry != "100.00" || page.Drivers[0].NetPayCents() != 10000 || len(page.Drivers[0].Loads) != 0 {
			t.Fatalf("lost or replayed inactive balance: %+v", page)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		changed := page.Drivers[0]
		changed.InvestorID = "18200000-0000-0000-0000-000000006004"
		err = guardInvestorCarryOrder(ctx, tx, changed, page.WeekStart, false)
		tx.Rollback(ctx)
		if err == nil || !strings.Contains(err.Error(), "previous investor") {
			t.Fatalf("transferred retained money to new investor: %v", err)
		}
		paid, err := repo.SettleInvestor(ctx, latest, projectionTruck, page.Revision, actor, "", false, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(paid.Drivers) != 1 || paid.Drivers[0].NetPayCents() != 10000 || paid.Drivers[0].RetainedPay != "" || !paid.Drivers[0].Settlement.Finalized {
			t.Fatalf("inactive payout failed: %+v", paid)
		}
	})
}
