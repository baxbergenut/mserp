package httpapi

import "testing"

func TestExpenseRequestValidate(t *testing.T) {
	t.Parallel()

	request := expenseRequest{
		Company: " MS Express ", Category: "Maintenance", ExpenseDate: "2026-09-23",
		Amount: "1234.50", UnitNumber: " 010 ", ExpenseType: " Oil change ", Description: " oil change ",
	}
	input, err := request.validate()
	if err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	if input.Company != "MS Express" || input.Amount != "1234.50" {
		t.Fatalf("validate() input = %#v", input)
	}
	if input.UnitNumber == nil || *input.UnitNumber != "010" {
		t.Fatalf("validate() unit = %#v", input.UnitNumber)
	}
	if input.Description == nil || *input.Description != "oil change" {
		t.Fatalf("validate() description = %#v", input.Description)
	}
	if input.ExpenseType == nil || *input.ExpenseType != "Oil change" {
		t.Fatalf("validate() name = %#v", input.ExpenseType)
	}
}

func TestExpenseNameRequired(t *testing.T) {
	for _, name := range []string{"", " \t\n "} {
		request := expenseRequest{Company: "MS Express", Category: "Penalties", ExpenseDate: "2026-09-29", Amount: "100", ExpenseType: name}
		if _, err := request.validate(); err == nil || err.Error() != "expense name is required" {
			t.Fatalf("name %q: expected required name error, got %v", name, err)
		}
	}
}

func TestDriverExpenseValidation(t *testing.T) {
	r := expenseRequest{Company: "MS Express", Category: "Penalties", ExpenseDate: "2026-09-29", Amount: "100.25", ExpenseType: "Parking violation", CoveredBy: " driver "}
	if _, err := r.validate(); err == nil {
		t.Fatal("driver responsibility requires a linked driver")
	}
	driver := "00000000-0000-4000-8000-000000000001"
	r.DriverID = &driver
	input, err := r.validate()
	if err != nil || *input.CoveredBy != "Driver" {
		t.Fatalf("penalty: %+v %v", input, err)
	}
	r.Amount = "-1"
	if _, err := r.validate(); err == nil {
		t.Fatal("negative driver expense accepted")
	}
	r.CoveredBy = "Company"
	if _, err := r.validate(); err != nil {
		t.Fatal("company credits must remain supported", err)
	}
}

func TestNormalizeExtractedExpenseValues(t *testing.T) {
	t.Parallel()

	amount := " $1,234.5 "
	if got := validExtractedAmount(&amount); got != "1234.50" {
		t.Fatalf("validExtractedAmount() = %q, want 1234.50", got)
	}
	negative := "-10.00"
	if got := validExtractedAmount(&negative); got != "" {
		t.Fatalf("validExtractedAmount(negative) = %q, want empty", got)
	}
	invalidDate := "09/23/2026"
	if got := validExtractedDate(&invalidDate); got != "" {
		t.Fatalf("validExtractedDate() = %q, want empty", got)
	}
	unknownCategory := "Fuel"
	if got := validExtractedCategory(&unknownCategory); got != "Other" {
		t.Fatalf("validExtractedCategory() = %q, want Other", got)
	}
}

func TestExpenseRequestValidateRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request expenseRequest
	}{
		{name: "missing company", request: expenseRequest{Category: "Safety", ExpenseDate: "2026-09-23", Amount: "10.00"}},
		{name: "unknown category", request: expenseRequest{Company: "MS Express", Category: "Fuel", ExpenseDate: "2026-09-23", Amount: "10.00"}},
		{name: "invalid date", request: expenseRequest{Company: "MS Express", Category: "Safety", ExpenseDate: "09/23/2026", Amount: "10.00"}},
		{name: "missing amount", request: expenseRequest{Company: "MS Express", Category: "Safety", ExpenseDate: "2026-09-23"}},
		{name: "too many cents", request: expenseRequest{Company: "MS Express", Category: "Safety", ExpenseDate: "2026-09-23", Amount: "10.001"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.request.ExpenseType = "Test expense"
			if _, err := test.request.validate(); err == nil {
				t.Fatal("validate() error = nil, want error")
			}
		})
	}
}
