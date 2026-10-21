package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testHistoricalPaidCutover(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool, migration string) {
	t.Helper()
	repo := NewMaintenanceRepository(pool)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var truck string
	if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number) VALUES('CUTOVER') RETURNING id`).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().Add(-time.Hour)
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	newInput := func(stage string, linked bool) MaintenanceInput {
		var id string
		if err := admin.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		in := MaintenanceInput{ID: id, Status: "needs_review", WorkStatus: &stage, ServiceDate: &day, Location: "shop", Notes: "Cutover fixture", Services: []MaintenanceWork{{TypeCode: "electrical"}}, Actor: "Original employee"}
		if linked {
			in.TruckID = &truck
		}
		return in
	}
	save := func(in MaintenanceInput) MaintenanceRecord {
		t.Helper()
		m, err := repo.Save(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	legacy := func(in MaintenanceInput) MaintenanceRecord {
		t.Helper()
		m := save(in)
		exec(`UPDATE maintenance_records SET work_status='',created_at=$2 WHERE id=$1`, m.ID, cutoff.Add(-time.Hour))
		return m
	}
	oilInput := newInput("needs_review", false)
	oilInput.SourceUnitNumber = "047"
	oilInput.Services = []MaintenanceWork{{TypeCode: "preventive"}}
	oil := legacy(oilInput)
	category, err := repo.CategoryID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	invoiceInput := newInput("completed", true)
	invoiceInput.Expense = &ExpenseInput{CategoryID: category, ExpenseDate: day, Amount: "100.10", BaseAmount: payTestString("100.10"), PaymentType: payTestString("Check"), PaidBy: payTestString("Original payer"), CoveredBy: payTestString("Company"), CostBreakdown: &[]ExpenseCostItem{}}
	invoice := legacy(invoiceInput)
	unknownCost := legacy(newInput("completed", true))
	futureInput := newInput("completed", true)
	future := save(futureInput)
	exec(`UPDATE maintenance_records SET work_status='' WHERE id=$1`, future.ID)
	ongoing := save(newInput("open", true))
	pending := save(newInput("completed", true))
	// A timestamp alone must never close an explicit new workflow order.
	exec(`UPDATE maintenance_records SET created_at=$2 WHERE id=ANY($1::uuid[])`, []string{ongoing.ID, pending.ID}, cutoff.Add(-time.Hour))
	heldInput := newInput("open", true)
	heldInput.DispatchStatus = payTestString("shop")
	held := legacy(heldInput)
	voided := legacy(newInput("needs_review", true))
	if err := repo.Void(ctx, voided.ID, voided.Version, "Duplicate historical record", "Tester"); err != nil {
		t.Fatal(err)
	}

	snapshot := func() string {
		t.Helper()
		var value string
		if err := admin.QueryRow(ctx, `SELECT jsonb_build_object('expenses',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM expenses e),'recovery',(SELECT jsonb_agg(to_jsonb(p) ORDER BY expense_id,week_start) FROM expense_payments p))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	financialBefore := snapshot()
	// Recreate the actual pre-135 schema and frozen workflow activation time.
	exec(`DROP TRIGGER maintenance_historical_paid_guard ON maintenance_records; DROP FUNCTION maintenance_historical_paid_guard(); ALTER TABLE maintenance_records DROP COLUMN historical_paid`)
	exec(`INSERT INTO schema_migrations(version,applied_at) VALUES('132_fleet_workflow.sql',$1) ON CONFLICT(version) DO UPDATE SET applied_at=excluded.applied_at`, cutoff)
	exec(migration)
	if snapshot() != financialBefore {
		t.Fatal("historical classification changed invoice or payroll recovery data")
	}
	for _, id := range []string{oil.ID, invoice.ID, unknownCost.ID} {
		m, err := repo.Get(ctx, id)
		if err != nil || !m.HistoricalPaid || m.WorkStatus != "closed" || m.NoCharge || len(m.PaymentConfirmation) > 0 {
			t.Fatalf("historical paid record: %+v %v", m, err)
		}
		if m.CreatedByName != "Original employee" {
			t.Fatal("cutover rewrote creator")
		}
		var count int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM maintenance_changes WHERE record_id=$1 AND actor_name='User confirmed historical paid'`, id).Scan(&count); err != nil || count != 1 {
			t.Fatal("cutover audit missing", err)
		}
	}
	review, err := repo.Get(ctx, oil.ID)
	if err != nil || review.Status != "needs_review" || review.TruckID != nil || review.UnitNumber != "047" {
		t.Fatal("historical oil import invented a service identity", review, err)
	}
	for _, id := range []string{future.ID, ongoing.ID, pending.ID, held.ID, voided.ID} {
		m, err := repo.Get(ctx, id)
		if err != nil || m.HistoricalPaid {
			t.Fatal("cutover included new, held or voided work", m, err)
		}
	}
	for status, want := range map[string]int{"active": 2, "completed": 2, "closed": 3, "archived": 1} {
		list, err := repo.List(ctx, MaintenanceQuery{Search: "Cutover fixture", Status: status, PageSize: 100})
		if err != nil || list.Total != want {
			t.Fatalf("%s got %d want %d: %v", status, list.Total, want, err)
		}
	}
	report, err := repo.Reports(ctx, MaintenanceQuery{Search: "Cutover fixture", Status: "closed"})
	if err != nil || report.Total != "100.10" {
		t.Fatal("paid reporting omitted historical invoices", report, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE maintenance_records SET historical_paid=false WHERE id=$1`, oil.ID); err == nil {
		t.Fatal("historical marker was editable")
	}
	oilInput.Version = review.Version
	oilInput.Notes = "Cutover fixture corrected source note"
	corrected := save(oilInput)
	if !corrected.HistoricalPaid || corrected.WorkStatus != "closed" || corrected.Status != "needs_review" || corrected.TruckID != nil {
		t.Fatal("historical correction changed classification", corrected)
	}
	oilInput.Version = corrected.Version
	oilInput.WorkStatus = payTestString("open")
	if _, err := repo.Save(ctx, oilInput); err == nil {
		t.Fatal("historical paid record reopened as ongoing")
	}
	futureInput.Version = future.Version
	futureInput.WorkStatus = payTestString("closed")
	if _, err := repo.Save(ctx, futureInput); err == nil {
		t.Fatal("new unpaid work closed without confirmation")
	}
	if _, err := pool.Exec(ctx, `UPDATE maintenance_records SET dispatch_status='shop' WHERE id=$1`, oil.ID); err == nil {
		t.Fatal("historical record blocked dispatch")
	}
	// Avoid introducing reminders into the other fixture assertions.
	exec(`UPDATE trucks SET active=false WHERE id=$1`, truck)
}
