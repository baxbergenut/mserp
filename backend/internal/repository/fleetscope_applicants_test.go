package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
	"os"
	"strings"
	"testing"
	"time"
)

func testFleetScopeApplicants(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	fleet, expenses := NewFleetRepository(pool), NewExpenseRepository(pool)
	const company = "00000000-0000-4000-8000-00000000a001"
	const source = "00000000-0000-4000-8000-00000000a002"
	event := fleetscope.Event{Version: 1, EventID: "00000000-0000-4000-8000-00000000a003", Type: "applicant.updated", CompanyID: company, OccurredAt: time.Now().UTC().Truncate(time.Microsecond), Applicant: &fleetscope.Applicant{ID: source, FullName: "Applicant Expense Test", Status: "applied"}}
	result, err := fleet.AcceptFleetScopeApplicant(ctx, event, "applicant-original")
	if err != nil || result.Status != "accepted" || result.ApplicantID == "" {
		t.Fatalf("accept: %+v %v", result, err)
	}
	applicant := result.ApplicantID
	retry, err := fleet.AcceptFleetScopeApplicant(ctx, event, "applicant-original")
	if err != nil || retry.Status != "duplicate" || retry.ApplicantID != applicant {
		t.Fatal("retry", retry, err)
	}
	if _, err = fleet.AcceptFleetScopeApplicant(ctx, event, "different"); !errors.Is(err, ErrFleetScopeEventConflict) {
		t.Fatal("changed receipt accepted", err)
	}
	stale := event
	stalePerson := *event.Applicant
	stale.Applicant = &stalePerson
	stale.EventID = "00000000-0000-4000-8000-00000000a004"
	stale.OccurredAt = stale.OccurredAt.Add(-time.Hour)
	stale.Applicant.FullName = "Stale Applicant"
	if got, err := fleet.AcceptFleetScopeApplicant(ctx, stale, "stale"); err != nil || got.Status != "duplicate" {
		t.Fatal("stale", got, err)
	}
	var n int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM drivers WHERE full_name=$1`, event.Applicant.FullName).Scan(&n); err != nil || n != 0 {
		t.Fatal("applicant entered fleet", n, err)
	}
	directory, err := fleet.ListDriverDirectory(ctx, Pagination{}, event.Applicant.FullName, true)
	if err != nil || directory.Total != 0 {
		t.Fatal("applicant visible in directory", directory, err)
	}
	options, err := expenses.ListExpenseApplicants(ctx)
	found := false
	for _, a := range options {
		if a.ID == applicant && a.FullName == event.Applicant.FullName {
			found = true
		}
	}
	if err != nil || !found {
		t.Fatal("missing expense option", options, err)
	}
	_, driver, matched, err := expenses.ResolveExpenseDraftLinks(ctx, nil, &event.Applicant.FullName)
	if err != nil || driver != nil || matched == nil || *matched != applicant {
		t.Fatal("AI applicant match", driver, matched, err)
	}
	var category string
	if err = pool.QueryRow(ctx, `SELECT id FROM expense_settings WHERE kind='category' AND name='HR'`).Scan(&category); err != nil {
		t.Fatal(err)
	}
	name, kind, covered := "Spoofed supplied name", "Transportation", "Company"
	input := ExpenseInput{ApplicantID: &applicant, CategoryID: category, ExpenseDate: time.Now(), DriverName: &name, Amount: "85.00", ExpenseType: &kind, CoveredBy: &covered}
	expense, err := expenses.CreateExpense(ctx, input)
	if err != nil || expense.DriverID != nil || expense.TruckID != nil || expense.DriverName == nil || *expense.DriverName != event.Applicant.FullName {
		t.Fatal("applicant expense", expense, err)
	}
	// A same-name operational driver makes AI resolution ambiguous; no name-based merge.
	var decoy string
	if err = pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,'different candidate key','cpm',.7) RETURNING id`, event.Applicant.FullName).Scan(&decoy); err != nil {
		t.Fatal(err)
	}
	_, driver, matched, err = expenses.ResolveExpenseDraftLinks(ctx, nil, &event.Applicant.FullName)
	if err != nil || driver != nil || matched != nil {
		t.Fatal("ambiguous person guessed", driver, matched, err)
	}
	bad := input
	bad.DriverID = &decoy
	if _, err = expenses.CreateExpense(ctx, bad); err == nil {
		t.Fatal("mismatched applicant driver accepted")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM drivers WHERE id=$1`, decoy); err != nil {
		t.Fatal(err)
	}
	// FleetScope creates a different UUID at hire; explicit mapping preserves identity.
	event.Applicant.Status = "hired"
	event.Applicant.HiredDriverID = "00000000-0000-4000-8000-00000000b002"
	event.OccurredAt = event.OccurredAt.Add(time.Minute)
	event.EventID = "00000000-0000-4000-8000-00000000a007"
	if _, err = fleet.AcceptFleetScopeApplicant(ctx, event, "hired-map"); err != nil {
		t.Fatal(err)
	}
	// Hire identity is stable even if the final driver's reviewed name changes.
	hire := event
	hire.EventID = "00000000-0000-4000-8000-00000000a005"
	hire.Type = "driver.hired"
	hire.Applicant = nil
	hire.Driver = fleetscope.Driver{ID: "00000000-0000-4000-8000-00000000b002", FullName: event.Applicant.FullName}
	hire.Driver.DriverType = "company"
	hire.Driver.HireDate = "2026-10-19"
	accepted, err := fleet.AcceptFleetScopeHire(ctx, hire, "hire-original")
	if err != nil {
		t.Fatal(err)
	}
	hireDate, _ := time.Parse(time.DateOnly, hire.Driver.HireDate)
	hired, err := fleet.CompleteDriverIntake(ctx, accepted.IntakeID, "00000000-0000-0000-0000-000000000001", "", &DriverInput{FullName: "Reviewed Applicant Name", PayType: "cpm", PayRate: .75, Active: true, HireDate: &hireDate}, true)
	if err != nil {
		t.Fatal("hire completion", err)
	}
	history, err := expenses.ListExpensesPage(ctx, ExpensePageQuery{DriverID: &hired.ID, Responsibility: "non_personal", VisibleCategoryIDs: []string{category}, AccessibleCategoryIDs: []string{category}})
	if err != nil || history.Total != 1 || history.Items[0].ID != expense.ID || history.Items[0].DriverID == nil || *history.Items[0].DriverID != hired.ID || history.Items[0].ChargeDriverID != nil || *history.Items[0].CoveredBy != "Company" || !history.Items[0].UpdatedAt.Equal(expense.UpdatedAt) {
		t.Fatalf("prehire history without financial rewrite: %+v %v", history, err)
	}
	// A draft saved after setup still resolves to the hire, without a timing gap.
	late, err := expenses.CreateExpense(ctx, input)
	if err != nil || late.DriverID == nil || *late.DriverID != hired.ID {
		t.Fatal("late draft", late, err)
	}
	options, err = expenses.ListExpenseApplicants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range options {
		if a.ID == applicant {
			t.Fatal("hired person duplicated in applicant options")
		}
	}
	// Late applicant updates cannot undo the reviewed link.
	event.EventID = "00000000-0000-4000-8000-00000000a006"
	event.OccurredAt = event.OccurredAt.Add(time.Minute)
	event.Applicant.Status = "withdrawn"
	event.Applicant.HiredDriverID = ""
	if _, err = fleet.AcceptFleetScopeApplicant(ctx, event, "late-update"); !errors.Is(err, ErrFleetScopeEventConflict) {
		t.Fatal(err)
	}
	saved, err := expenses.GetExpense(ctx, expense.ID)
	if err != nil || saved.DriverID == nil || *saved.DriverID != hired.ID {
		t.Fatal("source status removed history", saved, err)
	}
}

// Expense-only legacy fixtures omit outbound fleet capture. Use its exact identity
// table DDL, then the real applicant migration; the FleetScope suite tests full DDL.
func applyExpenseApplicantTestSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('fleetscope_driver_identity') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		source, err := os.ReadFile("../../sql/103_fleetscope_fleet_sync.sql")
		if err != nil {
			t.Fatal(err)
		}
		_, tail, ok := strings.Cut(string(source), "CREATE TABLE fleetscope_driver_identity (")
		if !ok {
			t.Fatal("missing identity DDL")
		}
		body, _, ok := strings.Cut(tail, ";")
		if !ok {
			t.Fatal("missing identity DDL terminator")
		}
		if _, err = conn.Exec(ctx, "CREATE TABLE fleetscope_driver_identity ("+body+"; GRANT SELECT ON fleetscope_driver_identity TO mserp_app"); err != nil {
			t.Fatal(err)
		}
	}
	body, err := os.ReadFile("../../sql/134_fleetscope_applicants.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
}
