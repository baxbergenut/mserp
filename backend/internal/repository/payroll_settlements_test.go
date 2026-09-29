package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testOwnerSettlements(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool, operator, ownerDriver string) {
	t.Helper()
	var owner, independent, actor string
	for _, q := range []struct {
		sql  string
		dest *string
		args []any
	}{
		{`INSERT INTO investors(full_name,driver_id) VALUES('Driver Owner',$1) RETURNING id`, &owner, []any{ownerDriver}},
		{`INSERT INTO investors(full_name) VALUES('Independent Owner') RETURNING id`, &independent, nil},
		{`INSERT INTO app_users(username,password_hash) VALUES('settler',crypt('test-only',gen_salt('bf'))) RETURNING id`, &actor, nil},
	} {
		if err := admin.QueryRow(ctx, q.sql, q.args...).Scan(q.dest); err != nil {
			t.Fatal(err)
		}
	}
	week, _ := time.Parse(time.DateOnly, ChargeCurrentWeek())
	pay := NewDriverPayRepository(pool)
	expenses := NewExpenseRepository(pool)
	input := ExpenseInput{Company: "MS Express", Category: "Penalties", ExpenseDate: week, DriverID: &operator, OwnerID: &owner, Amount: "50.25", CoveredBy: payTestString("Truck Owner")}
	expense, err := expenses.CreateExpense(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if expense.ChargeDriverID == nil || *expense.ChargeDriverID != ownerDriver {
		t.Fatal("operating driver billed instead of owner", expense)
	}
	input.OwnerID = &independent
	external, err := expenses.CreateExpense(ctx, input)
	if err != nil || external.ChargeDriverID != nil {
		t.Fatal("independent investor charged to operator", err)
	}
	personal, err := expenses.ListExpensesPage(ctx, ExpensePageQuery{ChargeDriverID: &ownerDriver, Pagination: Pagination{PageSize: 100}})
	if err != nil || personal.Total != 1 {
		t.Fatal("personal responsibility filter", personal.Total, err)
	}
	linked, err := expenses.ListExpensesPage(ctx, ExpensePageQuery{DriverID: &operator, Responsibility: "non_personal", Pagination: Pagination{PageSize: 100}})
	if err != nil || linked.Total < 2 {
		t.Fatal("company/other responsibility filter", linked.Total, err)
	}
	chargeRepo := NewDriverChargeRepository(pool)
	ids, err := chargeRepo.Create(ctx, ChargeCreate{DriverIDs: []string{ownerDriver}, Kind: "installment", Name: "Settlement installment", Amount: "20", Total: "100", StartWeek: week.Format(time.DateOnly), Eligibility: "calendar"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	report, err := pay.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pay.Settle(ctx, week, ownerDriver, "stale", actor, "", false); !errors.Is(err, ErrDriverPayConflict) {
		t.Fatal("stale finalization accepted", err)
	}
	final, err := pay.Settle(ctx, week, ownerDriver, report.Revision, actor, "", false)
	if err != nil {
		t.Fatal(err)
	}
	find := func(report DriverPayWeek, id string) DriverPayDriver {
		for _, d := range report.Drivers {
			if d.ID == id {
				return d
			}
		}
		t.Fatal("driver missing", id)
		return DriverPayDriver{}
	}
	frozen := find(final, ownerDriver)
	if frozen.Settlement == nil || !frozen.Settlement.Finalized || frozen.Edits.GeneratedCharges[0].ConfirmedAt == nil {
		t.Fatal("settlement did not confirm installments", frozen)
	}
	expense, err = expenses.GetExpense(ctx, expense.ID)
	if err != nil || *expense.RemainingAmount != "0.00" {
		t.Fatal("finalization did not apply expense", err)
	}
	if _, err = pay.Save(ctx, frozen.Edits, actor); err == nil {
		t.Fatal("finalized driver editable")
	}
	if err = chargeRepo.Confirm(ctx, ChargeConfirm{DriverID: ownerDriver, WeekStart: week.Format(time.DateOnly), Rows: frozen.Edits.GeneratedCharges, Reason: "bypass"}, actor, true); err == nil {
		t.Fatal("finalized installments reopened separately")
	}
	oldRate := frozen.PayRate
	if _, err = admin.Exec(ctx, `UPDATE drivers SET pay_rate=0.95 WHERE id=$1`, ownerDriver); err != nil {
		t.Fatal(err)
	}
	current, err := pay.Get(ctx, week)
	if err != nil || find(current, ownerDriver).PayRate != oldRate {
		t.Fatal("finalized snapshot changed with profile", err)
	}
	if _, err = pay.Settle(ctx, week, ownerDriver, current.Revision, actor, "", true); err == nil {
		t.Fatal("reopen without reason accepted")
	}
	reopened, err := pay.Settle(ctx, week, ownerDriver, current.Revision, actor, "Correct deduction", true)
	if err != nil {
		t.Fatal(err)
	}
	d := find(reopened, ownerDriver)
	if d.Settlement.Finalized || d.Edits.GeneratedCharges[0].ConfirmedAt != nil {
		t.Fatal("reopening did not reverse finalization's confirmation")
	}
	d.Edits.ExpenseDeductions[0].Amount = "10.25"
	d.Edits.ExpenseDeductions[0].Apply = true
	if _, err = pay.Save(ctx, d.Edits, actor); err != nil {
		t.Fatal(err)
	}
	expense, err = expenses.GetExpense(ctx, expense.ID)
	if err != nil || *expense.RemainingAmount != "40.00" {
		t.Fatal("correction did not restore expense balance", err)
	}
	current, err = pay.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	final, err = pay.Settle(ctx, week, "", current.Revision, actor, "", false)
	if err != nil {
		t.Fatal("whole week finalization", err)
	}
	for _, d := range final.Drivers {
		if d.Settlement == nil || !d.Settlement.Finalized {
			t.Fatal("whole-week missed driver")
		}
	}
	events, err := pay.SettlementHistory(ctx, ownerDriver, week.Format(time.DateOnly))
	if err != nil || len(events) != 3 || events[1].Reason != "Correct deduction" {
		t.Fatal("settlement audit history", events, err)
	}
	history, err := pay.History(ctx, ownerDriver, Pagination{PageSize: 12})
	if err != nil || history.Total == 0 || !history.Items[0].Driver.Settlement.Finalized {
		t.Fatal("profile pay history", history, err)
	}
	plan, err := chargeRepo.List(ctx, ownerDriver)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Schedules {
		if s.ID == ids[0] && s.Confirmed != "20.00" {
			t.Fatal("duplicate installment collection", s.Confirmed)
		}
	}
	opened, err := pay.Settle(ctx, week, "", final.Revision, actor, "Recheck the whole week", true)
	if err != nil {
		t.Fatal("whole week reopen", err)
	}
	for _, d := range opened.Drivers {
		if d.Settlement == nil || d.Settlement.Finalized {
			t.Fatal("whole-week reopen missed driver")
		}
	}

}
