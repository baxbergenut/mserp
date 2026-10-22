package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"mserp/internal/repository"
)

func TestExpenseEditProjectionIsPerUser(t *testing.T) {
	e := repository.Expense{CategoryID: "category", CreatorRoleID: "creator-role"}
	m := repository.MaintenanceRecord{CategoryID: "category", CreatorRoleID: "creator-role", Expense: &e}
	page := repository.MaintenancePage{Items: []repository.MaintenanceRecord{m}}
	for _, role := range []string{"creator-role", "other-role"} {
		user := repository.AuthUser{RoleID: role, ExpenseCategoryAccess: []repository.ExpenseCategoryAccess{{CategoryID: "category", CanView: true, CanCreate: true}}}
		out := projectExpenseEditAccess(page, user).(repository.MaintenancePage)
		want := role == "creator-role"
		if out.Items[0].CanEdit != want || out.Items[0].Expense.CanEdit != want {
			t.Fatalf("wrong capability for %s", role)
		}
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), "creator-role") {
			t.Fatal("creator role leaked into JSON")
		}
	}
	if page.Items[0].CanEdit || e.CanEdit {
		t.Fatal("projection mutated shared records")
	}
}
