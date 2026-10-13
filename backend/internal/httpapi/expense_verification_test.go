package httpapi

import (
	"mserp/internal/repository"
	"testing"
)

func TestExpenseVerificationRole(t *testing.T) {
	for _, tc := range []struct {
		user repository.AuthUser
		want string
	}{
		{repository.AuthUser{Administrator: true}, "admin"},
		{repository.AuthUser{RoleName: "Accountant"}, "accountant"},
		{repository.AuthUser{RoleName: " accountant "}, "accountant"},
		{repository.AuthUser{RoleName: "Manager"}, ""},
		{repository.AuthUser{RoleName: "Administrator"}, ""},
		{repository.AuthUser{Permissions: []string{"access.manage", "expenses.write"}}, ""},
	} {
		if got := expenseVerificationRole(tc.user); got != tc.want {
			t.Errorf("role = %q, want %q", got, tc.want)
		}
	}
}

func TestExpenseVerificationFilters(t *testing.T) {
	for _, value := range []string{"", "true", "false", "yes", "1", "admin"} {
		got, err := parseExpenseVerificationFilter(value)
		if value == "" {
			if got != nil || err != nil {
				t.Fatal("empty filter must include all statuses")
			}
		} else if value == "true" || value == "false" {
			if err != nil || got == nil || *got != (value == "true") {
				t.Fatalf("invalid parsed filter %q", value)
			}
		} else if err == nil {
			t.Fatalf("accepted invalid filter %q", value)
		}
	}
}

func TestExpenseFormCannotSetVerification(t *testing.T) {
	input, err := (expenseRequest{Company: "MS Express", CategoryID: testExpenseCategoryID,
		ExpenseDate: "2026-10-12", Amount: "25", ExpenseType: "Parking", ManagerVerified: true, AccountingVerified: true}).validate()
	if err != nil {
		t.Fatal(err)
	}
	if input.ManagerVerified || input.AccountingVerified {
		t.Fatal("form payload bypassed role verification")
	}
}
