package httpapi

import "testing"

func TestStatusColorPermissions(t *testing.T) {
	if routePermission("GET", "/settings/status-colors") != "authenticated" {
		t.Fatal("every signed-in user needs the shared palette")
	}
	if routePermission("PUT", "/settings/status-colors") != "access.manage" {
		t.Fatal("shared palette edits need settings management")
	}
}
