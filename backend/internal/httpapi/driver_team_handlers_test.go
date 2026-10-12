package httpapi

import "testing"

func TestDriverTeamPermissions(t *testing.T) {
	for _, tc := range []struct{ method, want string }{{"GET", "fleet.read"}, {"PUT", "payroll.write"}} {
		if got := routePermission(tc.method, "/drivers/00000000-0000-0000-0000-000000000001/team"); got != tc.want {
			t.Fatalf("%s requires %s, got %s", tc.method, tc.want, got)
		}
	}
}
