package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInvestorTruckAccountsUpgradeDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_CHARGES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable database required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.Database, "_test") || (cfg.Host != "127.0.0.1" && cfg.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	schema := pgx.Identifier{fmt.Sprintf("investor_accounts_upgrade_%d", time.Now().UnixNano())}.Sanitize()
	exec := func(sql string) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE SCHEMA " + schema + "; SET search_path TO " + schema + ",public")
	defer conn.Exec(ctx, "SET search_path TO public; DROP SCHEMA "+schema+" CASCADE")
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/214_investor_truck_accounts.sql")
	if err != nil {
		t.Fatal(err)
	}
	before := strings.ReplaceAll(string(init), "\r\n", "\n")
	upgrade := strings.ReplaceAll(string(migration), "\r\n", "\n")
	if strings.Count(before, upgrade) != 1 {
		t.Fatal("migration is not present exactly once in init.sql")
	}
	exec(strings.Replace(before, upgrade, "", 1))
	exec(`INSERT INTO investors(id,full_name) VALUES('20200000-0000-0000-0000-000000000001','Upgrade Investor');
 INSERT INTO trucks(id,unit_number,owner_id) VALUES('20200000-0000-0000-0000-000000000002','043','20200000-0000-0000-0000-000000000001');
 INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,finalized,edits,report) VALUES('20200000-0000-0000-0000-000000000002','20200000-0000-0000-0000-000000000001','2026-10-05',true,'{}','{"unchanged":true}');
 INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('20200000-0000-0000-0000-000000000010','Upgrade Owner','upgrade owner','gross_percentage',90);
 INSERT INTO investors(id,full_name,driver_id) VALUES('20200000-0000-0000-0000-000000000011','Upgrade Owner','20200000-0000-0000-0000-000000000010');
 INSERT INTO trucks(id,unit_number,owner_id) VALUES('20200000-0000-0000-0000-000000000012','OWN-045','20200000-0000-0000-0000-000000000011');
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('20200000-0000-0000-0000-000000000012','20200000-0000-0000-0000-000000000010','2026-01-01');
 UPDATE payroll_projection_control SET protocol_mode='resources',capture_enabled=true WHERE singleton`)
	exec(upgrade)
	var valid bool
	err = conn.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM driver_escrows WHERE truck_id='20200000-0000-0000-0000-000000000012') AND (SELECT count(*)=1 FROM driver_escrows WHERE truck_id='20200000-0000-0000-0000-000000000002')
 AND (SELECT e.amount=s.default_amount AND e.start_date=date_trunc('week',now() AT TIME ZONE 'America/New_York')::date AND e.opening_paid=0 FROM driver_escrows e CROSS JOIN driver_escrow_settings s WHERE e.truck_id='20200000-0000-0000-0000-000000000002')
 AND (SELECT report='{"unchanged":true}'::jsonb AND finalized FROM investor_pay_weeks WHERE truck_id='20200000-0000-0000-0000-000000000002')`).Scan(&valid)
	if err != nil || !valid {
		t.Fatal("migration changed historical money or failed enrollment", valid, err)
	}
}

func TestInvestorTruckAccountsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		var owner, truck string
		if err := pool.QueryRow(ctx, `INSERT INTO investors(full_name) VALUES('Account Investor') RETURNING id::text`).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO trucks(unit_number,owner_id) VALUES('043', $1) RETURNING id::text`, owner).Scan(&truck); err != nil {
			t.Fatal(err)
		}
		current, _ := chargeWeek(ChargeCurrentWeek())
		week := current.AddDate(0, 0, -7).Format(time.DateOnly)
		if _, err := pool.Exec(ctx, `INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,$3,90)`, truck, owner, week); err != nil {
			t.Fatal(err)
		}
		var escrow, amount, expected, start string
		if err := pool.QueryRow(ctx, `SELECT e.id::text,e.amount::text,s.default_amount::text,e.start_date::text FROM driver_escrows e CROSS JOIN driver_escrow_settings s WHERE e.truck_id=$1`, truck).Scan(&escrow, &amount, &expected, &start); err != nil {
			t.Fatal(err)
		}
		if amount != expected || start != ChargeCurrentWeek() {
			t.Fatalf("default %s/%s start %s", amount, expected, start)
		}
		if _, err := pool.Exec(ctx, `UPDATE driver_escrows SET start_date=$2,amount=500 WHERE id=$1`, escrow, week); err != nil {
			t.Fatal(err)
		}
		charges := NewDriverChargeRepository(pool)
		ids, err := charges.Create(ctx, ChargeCreate{TruckID: truck, Kind: "installment", Name: "Truck repair", Total: "300", Amount: "100", StartWeek: week, Eligibility: "calendar"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		data, err := charges.List(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, s := range data.Schedules {
			if s.ID == ids[0] {
				found = s.TruckID == truck && s.OwnerID == owner && s.DriverName == "Account Investor - Truck 043"
			}
		}
		if !found {
			t.Fatalf("missing truck installment: %+v", data.Schedules)
		}
		escrows := NewEscrowRepository(pool)
		page, err := escrows.List(ctx, EscrowQuery{})
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, e := range page.Items {
			if e.ID == escrow {
				found = e.DriverName == "Account Investor - Truck 043" && e.DriverID == nil
			}
		}
		if !found {
			t.Fatalf("missing investor escrow: %+v", page)
		}
		pay := NewDriverPayRepository(pool)
		date, _ := chargeWeek(week)
		report, err := pay.InvestorPay(ctx, date, truck)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Drivers) != 1 {
			t.Fatalf("missing statement: %+v", report)
		}
		d := report.Drivers[0]
		if len(d.Edits.GeneratedCharges) != 1 || len(d.Edits.ExpenseDeductions) != 1 {
			t.Fatalf("missing accounts: %+v", d.Edits)
		}
		d.Edits.ExpenseDeductions[0].Amount = "50.00"
		d.Edits.ExpenseDeductions[0].Apply = true
		d.Edits.GeneratedCharges[0].Amount = "-25.00"
		if _, err = pay.SaveInvestorPay(ctx, d.Edits, actor); err != nil {
			t.Fatal(err)
		}
		report, err = pay.InvestorPay(ctx, date, truck)
		if err != nil {
			t.Fatal(err)
		}
		if report.Drivers[0].Edits.GeneratedCharges[0].Amount != "-25.00" {
			t.Fatal("installment override lost")
		}
		report, err = pay.SettleInvestor(ctx, date, truck, report.Revision, actor, "", false, true)
		if err != nil {
			t.Fatal(err)
		}
		var confirmed bool
		if err = pool.QueryRow(ctx, `SELECT confirmed_at IS NOT NULL FROM driver_charge_occurrences WHERE schedule_id=$1 AND week_start=$2`, ids[0], week).Scan(&confirmed); err != nil || !confirmed {
			t.Fatalf("not confirmed: %v", err)
		}
		if _, err = pool.Exec(ctx, `UPDATE driver_escrow_payments SET amount=10 WHERE escrow_id=$1`, escrow); err == nil {
			t.Fatal("finalized escrow was editable")
		}
		report, err = pay.InvestorPay(ctx, date, truck)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pay.SettleInvestor(ctx, date, truck, report.Revision, actor, "", true, false); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT confirmed_at IS NOT NULL FROM driver_charge_occurrences WHERE schedule_id=$1 AND week_start=$2`, ids[0], week).Scan(&confirmed); err != nil || confirmed {
			t.Fatalf("confirmation not reopened: %v", err)
		}
		var version int
		if err = pool.QueryRow(ctx, `SELECT balance_version FROM driver_escrows WHERE id=$1`, escrow).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if err = escrows.SaveOpening(ctx, escrow, actor, EscrowOpeningInput{OpeningPaid: "200", Version: version, Reason: "Verified opening funds"}); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT balance_version FROM driver_escrows WHERE id=$1`, escrow).Scan(&version); err != nil {
			t.Fatal(err)
		}
		var release string
		if err = pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&release); err != nil {
			t.Fatal(err)
		}
		if err = escrows.SaveRelease(ctx, escrow, EscrowReleaseInput{ID: release, WeekStart: ChargeCurrentWeek(), Amount: "50", EscrowVersion: version}, actor); err != nil {
			t.Fatal(err)
		}
		live, err := pay.InvestorPay(ctx, current, truck)
		if err != nil {
			t.Fatal(err)
		}
		credit := false
		for _, c := range live.Drivers[0].AutoCharges {
			if c.Source == "escrow-release:"+release {
				credit = c.Amount == "50.00"
			}
		}
		if !credit {
			t.Fatal("investor release credit missing")
		}
		page, err = escrows.List(ctx, EscrowQuery{})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Items {
			if e.ID == escrow && (e.HeldAmount != "200.00" || len(e.Releases) != 1 || !e.Releases[0].Editable) {
				t.Fatalf("wrong release balance/history: %+v", e)
			}
		}
		for _, balance := range []string{"175.00", "200.00"} {
			if err = pool.QueryRow(ctx, "SELECT balance_version FROM driver_escrows WHERE id=$1", escrow).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err = escrows.SaveBalance(ctx, escrow, actor, EscrowBalanceInput{Balance: balance, Version: version, Reason: "Verified investor funds"}); err != nil {
				t.Fatal(err)
			}
			accountPage, e := escrows.List(ctx, EscrowQuery{})
			if e != nil {
				t.Fatal(e)
			}
			for _, account := range accountPage.Items {
				if account.ID == escrow && (account.HeldAmount != balance || account.ReleasedAmount != "50.00") {
					t.Fatalf("correction changed release history: %+v", account)
				}
			}
			statement, e := pay.InvestorPay(ctx, current, truck)
			if e != nil {
				t.Fatal(e)
			}
			for _, c := range statement.Drivers[0].AutoCharges {
				if strings.HasPrefix(c.Source, "escrow-release:") && c.Source != "escrow-release:"+release {
					t.Fatal("balance correction created a payroll credit", c)
				}
			}
		}
		// Exercise the production resource protocol, including prepared commands
		// and publication of escrow and installment resource revisions.
		pay.resourceConcurrency = true
		pay.ConfigurePreparedFinalization(true)
		pay.ConfigurePreparedReads(true)
		if err = pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		if _, err = settlePayrollFixture(t, ctx, pay, "investor", date, truck, "", actor, "", false, true, true); err != nil {
			t.Fatal("prepared finalization", err)
		}
		if _, err = settlePayrollFixture(t, ctx, pay, "investor", date, truck, "", actor, "", true, false, true); err != nil {
			t.Fatal("prepared reopen", err)
		}
		prepared, err := pay.PreparedPayroll(ctx, "investor", date, PayPageQuery{ID: truck}, true)
		if err != nil || prepared.Preparation.State != "ready" || len(prepared.Drivers) != 1 {
			t.Fatal("prepared read", err, prepared.Preparation)
		}
		edit := prepared.Drivers[0].Edits
		edit.DriverID = truck
		edit.WeekStart = week
		edit.ExpectedStatementRevision = prepared.Drivers[0].StatementRevision
		if err = pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&edit.RequestID); err != nil {
			t.Fatal(err)
		}
		edit.GeneratedCharges[0].Amount = "-10.00"
		if _, err = pay.SaveInvestorPay(WithPayrollReceipt(ctx), edit, actor); err != nil {
			t.Fatal("resource installment save", err)
		}
	})
}

