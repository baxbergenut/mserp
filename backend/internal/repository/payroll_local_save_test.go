package repository

import "testing"

func TestPayrollStatementLocalSave(t *testing.T) {
	before := DriverPayEdits{DriverID: "driver", WeekStart: "2026-10-05", Costs: &DriverPayCosts{Revision: "old"},
		ExpenseDeductions: []ExpenseDeduction{{ExpenseID: "expense", Amount: "100.00", Mode: "auto"}}}
	for _, tc := range []struct {
		name  string
		edit  func(*DriverPayEdits)
		local bool
	}{
		{"notes", func(e *DriverPayEdits) { e.Notes = "Reviewed" }, true},
		{"adjustment", func(e *DriverPayEdits) { e.Adjustments = []DriverPayAdjustment{{Amount: "25.00"}} }, true},
		{"expense choice", func(e *DriverPayEdits) {
			e.ExpenseDeductions = []ExpenseDeduction{{ExpenseID: "expense", Amount: "0.00", Mode: "skip", Apply: true}}
		}, true},
		{"cost snapshot changed", func(e *DriverPayEdits) { e.Costs = &DriverPayCosts{Revision: "new"} }, false},
		{"fuel override", func(e *DriverPayEdits) { amount := "-10.00"; e.FuelOverride = &amount }, false},
		{"escrow application", func(e *DriverPayEdits) {
			e.ExpenseDeductions = []ExpenseDeduction{{Source: "escrow", Amount: "100.00", Apply: true}}
		}, false},
		{"reimbursement application", func(e *DriverPayEdits) {
			e.ExpenseDeductions = []ExpenseDeduction{{Source: "reimburse", Amount: "100.00", Apply: true}}
		}, false},
		{"charge reset", func(e *DriverPayEdits) { e.GeneratedCharges = []ChargeOccurrence{{Reset: true}} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := before
			tc.edit(&after)
			if got := payrollStatementLocalSave(before, after); got != tc.local {
				t.Fatalf("local=%v want %v", got, tc.local)
			}
		})
	}
}
