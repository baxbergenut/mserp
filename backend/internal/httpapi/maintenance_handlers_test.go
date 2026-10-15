package httpapi

import (
	"mserp/internal/repository"
	"testing"
)

func TestMaintenanceRequestValidation(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000098"
	truck := "00000000-0000-4000-8000-000000000097"
	base := maintenanceRequest{ID: id, TruckID: &truck, ServiceDate: "2026-10-01", Location: "shop", Status: "completed", Services: []repository.MaintenanceWork{{TypeCode: "oil", Name: "Oil change", TrackingCode: "oil"}}}
	if _, err := base.validate(testExpenseCategoryID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*maintenanceRequest)
	}{
		{"missing truck", func(v *maintenanceRequest) { v.TruckID = nil }},
		{"missing service date", func(v *maintenanceRequest) { v.ServiceDate = "" }},
		{"bad date", func(v *maintenanceRequest) { v.ServiceDate = "2026-02-31" }},
		{"future date", func(v *maintenanceRequest) { v.ServiceDate = "2099-01-01" }},
		{"negative mileage", func(v *maintenanceRequest) { n := -1; v.Odometer = &n }},
		{"no work", func(v *maintenanceRequest) { v.Services = nil }},
		{"paid work without invoice", func(v *maintenanceRequest) { v.Services[0].Amount = stringPointer("10") }},
		{"partial prices", func(v *maintenanceRequest) {
			v.Services = append(v.Services, repository.MaintenanceWork{TypeCode: "tires", Name: "Tires", Amount: stringPointer("10")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := base
			value.Services = append([]repository.MaintenanceWork(nil), base.Services...)
			tc.change(&value)
			if _, err := value.validate(testExpenseCategoryID); err == nil {
				t.Fatal("invalid maintenance accepted")
			}
		})
	}
	canonical := base
	canonical.Services = []repository.MaintenanceWork{{TypeCode: "electrical", TrackingCode: "pm"}}
	normalized, err := canonical.validate(testExpenseCategoryID)
	if err != nil || normalized.Services[0].TrackingCode != "" {
		t.Fatal("repair spoofed PM completion", err)
	}
	review := base
	review.Status = "needs_review"
	review.TruckID = nil
	review.ServiceDate = ""
	if _, err := review.validate(testExpenseCategoryID); err != nil {
		t.Fatal("unknown history must remain reviewable", err)
	}
	paid := base
	paid.Services = append([]repository.MaintenanceWork(nil), base.Services...)
	paid.Services[0].Amount = stringPointer("500.01")
	paid.Expense = &expenseRequest{ExpenseDate: "2026-10-01", ExpenseType: "Oil change", Amount: "500.01", BaseAmount: stringPointer("500.01"), PaymentType: "EFS"}
	input, err := paid.validate(testExpenseCategoryID)
	if err != nil || input.Expense.Amount != "506.01" {
		t.Fatal("EFS base validation", input, err)
	}
	if routePermission("GET", "/maintenance/documents/"+id) != "authenticated" {
		t.Fatal("maintenance routes must defer to category access")
	}
}
