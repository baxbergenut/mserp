package httpapi

import "testing"

func TestExpenseRequestValidate(t *testing.T) {
	t.Parallel()

	request := expenseRequest{
		Company: " MS Express ", Category: "Maintenance", ExpenseDate: "2026-09-23",
		Amount: "1234.50", UnitNumber: " 010 ", Description: " oil change ",
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
			if _, err := test.request.validate(); err == nil {
				t.Fatal("validate() error = nil, want error")
			}
		})
	}
}
