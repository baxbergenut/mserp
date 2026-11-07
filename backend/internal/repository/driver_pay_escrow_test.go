package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDriverPayEscrowSettlementDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		var driver, escrow, expense, actor, receipt string
		if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active)
 VALUES('Former Driver','former driver','cpm',0.75,false) RETURNING id`).Scan(&driver); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount,opening_paid)
 VALUES($1,'Former Driver','2026-09-28',2000,2000) RETURNING id`, driver).Scan(&escrow); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,covered_by,payment_type)
 SELECT id,name,'Settlement charge','2026-10-28',500,$1,$1,'Driver','' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id`, driver).Scan(&expense); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('escrow settlement test',crypt('test-only',gen_salt('bf'))) RETURNING id,gen_random_uuid()`).Scan(&actor, &receipt); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		week, _ := chargeWeek("2026-10-19")
		other, _ := chargeWeek("2026-10-26")
		check := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		read := func() DriverPayWeek {
			t.Helper()
			report, err := repo.GetDriverWeek(ctx, week, driver)
			check(err)
			if len(report.Drivers) != 1 {
				t.Fatalf("expected one statement, got %d", len(report.Drivers))
			}
			return report
		}
		balances := func(wantHeld, wantPaid, wantEscrow string, wantStop bool) {
			t.Helper()
			var held, paid, paidEscrow string
			var stopped bool
			check(pool.QueryRow(ctx, `SELECT (e.opening_paid-coalesce((SELECT sum(amount) FROM driver_escrow_releases WHERE escrow_id=e.id AND NOT cancelled),0))::text,
 coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=$2),0)::numeric(14,2)::text,
 coalesce((SELECT sum(escrow_amount) FROM expense_payments WHERE expense_id=$2),0)::numeric(14,2)::text,
 escrow_collection_stopped(e.id,'2026-10-26') FROM driver_escrows e WHERE e.id=$1`, escrow, expense).Scan(&held, &paid, &paidEscrow, &stopped))
			if held != wantHeld || paid != wantPaid || paidEscrow != wantEscrow || stopped != wantStop {
				t.Fatalf("unexpected balances: held=%s paid=%s escrow=%s stopped=%v", held, paid, paidEscrow, stopped)
			}
		}
		candidates, err := repo.DriverPayCandidates(ctx, week, "Former")
		check(err)
		if len(candidates) != 1 || candidates[0].ID != driver || candidates[0].Active {
			t.Fatal(candidates)
		}
		check(repo.ChangeDriverPayMembership(ctx, week, driver, false))
		if err = repo.ChangeDriverPayMembership(ctx, week, driver, false); !errors.Is(err, ErrDriverPayConflict) {
			t.Fatal("duplicate membership", err)
		}
		check(repo.ChangeDriverPayMembership(ctx, week, driver, true))
		check(repo.ChangeDriverPayMembership(ctx, week, driver, false))
		later, err := repo.GetDriverWeek(ctx, other, driver)
		check(err)
		if len(later.Drivers) != 0 {
			t.Fatal("membership leaked into later week")
		}
		stop := true
		editor, err := repo.DriverPayEscrow(ctx, week, driver)
		check(err)
		draft := PayrollEscrowDraft{EscrowID: escrow, Version: editor.Accounts[0].Version, Return: "1800", Expenses: map[string]string{expense: "300"}, StopCollection: &stop}
		if err = repo.SaveDriverPayEscrow(ctx, week, driver, editor.EditVersion, editor.ExpenseRevision, draft); err == nil {
			t.Fatal("accepted overdraw")
		}
		draft.Return = "1700"
		check(repo.SaveDriverPayEscrow(ctx, week, driver, editor.EditVersion, editor.ExpenseRevision, draft))
		balances("2000.00", "0.00", "0.00", false)
		report := read()
		card := report.Drivers[0]
		if card.NetPayCents() != 150000 || card.Edits.ExpenseDeductions[0].EscrowAmount != "300.00" || card.Edits.ExpenseDeductions[0].Amount != "200.00" {
			t.Fatalf("invalid draft breakdown: payable %d rows %+v", card.NetPayCents(), card.Edits.ExpenseDeductions)
		}
		// A scheduled release can claim funds while the statement is open.
		var release string
		check(pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&release))
		legacy := NewEscrowRepository(pool)
		check(legacy.SaveRelease(ctx, escrow, EscrowReleaseInput{ID: release, WeekStart: ChargeCurrentWeek(), Amount: "100", EscrowVersion: draft.Version}, actor))
		report = read()
		if _, err = repo.Settle(ctx, week, driver, report.Revision, actor, "", false, false); err == nil {
			t.Fatal("finalized a draft after a competing release changed the balance")
		}
		balances("1900.00", "0.00", "0.00", false)
		editor, err = repo.DriverPayEscrow(ctx, week, driver)
		check(err)
		check(legacy.SaveRelease(ctx, escrow, EscrowReleaseInput{ID: release, WeekStart: ChargeCurrentWeek(), Amount: "100", EscrowVersion: editor.Accounts[0].Version, Version: 1, Cancelled: true}, actor))
		editor, err = repo.DriverPayEscrow(ctx, week, driver)
		check(err)
		draft.Version = editor.Accounts[0].Version
		check(repo.SaveDriverPayEscrow(ctx, week, driver, editor.EditVersion, editor.ExpenseRevision, draft))
		report = read()
		command := PayrollSettlementCommand{RequestID: receipt, ExpenseRevision: report.Drivers[0].ExpenseRevision}
		_, err = repo.Settle(ctx, week, driver, report.Revision, actor, "", false, false, command)
		check(err)
		_, err = repo.Settle(ctx, week, driver, report.Revision, actor, "", false, false, command)
		check(err)
		balances("0.00", "500.00", "300.00", true)
		report = read()
		if !report.Drivers[0].Settlement.Finalized || report.Drivers[0].NetPayCents() != 150000 {
			t.Fatal("frozen statement did not match draft")
		}
		// A stopped account cannot collect after the settlement, in a later week.
		if _, err = pool.Exec(ctx, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) VALUES($1,$2,10)`, escrow, other); err == nil {
			t.Fatal("stopped account accepted collection")
		}
		check(repo.ChangeDriverPayMembership(ctx, other, driver, false))
		later, err = repo.GetDriverWeek(ctx, other, driver)
		check(err)
		if len(later.Drivers[0].Edits.ExpenseDeductions) != 0 {
			t.Fatal("stopped escrow or paid expense appeared again")
		}
		later, err = repo.Settle(ctx, other, driver, later.Revision, actor, "", false, false)
		check(err)
		report = read()
		if _, err = repo.Settle(ctx, week, driver, report.Revision, actor, "", true, false); err == nil {
			t.Fatal("reopened beneath a later finalized statement")
		}
		_, err = repo.Settle(ctx, other, driver, later.Revision, actor, "", true, false)
		check(err)
		report = read()
		_, err = repo.Settle(ctx, week, driver, report.Revision, actor, "", true, false)
		check(err)
		balances("2000.00", "0.00", "0.00", false)
		report = read()
		_, err = repo.Settle(ctx, week, driver, report.Revision, actor, "", false, false)
		check(err)
		balances("0.00", "500.00", "300.00", true)
		// Resuming is a separate audited instruction and takes effect the next week.
		resume := false
		editor, err = repo.DriverPayEscrow(ctx, other, driver)
		check(err)
		check(repo.SaveDriverPayEscrow(ctx, other, driver, editor.EditVersion, editor.ExpenseRevision, PayrollEscrowDraft{
			EscrowID: escrow, Version: editor.Accounts[0].Version, Return: "0", StopCollection: &resume,
		}))
		later, err = repo.GetDriverWeek(ctx, other, driver)
		check(err)
		_, err = repo.Settle(ctx, other, driver, later.Revision, actor, "", false, false)
		check(err)
		var stoppedDuring, stoppedAfter bool
		check(pool.QueryRow(ctx, `SELECT escrow_collection_stopped($1,$2::date),escrow_collection_stopped($1,$2::date+7)`, escrow, other).Scan(&stoppedDuring, &stoppedAfter))
		if !stoppedDuring || stoppedAfter {
			t.Fatal("resume took effect in the wrong week")
		}

	})
}
