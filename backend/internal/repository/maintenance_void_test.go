package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testVoidConfirmedWorkOrders(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	repo := NewMaintenanceRepository(pool)
	category, err := repo.CategoryID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, corrected := range []bool{false, true} {
		name := "original invoice"
		if corrected {
			name = "corrected invoice"
		}
		t.Run("void confirmed "+name, func(t *testing.T) {
			var truck, id string
			if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,active) VALUES($1,false) RETURNING id`, "VOID-"+name).Scan(&truck); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			date := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			odo := 100000
			input := MaintenanceInput{ID: id, TruckID: &truck, ServiceDate: &date, Odometer: &odo,
				WorkStatus: payTestString("completed"), Status: "completed", Location: "shop",
				Services: []MaintenanceWork{{TypeCode: "preventive"}}, Actor: "Fleet employee",
				Expense: &ExpenseInput{CategoryID: category, ExpenseDate: date, Amount: "100.10", BaseAmount: payTestString("100.10"),
					CoveredBy: payTestString("Company"), PaymentType: payTestString("Check"), PaidBy: payTestString("Fleet employee"), CostBreakdown: &[]ExpenseCostItem{}}}
			m, err := repo.Save(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			input.Version = m.Version
			input.Expense.UpdatedAt = &m.Expense.UpdatedAt
			input.WorkStatus = payTestString("closed")
			input.ConfirmPayment = true
			m, err = repo.Save(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if corrected {
				input.Version = m.Version
				input.Expense.UpdatedAt = &m.Expense.UpdatedAt
				input.Expense.Amount = "175.25"
				input.Expense.BaseAmount = payTestString("175.25")
				input.Expense.PaymentType = payTestString("Cash")
				m, err = repo.Save(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
			}
			// Confirmation does not bypass independent accounting verification guards.
			if _, err := admin.Exec(ctx, `UPDATE expenses SET manager_verified=true WHERE id=$1`, *m.ExpenseID); err != nil {
				t.Fatal(err)
			}
			if err := repo.Void(ctx, id, m.Version, "Duplicate work order", "Fleet employee"); !errors.Is(err, ErrMaintenanceInvalid) {
				t.Fatal("void bypassed invoice verification", err)
			}
			if _, err := admin.Exec(ctx, `UPDATE expenses SET manager_verified=false WHERE id=$1`, *m.ExpenseID); err != nil {
				t.Fatal(err)
			}
			var invoiceBefore []byte
			if err := pool.QueryRow(ctx, `SELECT to_jsonb(e) FROM expenses e WHERE id=$1`, *m.ExpenseID).Scan(&invoiceBefore); err != nil {
				t.Fatal(err)
			}
			if err := repo.Void(ctx, id, m.Version-1, "Duplicate work order", "Fleet employee"); !errors.Is(err, ErrMaintenanceConflict) {
				t.Fatal("stale void accepted", err)
			}
			if err := repo.Void(ctx, id, m.Version, "Duplicate work order", "Fleet employee"); err != nil {
				t.Fatal("payment-confirmed duplicate could not be voided", err)
			}
			voided, err := repo.Get(ctx, id)
			if err != nil || voided.Status != "voided" || voided.WorkStatus != "voided" || voided.ExpenseID != nil || voided.VoidReason != "Duplicate work order" || voided.Version != m.Version+1 || string(voided.PaymentConfirmation) != string(m.PaymentConfirmation) {
				t.Fatal("void lost payment evidence or lifecycle state", voided, err)
			}
			var invoiceAfter []byte
			var exists, active bool
			if err := pool.QueryRow(ctx, `SELECT voided_expense,EXISTS(SELECT 1 FROM expenses WHERE id=$2),EXISTS(SELECT 1 FROM maintenance_odometer_readings WHERE record_id=$1 AND active) FROM maintenance_records WHERE id=$1`, id, *m.ExpenseID).Scan(&invoiceAfter, &exists, &active); err != nil || string(invoiceBefore) != string(invoiceAfter) || exists || active {
				t.Fatal("void did not archive the invoice or deactivate service mileage", err)
			}
			// The archived invoice must still match the latest confirmed correction.
			if _, err := admin.Exec(ctx, `UPDATE maintenance_records SET voided_expense=jsonb_set(voided_expense,'{amount}','0') WHERE id=$1`, id); err == nil {
				t.Fatal("void snapshot bypassed confirmed invoice validation")
			}
			if _, err := admin.Exec(ctx, `UPDATE maintenance_records SET voided_expense=NULL WHERE id=$1`, id); err == nil {
				t.Fatal("void discarded confirmed invoice snapshot")
			}
			changes, err := repo.History(ctx, id)
			if err != nil || len(changes) == 0 {
				t.Fatal("void audit missing", err)
			}
			var audit struct {
				ActorName string            `json:"actorName"`
				Before    MaintenanceRecord `json:"before"`
				After     MaintenanceRecord `json:"after"`
			}
			if err := json.Unmarshal(changes[0], &audit); err != nil || audit.ActorName != "Fleet employee" || audit.Before.ExpenseID == nil || string(audit.Before.PaymentConfirmation) != string(m.PaymentConfirmation) || audit.After.Status != "voided" {
				t.Fatal("void audit lost invoice or actor", err)
			}
			listing, err := repo.List(ctx, MaintenanceQuery{TruckID: truck, Status: "closed"})
			if err != nil || listing.Total != 0 {
				t.Fatal("voided duplicate remained in paid orders", err)
			}
			listing, err = repo.List(ctx, MaintenanceQuery{TruckID: truck, Status: "archived"})
			if err != nil || listing.Total != 1 {
				t.Fatal("voided duplicate missing from canceled orders", err)
			}
			if err := repo.Void(ctx, id, voided.Version, "Duplicate work order", "Fleet employee"); !errors.Is(err, ErrMaintenanceConflict) {
				t.Fatal("duplicate void accepted", err)
			}
		})
	}
}
