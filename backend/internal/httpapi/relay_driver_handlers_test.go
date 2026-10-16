package httpapi

import (
	"mserp/internal/repository"
	"testing"
)

func TestRelayDriverPermissions(t *testing.T) {
	for _, v := range []struct{ method, path, permission string }{
		{"GET", "/relay-drivers", "fuel.read"}, {"POST", "/relay-drivers/refresh", "fuel.drivers.sync"},
		{"POST", "/relay-drivers/change", "authenticated"}, {"POST", "/relay-drivers/dr_one/link", "tasks.review"},
		{"POST", "/relay-drivers/operations/id/verify", "fuel.drivers.recover"},
	} {
		if got := routePermission(v.method, v.path); got != v.permission {
			t.Fatalf("%s: got %s", v.path, got)
		}
	}
}
func TestRelayChangeValidation(t *testing.T) {
	input := repository.RelayDriverChange{RequestID: "07500000-0000-0000-0000-000000000001", Action: "add", DriverID: "07500000-0000-0000-0000-000000000002", FirstName: "Test", LastName: "Driver", Phone: "(555) 123-4567"}
	if !validateRelayChange(&input) || input.Phone != "+15551234567" {
		t.Fatal("valid US driver rejected")
	}
	input.Action = "transfer"
	if validateRelayChange(&input) {
		t.Fatal("accepted unknown action")
	}
	input.Action = "add"
	input.Phone = "invalid"
	if validateRelayChange(&input) {
		t.Fatal("accepted invalid phone")
	}
}