func TestInvestorTruckAccountsExcludeOwnerOperatorsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		const driver = "20200000-0000-0000-0000-000000000010"
		const hired = "20200000-0000-0000-0000-000000000011"
		const owner = "20200000-0000-0000-0000-000000000012"
		const ownTruck = "20200000-0000-0000-0000-000000000013"
		const hiredTruck = "20200000-0000-0000-0000-000000000014"
		_, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES
 ($1,'Owner Driver','owner driver','gross_percentage',90),($2,'Hired Driver','hired driver','cpm',0.65);
 `, driver, hired)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO investors(id,full_name,driver_id) VALUES($1,'Owner Driver',$2)`, owner, driver); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO trucks(id,unit_number,owner_id) VALUES($1,'OWN-043',$3),($2,'INV-044',$3)`, ownTruck, hiredTruck, owner); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01'),($3,$4,'2026-01-01')`, ownTruck, driver, hiredTruck, hired); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$3,$4,90),($2,$3,$4,90)`, ownTruck, hiredTruck, owner, ChargeCurrentWeek()); err != nil {
			t.Fatal(err)
		}
		var ownCount, hiredCount int
		if err = pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE truck_id=$1),count(*) FILTER(WHERE truck_id=$2) FROM driver_escrows`, ownTruck, hiredTruck).Scan(&ownCount, &hiredCount); err != nil || ownCount != 0 || hiredCount != 1 {
			t.Fatalf("wrong escrow enrollment own=%d investor=%d: %v", ownCount, hiredCount, err)
		}
		repo := NewDriverChargeRepository(pool)
		input := ChargeCreate{TruckID: ownTruck, Kind: "installment", Name: "Truck plan", Total: "300", Amount: "100", StartWeek: ChargeCurrentWeek(), Eligibility: "calendar"}
		if _, err = repo.Draft(ctx, input); err == nil {
			t.Fatal("owner-operator truck preview accepted")
		}
		if _, err = repo.Create(ctx, input, actor); err == nil {
			t.Fatal("owner-operator truck installment accepted")
		}
		input.TruckID = hiredTruck
		if _, err = repo.Create(ctx, input, actor); err != nil {
			t.Fatal("non-driving investor truck rejected", err)
		}
		choices, err := repo.List(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range choices.InvestorTrucks {
			if v.ID == ownTruck {
				t.Fatal("owner-operator offered in selector")
			}
			found = found || v.ID == hiredTruck
		}
		if !found {
			t.Fatal("other truck of driving investor missing")
		}
		page, err := NewEscrowRepository(pool).List(ctx, EscrowQuery{})
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, v := range page.Items {
			if v.TruckID != nil && *v.TruckID == ownTruck {
				t.Fatal("owner-operator on investor escrow list")
			}
			found = found || v.TruckID != nil && *v.TruckID == hiredTruck
		}
		if !found {
			t.Fatal("non-driving investor escrow missing")
		}
	})
}
