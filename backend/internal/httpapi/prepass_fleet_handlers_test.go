package httpapi

import (
	"strings"
	"testing"

	"mserp/internal/repository"
)

func TestPrePassFleetPermissions(t *testing.T) {
	for _, c := range []struct{ method, path, want string }{
		{"GET", "/prepass-fleet", "tolls.read"}, {"POST", "/prepass-fleet/refresh", "tolls.read"},
		{"POST", "/prepass-fleet/link", "tolls.link"}, {"POST", "/prepass-fleet/change", "tolls.fleet.write"},
		{"POST", "/prepass-fleet/operations/id/verify", "tolls.fleet.write"},
	} {
		if got := routePermission(c.method, c.path); got != c.want {
			t.Fatalf("%s: %s", c.path, got)
		}
	}
}
func TestPrePassFleetRequestBoundary(t *testing.T) {
	in := repository.PrePassFleetChange{RequestID: "00000000-0000-4000-8000-000000000001", Action: "assign", AccountNumber: "123", VIN: "1XKADP9X8DJ364740", Revision: strings.Repeat("a", 64), TransponderNumber: "777111222", FleetServiceConfirmed: true}
	if !validatePrePassFleetChange(&in, false) {
		t.Fatal("valid request rejected")
	}
	in.FleetServiceConfirmed = false
	if validatePrePassFleetChange(&in, false) {
		t.Fatal("missing service review accepted")
	}
	in.FleetServiceConfirmed = true
	in.Action = "link"
	if validatePrePassFleetChange(&in, false) {
		t.Fatal("link permission bypass")
	}
	in.Action = "assign"
	if validatePrePassFleetChange(&in, true) {
		t.Fatal("vendor write via local-link permission")
	}
	in.Action = "remove"
	if validatePrePassFleetChange(&in, false) {
		t.Fatal("remove accepted device change")
	}
}
