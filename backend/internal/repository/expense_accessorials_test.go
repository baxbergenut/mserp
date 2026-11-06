package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAccessorialPayrollDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		id := func(sql string, args ...any) string {
			t.Helper()
			var s string
			if err := pool.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
				t.Fatal(err)
			}
			return s
		}
		actor := id(`INSERT INTO app_users(username,password_hash) VALUES('Accessorial accountant',crypt('test-only',gen_salt('bf'))) RETURNING id`)
		owner := id(`SELECT owner_id FROM trucks WHERE id=$1`, projectionTruck)
		accessorial := id(`SELECT id FROM expense_settings WHERE accessorials`)
		other := id(`SELECT id FROM expense_settings WHERE kind='category' AND name='Other'`)
		expenses := NewExpenseRepository(pool)
		expenseDate, _ := time.Parse(time.DateOnly, "2026-11-20")
		pastDate, _ := time.Parse(time.DateOnly, "2026-10-05")
		input := ExpenseInput{CategoryID: accessorial, ExpenseDate: expenseDate, DriverID: payTestString(projectionDriver), Amount: "150.00", PaymentType: payTestString("Driver payroll"), ExpenseType: payTestString("DOT inspection bonus"), CoveredBy: payTestString("Truck Owner"), TruckID: payTestString(projectionTruck), OwnerID: &owner, CreatedBy: actor, AccessorialRequestID: "20300000-0000-0000-0000-000000000001"}
		e, err := expenses.CreateExpense(ctx, input)
		if err != nil {
			t.Fatal("create accessorial", err)
		}
		replay, err := expenses.CreateExpense(ctx, input)
		if err != nil || replay.ID != e.ID {
			t.Fatal("accessorial retry duplicated the entry", replay.ID, err)
		}
		changed := input
		changed.Amount = "151.00"
		if _, err = expenses.CreateExpense(ctx, changed); !errors.Is(err, ErrExpenseSettingConflict) {
			t.Fatal("changed retry accepted", err)
		}
		input.AccessorialRequestID = ""
		input.CategoryID, input.ExpenseDate, input.Amount, input.PaymentType, input.ExpenseType = other, pastDate, "75.00", payTestString("Reimburse"), payTestString("Parking repayment")
		reimb, err := expenses.CreateExpense(ctx, input)
		if err != nil || !reimb.OwnerRecovery {
			t.Fatal("new owner reimbursement not enabled", reimb, err)
		}
		input.CategoryID, input.Amount, input.PaymentType, input.ExpenseType, input.CoveredBy, input.OwnerID = accessorial, "25.00", payTestString("Driver payroll"), payTestString("Company layover"), payTestString("Company"), nil
		company, err := expenses.CreateExpense(ctx, input)
		if err != nil {
			t.Fatal("company accessorial", err)
		}

		pay := NewDriverPayRepository(pool)
		week, _ := chargeWeek("2026-10-05")
		read := func(kind string, w time.Time) DriverPayWeek {
			t.Helper()
			var report DriverPayWeek
			var err error
			if kind == "driver" {
				report, err = pay.GetDriverWeek(ctx, w, projectionDriver)
			} else {
				report, err = pay.InvestorPay(ctx, w)
			}
			if err != nil {
				t.Fatal(err)
			}
			return report
		}
		card := func(r DriverPayWeek, want string) DriverPayDriver {
			t.Helper()
			for _, c := range r.Drivers {
				if c.ID == want {
					return c
				}
			}
			t.Fatalf("missing statement %s", want)
			return DriverPayDriver{}
		}
		row := func(c DriverPayDriver, id, source string, want string) ExpenseDeduction {
			t.Helper()
			for _, v := range c.Edits.ExpenseDeductions {
				if v.ExpenseID == id && v.Source == source {
					if v.Amount != want {
						t.Fatalf("amount for %s/%s got %s want %s", id, source, v.Amount, want)
					}
					return v
				}
			}
			t.Fatalf("missing %s/%s in %+v", id, source, c.Edits.ExpenseDeductions)
			return ExpenseDeduction{}
		}
		balances := func(id, payout, charge string) {
			t.Helper()
			v, err := expenses.GetExpense(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if v.DriverPayoutRemaining == nil || *v.DriverPayoutRemaining != payout || (charge != "" && (v.OwnerCollectionRemaining == nil || *v.OwnerCollectionRemaining != charge)) || (charge == "" && v.OwnerCollectionRemaining != nil) {
				t.Fatalf("unexpected balances for %s: %+v", id, v)
			}
		}
		d := card(read("driver", week), projectionDriver)
		credit := row(d, e.ID, "accessorial", "150.00")
		row(d, reimb.ID, "reimburse", "75.00")
		companyCredit := row(d, company.ID, "accessorial", "25.00")
		inv := card(read("investor", week), projectionTruck)
		row(inv, e.ID, "", "150.00")
		row(inv, reimb.ID, "", "75.00")
		credit.Amount, credit.Mode, credit.Apply = "60.00", "fixed", true
		companyCredit.Amount, companyCredit.Mode, companyCredit.Apply = "0.00", "skip", true
		d.Edits.ExpenseDeductions = []ExpenseDeduction{credit, companyCredit}
		if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
			t.Fatal("draft", err)
		}
		balances(e.ID, "150.00", "150.00")
		balances(company.ID, "25.00", "")
		later := week.AddDate(0, 0, 7)
		row(card(read("driver", later), projectionDriver), e.ID, "accessorial", "150.00")
		report := read("driver", week)
		row(card(report, projectionDriver), e.ID, "accessorial", "60.00")
		if _, err = pay.Settle(ctx, week, projectionDriver, report.Revision, actor, "", false, true); err != nil {
			t.Fatal("driver finalize", err)
		}
		balances(e.ID, "90.00", "150.00")
		balances(reimb.ID, "0.00", "75.00")
		balances(company.ID, "25.00", "")
		if err = expenses.DeleteExpense(ctx, e.ID); !errors.Is(err, ErrExpenseFinalized) {
			t.Fatal("frozen expense was not protected", err)
		}
		report = read("investor", later)
		inv = card(report, projectionTruck)
		row(inv, e.ID, "", "150.00")
		row(inv, reimb.ID, "", "75.00")
		if _, err = pay.SettleInvestor(ctx, later, projectionTruck, report.Revision, actor, "", false, true); err != nil {
			t.Fatal("owner finalize", err)
		}
		balances(e.ID, "90.00", "0.00")
		balances(reimb.ID, "0.00", "0.00")
		report = read("driver", week)
		if _, err = pay.Settle(ctx, week, projectionDriver, report.Revision, actor, "", true, false); err != nil {
			t.Fatal("driver reopen", err)
		}
		balances(e.ID, "150.00", "0.00")
		row(card(read("driver", week), projectionDriver), e.ID, "accessorial", "60.00")
		if err = pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal("source registration", err)
		}
		pay.resourceConcurrency = true
		pay.ConfigurePreparedFinalization(true)
		if _, err = pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		if err = pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err = pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		prepared, err := pay.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil {
			t.Fatal(err)
		}
		p := card(prepared, projectionDriver)
		row(p, e.ID, "accessorial", "60.00")
		command := PayrollSettlementCommand{RequestID: "20300000-0000-0000-0000-000000000002", ExpectedStatementRevision: p.StatementRevision, ExpenseRevision: p.ExpenseRevision}
		for i := 0; i < 2; i++ {
			if _, err = pay.Settle(WithPayrollReceipt(ctx), week, projectionDriver, prepared.Revision, actor, "", false, true, command); err != nil {
				t.Fatal("prepared refinalize/retry", err)
			}
		}
		balances(e.ID, "90.00", "0.00")
	})
}

func TestAccessorialOwnerOperatorDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `UPDATE investors SET driver_id=$1 WHERE id=(SELECT owner_id FROM trucks WHERE id=$2)`, projectionDriver, projectionTruck); err != nil {
			t.Fatal(err)
		}
		var actor, expense string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Owner accessorial test',crypt('test-only',gen_salt('bf'))) RETURNING id`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_date,expense_type,amount,payment_type,driver_id,truck_id,owner_id,covered_by)
 SELECT s.id,s.name,'2026-11-20','Owner detention',100,'Driver payroll',$1,t.id,t.owner_id,'Truck Owner'
 FROM expense_settings s CROSS JOIN trucks t WHERE s.accessorials AND t.id=$2 RETURNING expenses.id`, projectionDriver, projectionTruck).Scan(&expense); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		week, _ := chargeWeek("2026-10-05")
		read := func() DriverPayWeek {
			t.Helper()
			report, err := pay.GetDriverWeek(ctx, week, projectionDriver)
			if err != nil || len(report.Drivers) != 1 {
				t.Fatal("driver report", err)
			}
			return report
		}
		report := read()
		edit := report.Drivers[0].Edits
		count := 0
		for i := range edit.ExpenseDeductions {
			r := &edit.ExpenseDeductions[i]
			if r.ExpenseID != expense {
				continue
			}
			count++
			r.Apply, r.Mode = true, "fixed"
			if r.Source == "accessorial" {
				r.Amount = "60.00"
			} else {
				r.Amount = "15.00"
			}
		}
		if count != 2 {
			t.Fatal("paired expense effects missing", edit.ExpenseDeductions)
		}
		if _, err := pay.Save(ctx, edit, actor); err != nil {
			t.Fatal(err)
		}
		report = read()
		if _, err := pay.Settle(ctx, week, projectionDriver, report.Revision, actor, "", false, true); err != nil {
			t.Fatal(err)
		}
		balances := func(payout, debt string) {
			t.Helper()
			v, err := NewExpenseRepository(pool).GetExpense(ctx, expense)
			if err != nil || v.DriverPayoutRemaining == nil || *v.DriverPayoutRemaining != payout || v.OwnerCollectionRemaining == nil || *v.OwnerCollectionRemaining != debt {
				t.Fatal("paired balances", v, err)
			}
		}
		balances("40.00", "85.00")
		if _, err := pay.Settle(ctx, week, projectionDriver, read().Revision, actor, "", true, false); err != nil {
			t.Fatal(err)
		}
		balances("100.00", "100.00")
	})
}
