package repository

import (
	"context"
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
