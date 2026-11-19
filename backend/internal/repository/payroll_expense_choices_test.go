package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollExpenseChoiceAmounts(t *testing.T) {
	base := []ExpenseDeduction{{ExpenseID: "invoice", Available: "100.00", Amount: "100.00", Version: 2}}
	for _, mode := range []string{"auto", "fixed", "skip"} {
		t.Run(mode, func(t *testing.T) {
			input := append([]ExpenseDeduction{}, base...)
			input[0].Mode, input[0].Amount, input[0].Apply = mode, "50.00", true
			choices, err := mergePayrollExpenseChoices(nil, base, input)
			if err != nil {
				t.Fatal(err)
			}
			if err = applyPayrollExpenseChoices(input, choices); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"auto": "100.00", "fixed": "50.00", "skip": "0.00"}[mode]
			if input[0].Amount != want || input[0].Saved || base[0].Amount != "100.00" {
				t.Fatal("choice changed posted balance or source", input, base)
			}
		})
	}
}

func TestPayrollExpenseDraftLifecycleDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor, expense string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Draft expense test',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		// An invoice newer than the selected historical statement is eligible.
		if err := pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,covered_by,payment_type)
 SELECT id,name,'Unpaid expense','2026-11-20',100,$1,$1,'Driver','' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id::text`, projectionDriver).Scan(&expense); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		week, _ := chargeWeek("2026-10-05")
		read := func() DriverPayWeek {
			t.Helper()
			report, err := repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			return report
		}
		card := func(report DriverPayWeek) DriverPayDriver {
			t.Helper()
			for _, d := range report.Drivers {
				if d.ID == projectionDriver {
					return d
				}
			}
			t.Fatalf("missing driver: %+v", report)
			return DriverPayDriver{}
		}
		balance := func(want string) {
			t.Helper()
			var remaining string
			if err := pool.QueryRow(ctx, `SELECT (expense_collectible_total(e)-expense_external_total(e.id)-coalesce((SELECT sum(amount) FROM expense_payments WHERE expense_id=e.id),0))::numeric(14,2)::text FROM expenses e WHERE id=$1`, expense).Scan(&remaining); err != nil || remaining != want {
				t.Fatal("central balance", remaining, want, err)
			}
		}
		d := card(read())
		if len(d.Edits.ExpenseDeductions) != 1 || d.Edits.ExpenseDeductions[0].Amount != "100.00" {
			t.Fatal("all unpaid invoices must be shown", d.Edits.ExpenseDeductions)
		}
		d.Edits.ExpenseDeductions[0].Amount = "50.00"
		d.Edits.ExpenseDeductions[0].Mode = "fixed"
		d.Edits.ExpenseDeductions[0].Apply = true
		if _, err := repo.Save(ctx, d.Edits, actor); err != nil {
			t.Fatal("save draft", err)
		}
		balance("100.00")
		otherWeek, _ := chargeWeek("2026-10-12")
		other, err := repo.Get(ctx, otherWeek)
		if err != nil || card(other).Edits.ExpenseDeductions[0].Amount != "100.00" {
			t.Fatal("a draft reserved another statement's balance", err)
		}
		report := read()
		if card(report).Edits.ExpenseDeductions[0].Amount != "50.00" {
			t.Fatal("fixed choice was lost")
		}
		if _, err = repo.Settle(ctx, week, projectionDriver, report.Revision, actor, "", false, true); err != nil {
			t.Fatal("finalize", err)
		}
		balance("50.00")
		report = read()
		if card(report).Edits.ExpenseDeductions[0].Amount != "50.00" {
			t.Fatal("finalized amount changed")
		}
		if _, err = repo.Settle(ctx, week, projectionDriver, report.Revision, actor, "", true, false); err != nil {
			t.Fatal("reopen", err)
		}
		balance("100.00")
		report = read()
		if card(report).Edits.ExpenseDeductions[0].Amount != "50.00" {
			t.Fatal("reopen lost the draft choice")
		}
		if _, err = repo.Settle(ctx, week, projectionDriver, report.Revision, actor, "", false, true); err != nil {
			t.Fatal("refinalize", err)
		}
		balance("50.00")
	})
}

func TestPayrollLegacyExpenseReopenDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor, expense string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Legacy expense reopening',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,covered_by)
 SELECT id,name,'Legacy partial','2026-10-05',100,$1,$1,'Driver' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id::text`, projectionDriver).Scan(&expense); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-05',40)`, expense); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		week, _ := chargeWeek("2026-10-05")
		for _, reopen := range []bool{false, true, false} {
			report, err := repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.Settle(ctx, week, projectionDriver, report.Revision, actor, "Legacy expense review", reopen, true); err != nil {
				t.Fatal("settle", reopen, err)
			}
			if reopen {
				var mode, amount string
				if err = pool.QueryRow(ctx, `SELECT expense_choices->$2->>'mode',expense_choices->$2->>'amount' FROM driver_pay_weeks WHERE driver_id=$1 AND week_start='2026-10-05'`, projectionDriver, expense).Scan(&mode, &amount); err != nil || mode != "fixed" || amount != "40.00" {
					t.Fatal("legacy choice was not retained", mode, amount, err)
				}
			}
		}
		var paid string
		if err := pool.QueryRow(ctx, `SELECT sum(amount)::text FROM expense_payments WHERE expense_id=$1`, expense).Scan(&paid); err != nil || paid != "40.00" {
			t.Fatal("legacy partial was collected twice or changed", paid, err)
		}
	})
}

func TestInvestorFrozenLateExpenseDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Frozen expense test',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		week, _ := chargeWeek("2026-10-05")
		page, err := repo.InvestorPay(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		final, err := repo.SettleInvestor(ctx, week, projectionTruck, page.Revision, actor, "", false, true)
		if err != nil {
			t.Fatal(err)
		}
		net := final.Drivers[0].NetPayCents()
		var expense string
		if err = pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_type,truck_id,driver_id,owner_id,covered_by,expense_date,amount)
 SELECT id,name,'Late repair',$1,$2,'18200000-0000-0000-0000-000000000003','Truck Owner','2026-10-20',102.67 FROM expense_settings WHERE kind='category' AND name='Maintenance' RETURNING id`, projectionTruck, projectionDriver).Scan(&expense); err != nil {
			t.Fatal(err)
		}
		repo.ConfigurePreparedReads(true)
		repo.ConfigurePreparedFinalization(true)
		repo.resourceConcurrency = true
		if err = repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		if err = repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		prepared, err := repo.PreparedPayroll(ctx, "investor", week, PayPageQuery{ID: projectionTruck}, false)
		if err != nil || len(prepared.Drivers) != 1 {
			t.Fatal("prepare frozen statement", err)
		}
		card := prepared.Drivers[0]
		if card.NetPayCents() != net || len(card.Edits.ExpenseDeductions) != 0 {
			t.Fatalf("frozen payable changed from %d to %d with expenses %+v", net, card.NetPayCents(), card.Edits.ExpenseDeductions)
		}
		open, err := repo.SettleInvestor(WithPayrollReceipt(ctx), week, projectionTruck, prepared.Revision, actor, "", true, false, PayrollSettlementCommand{RequestID: "18200000-0000-0000-0000-000000008191", ExpectedStatementRevision: card.StatementRevision})
		if err != nil {
			t.Fatal("reopen after late expense", err)
		}
		rows := open.Drivers[0].Edits.ExpenseDeductions
		if len(rows) != 1 || rows[0].ExpenseID != expense || rows[0].Saved || rows[0].Amount != "102.67" {
			t.Fatalf("reopened draft expenses: %+v", rows)
		}
		next := week.AddDate(0, 0, 7)
		if err = repo.RequestPayrollPreparation(ctx, next, false); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		nextPage, err := repo.PreparedPayroll(ctx, "investor", next, PayPageQuery{ID: projectionTruck}, false)
		if err != nil || len(nextPage.Drivers) != 1 || len(nextPage.Drivers[0].Edits.ExpenseDeductions) != 1 {
			t.Fatal("unpaid expense missing from second open week", err)
		}
		finalize := func(page DriverPayWeek, requestID string) error {
			t.Helper()
			w, err := chargeWeek(page.WeekStart)
			if err != nil {
				t.Fatal(err)
			}
			_, err = repo.SettleInvestor(WithPayrollReceipt(ctx), w, projectionTruck, page.Revision, actor, "", false, true, PayrollSettlementCommand{RequestID: requestID, ExpectedStatementRevision: page.Drivers[0].StatementRevision})
			return err
		}
		if err = finalize(open, "18200000-0000-0000-0000-000000008192"); err != nil {
			t.Fatal("collect expense in first week", err)
		}
		if err = finalize(nextPage, "18200000-0000-0000-0000-000000008193"); !errors.Is(err, ErrDriverPayConflict) {
			t.Fatal("stale second-week finalization should conflict", err)
		}
		nextPage, err = repo.PreparedPayroll(ctx, "investor", next, PayPageQuery{ID: projectionTruck}, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range nextPage.Drivers {
			for _, row := range d.Edits.ExpenseDeductions {
				if row.ExpenseID == expense {
					t.Fatal("collected expense is still deductible in second week")
				}
			}
		}
		var payments int
		var paid string
		if err = pool.QueryRow(ctx, `SELECT count(*),sum(amount)::text FROM expense_payments WHERE expense_id=$1`, expense).Scan(&payments, &paid); err != nil || payments != 1 || paid != "102.67" {
			t.Fatal("expected one full collection", payments, paid, err)
		}
	})
}
