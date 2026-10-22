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

func testFleetWorkOrderWorkflow(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	var truck, driver string
	if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('WORKFLOW') RETURNING id`).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Workflow driver','workflow driver','cpm',0.7) RETURNING id`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id) VALUES($1,$2)`, truck, driver); err != nil {
		t.Fatal(err)
	}
	repo := NewMaintenanceRepository(pool)
	uuid := func() string {
		var id string
		if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	status := func(want string) {
		t.Helper()
		var state string
		if err := pool.QueryRow(ctx, `SELECT fleet_truck_operating_status($1)`, truck).Scan(&state); err != nil || state != want {
			t.Fatalf("truck status %s, want %s: %v", state, want, err)
		}
	}
	save := func(in MaintenanceInput) MaintenanceRecord {
		t.Helper()
		m, err := repo.Save(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	newOrder := func(hold string) MaintenanceInput {
		return MaintenanceInput{ID: uuid(), TruckID: &truck, WorkStatus: payTestString("open"), DispatchStatus: &hold, Status: "needs_review", Location: "shop", Notes: "Diagnose reported problem", Services: []MaintenanceWork{{TypeCode: "electrical"}}, Actor: "Fleet employee"}
	}
	first := newOrder("")
	m := save(first)
	if m.ExpenseID != nil || len(m.PaymentConfirmation) > 0 {
		t.Fatal("opening work invented an invoice", m)
	}
	status("ready")
	first.Version = m.Version
	first.WorkStatus = payTestString("in_progress")
	first.DispatchStatus = payTestString("shop")
	m = save(first)
	status("shop")
	if err := repo.Void(ctx, m.ID, m.Version, "cancel", "Fleet"); err == nil {
		t.Fatal("void released an active hold")
	}
	first.Version = m.Version
	first.DispatchStatus = payTestString("")
	if _, err := repo.Save(ctx, first); err == nil {
		t.Fatal("unfinished work released truck")
	}
	first.DispatchStatus = payTestString("shop")
	date := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	first.ServiceDate = &date
	first.WorkStatus = payTestString("completed")
	m = save(first)
	status("shop")
	first.Version = m.Version
	second := newOrder("out_of_service")
	other := save(second)
	status("out_of_service")
	first.DispatchStatus = payTestString("")
	m = save(first)
	status("out_of_service")
	first.Version = m.Version
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireDispatchableTruck(ctx, tx, driver); !errors.Is(err, ErrTruckUnavailable) {
		t.Fatal("dispatch ignored hold", err)
	}
	tx.Rollback(ctx)
	boardRepo := NewDriverBoardRepository(pool)
	if _, err := boardRepo.Save(ctx, []DriverBoardEntry{{DriverID: driver, CurrentLoad: "NEW-BLOCKED"}}); !errors.Is(err, ErrTruckUnavailable) {
		t.Fatal("Status Board ignored hold", err)
	}
	if _, err := admin.Exec(ctx, `GRANT SELECT ON gross_board_entries,gross_board_extra_entries TO mserp_app`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewGrossBoardRepository(pool).SaveEntries(ctx, []GrossBoardEntry{{DriverID: driver, Date: boardLoadToday(time.Now()), LoadNumber: "NEW-BLOCKED", Slot: 0}}); !errors.Is(err, ErrTruckUnavailable) {
		t.Fatal("Gross Board ignored hold", err)
	}
	second.Version = other.Version
	second.WorkStatus = payTestString("canceled")
	second.CancelReason = "Duplicate report"
	other = save(second)
	status("out_of_service")
	second.Version = other.Version
	second.DispatchStatus = payTestString("")
	save(second)
	status("ready")
	first.WorkStatus = payTestString("closed")
	if _, err := repo.Save(ctx, first); err == nil {
		t.Fatal("unknown cost closed as zero")
	}
	first.WorkStatus = payTestString("completed")
	category, err := repo.CategoryID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first.Expense = &ExpenseInput{CategoryID: category, ExpenseDate: date, Amount: "100.10", BaseAmount: payTestString("100.10"), CoveredBy: payTestString("Company"), CostBreakdown: &[]ExpenseCostItem{}}
	m = save(first)
	first.Version = m.Version
	first.Expense.UpdatedAt = &m.Expense.UpdatedAt
	first.Expense.PaymentType = payTestString("Check")
	first.Expense.PaidBy = payTestString("Fleet employee")
	first.WorkStatus = payTestString("closed")
	if _, err := repo.Save(ctx, first); err == nil {
		t.Fatal("invoice fields were inferred as payment confirmation")
	}
	first.ConfirmPayment = true
	first.Actor = "Another fleet employee"
	m = save(first)
	if m.WorkStatus != "closed" || len(m.PaymentConfirmation) == 0 || m.Expense.DriverSettled || m.CreatedByName != "Fleet employee" {
		t.Fatal("payment confirmation or immutable creator incorrect", m)
	}
	first.Version = m.Version - 1
	if _, err := repo.Save(ctx, first); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatal("stale workflow overwrite", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE expenses SET amount=90,base_amount=90 WHERE id=$1`, *m.ExpenseID); err == nil {
		t.Fatal("invoice edit changed confirmed payment")
	}
	// An explicit versioned work-order edit records corrected invoice values while
	// preserving the original paid confirmation and independent collection ledger.
	originalConfirmation := string(m.PaymentConfirmation)
	first.Version = m.Version
	first.Expense.UpdatedAt = &m.Expense.UpdatedAt
	first.Expense.Amount = "175.25"
	first.Expense.BaseAmount = payTestString("175.25")
	first.Expense.PaymentType = payTestString("Cash")
	first.Expense.ReferenceNumber = payTestString("Corrected invoice")
	first.Expense.PaidBy = payTestString("Correcting employee")
	first.Notes = "Corrected repair details"
	first.Actor = "Correcting employee"
	m = save(first)
	if *m.Expense.Amount != "175.25" || *m.Expense.PaymentType != "Cash" || m.WorkStatus != "closed" || m.Notes != first.Notes {
		t.Fatal("paid work-order edits were not saved", m)
	}
	var original, corrected map[string]any
	if err := json.Unmarshal([]byte(originalConfirmation), &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(m.PaymentConfirmation, &corrected); err != nil {
		t.Fatal(err)
	}
	corrections, ok := corrected["invoiceCorrections"].([]any)
	if !ok || len(corrections) != 1 {
		t.Fatal("missing invoice correction audit")
	}
	delete(corrected, "invoiceCorrections")
	originalJSON, _ := json.Marshal(original)
	correctedJSON, _ := json.Marshal(corrected)
	if string(originalJSON) != string(correctedJSON) {
		t.Fatal("original payment confirmation changed")
	}
	if _, err := admin.Exec(ctx, `UPDATE expenses SET payment_type='Check' WHERE id=$1`, *m.ExpenseID); err == nil {
		t.Fatal("unreviewed update bypassed corrected confirmation")
	}
	free := newOrder("")
	free.WorkStatus = payTestString("completed")
	free.ServiceDate = &date
	free.NoCharge = true
	freeRecord := save(free)
	free.Version = freeRecord.Version
	free.WorkStatus = payTestString("closed")
	freeRecord = save(free)
	if freeRecord.ExpenseID != nil || len(freeRecord.PaymentConfirmation) > 0 {
		t.Fatal("no-charge work invented payment", freeRecord)
	}
	voided := save(newOrder(""))
	if err := repo.Void(ctx, voided.ID, voided.Version, "Entered on wrong truck", "Fleet"); err != nil {
		t.Fatal(err)
	}
	for filter, expected := range map[string]int{"active": 0, "completed": 0, "closed": 2, "archived": 2} {
		listing, err := repo.List(ctx, MaintenanceQuery{TruckID: truck, Status: filter, PageSize: 100})
		if err != nil || listing.Total != expected {
			t.Fatalf("stage filter %s: got %d, want %d: %v", filter, listing.Total, expected, err)
		}
	}
	if _, err := admin.Exec(ctx, `UPDATE trucks SET status='maintenance' WHERE id=$1`, truck); err != nil {
		t.Fatal(err)
	}
	status("shop")
	if _, err := admin.Exec(ctx, `UPDATE trucks SET status='available' WHERE id=$1`, truck); err != nil {
		t.Fatal(err)
	}
	status("ready")
	// Keep this fixture out of unrelated reminder/board tests.
	if _, err := admin.Exec(ctx, `UPDATE drivers SET active=false WHERE id=$1;`, driver); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `UPDATE trucks SET active=false WHERE id=$1`, truck); err != nil {
		t.Fatal(err)
	}
}
