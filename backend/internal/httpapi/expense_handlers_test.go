package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"mserp/internal/repository"
)

const testExpenseCategoryID = "00000000-0000-4000-8000-000000000099"

func TestExpenseCostBreakdownValidation(t *testing.T) {
	for _, tc := range []struct {
		rows  []repository.ExpenseCostItem
		total string
		valid bool
	}{
		{nil, "10", true},
		{[]repository.ExpenseCostItem{{Name: "Oil change", Amount: "0.10"}, {Name: "Tire issue", Amount: "0.20"}}, "0.30", true},
		{[]repository.ExpenseCostItem{{Name: "Oil change", Amount: "100"}, {Name: "Discount", Amount: "-10"}}, "90", true},
		{[]repository.ExpenseCostItem{{Name: "Oil change", Amount: "10.01"}}, "10", false},
		{[]repository.ExpenseCostItem{{Name: " ", Amount: "10"}}, "10", false},
		{[]repository.ExpenseCostItem{{Name: "Oil change", Amount: "10.001"}}, "10", false},
		{[]repository.ExpenseCostItem{{Name: "Oil change", Amount: "NaN"}}, "10", false},
	} {
		r := expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "2026-10-14", ExpenseType: "Maintenance", Amount: tc.total, CostBreakdown: &tc.rows}
		_, err := r.validate()
		if (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	rows := []repository.ExpenseCostItem{{Name: "Oil change", Amount: "500.01"}}
	base := "500.01"
	value, err := (expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "2026-10-14", ExpenseType: "Oil change", Amount: base, BaseAmount: &base, PaymentType: "EFS", CostBreakdown: &rows}).validate()
	if err != nil || value.Amount != "506.01" {
		t.Fatal("breakdown must match base before EFS fee", value, err)
	}
}

func TestExpenseEFSFee(t *testing.T) {
	for _, tc := range []struct{ base, fee, total string }{
		{"0", "0.00", "0.00"}, {"0.01", "3.00", "3.01"},
		{"300", "3.00", "303.00"}, {"500", "3.00", "503.00"},
		{"500.01", "6.00", "506.01"}, {"634", "6.00", "640.00"},
		{"1000", "6.00", "1006.00"}, {"1000.01", "9.00", "1009.01"},
	} {
		r := expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "2026-10-12", ExpenseType: "Parking", BaseAmount: &tc.base, Amount: "999", PaymentType: " eFs "}
		value, err := r.validate()
		if err != nil || value.Amount != tc.total || value.PaymentFee != tc.fee || *value.BaseAmount != tc.base {
			t.Fatalf("%s: %+v %v", tc.base, value, err)
		}
		r.Amount = value.Amount // Editing a saved total must use the original base, not compound the fee.
		second, err := r.validate()
		if err != nil || second.Amount != tc.total {
			t.Fatal("fee compounded", second, err)
		}
		r.PaymentType = "Cash"
		value, err = r.validate()
		if err != nil || value.Amount != tc.base || value.PaymentFee != "0.00" {
			t.Fatal("fee remained after switching method", value, err)
		}
	}
	for _, base := range []string{"-1", "1.001", "", "abc"} {
		r := expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "2026-10-12", ExpenseType: "Parking", BaseAmount: &base, PaymentType: "EFS"}
		if _, err := r.validate(); err == nil {
			t.Fatal("invalid EFS base accepted", base)
		}
	}
}

func TestExpenseRequestIgnoresRetiredCompany(t *testing.T) {
	for _, legacy := range []string{"", `,"company":"Historical company"`, `,"company":null`} {
		body := `{"categoryId":"` + testExpenseCategoryID + `","expenseDate":"2026-10-12","expenseType":"Repair","amount":"10"` + legacy + `}`
		var request expenseRequest
		if err := decodeJSON(httptest.NewRequest("POST", "/expenses", strings.NewReader(body)), &request); err != nil {
			t.Fatal(err)
		}
		if _, err := request.validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpenseCategoryActions(t *testing.T) {
	access := []repository.ExpenseCategoryAccess{{CategoryID: testExpenseCategoryID, CanView: true, CanCreate: true}}
	visible, accessible := expenseCategoryIDs(access)
	if len(visible) != 1 || len(accessible) != 1 || !hasExpenseCategoryAction(access, testExpenseCategoryID, "view") || !hasExpenseCategoryAction(access, testExpenseCategoryID, "create") {
		t.Fatal("expected view and create category access")
	}
	if hasExpenseCategoryAction(access, testExpenseCategoryID, "edit") || hasExpenseCategoryAction(access, "00000000-0000-4000-8000-000000000098", "view") {
		t.Fatal("category action escaped its assigned category")
	}
}

func TestExpenseRequestValidate(t *testing.T) {
	t.Parallel()

	request := expenseRequest{
		CategoryID: testExpenseCategoryID, ExpenseDate: "2026-09-23",
		Amount: "1234.50", UnitNumber: " 010 ", ExpenseType: " Oil change ", Description: " oil change ",
	}
	input, err := request.validate()
	if err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	if input.Amount != "1234.50" {
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
		request := expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "2026-09-29", Amount: "100", ExpenseType: name}
		if _, err := request.validate(); err == nil || err.Error() != "expense name is required" {
			t.Fatalf("name %q: expected required name error, got %v", name, err)
		}
	}
}

func TestDriverExpenseValidation(t *testing.T) {
	r := expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "2026-09-29", Amount: "100.25", ExpenseType: "Parking violation", CoveredBy: " driver "}
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
	if got := validExtractedCategory(&unknownCategory, []string{"Other"}); got != "" {
		t.Fatalf("validExtractedCategory() = %q, want empty", got)
	}
	customCategory := " travel "
	if got := validExtractedCategory(&customCategory, []string{"Travel"}); got != "Travel" {
		t.Fatalf("custom category = %q, want Travel", got)
	}
}

func TestExpenseRequestValidateRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request expenseRequest
	}{

		{name: "empty category", request: expenseRequest{CategoryID: "", ExpenseDate: "2026-09-23", Amount: "10.00"}},
		{name: "invalid date", request: expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "09/23/2026", Amount: "10.00"}},
		{name: "missing amount", request: expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "2026-09-23"}},
		{name: "too many cents", request: expenseRequest{CategoryID: testExpenseCategoryID, ExpenseDate: "2026-09-23", Amount: "10.001"}},
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
