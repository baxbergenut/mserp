package httpapi

import "testing"

func TestProfilePermissions(t *testing.T) {
	for _, v := range []struct{ method, path, want string }{
		{"GET", "/drivers/id/notes", "fleet.read"}, {"POST", "/drivers/id/notes", "fleet.write"},
		{"GET", "/trucks/id/notes", "fleet.read"}, {"POST", "/trucks/id/notes", "fleet.write"},
		{"GET", "/trucks/id/assignments", "fleet.read"}, {"GET", "/investors/id", "fleet.read"},
		{"GET", "/dispatchers/id", "fleet.read"}, {"GET", "/investor-pay/history", "payroll.read"},
	} {
		if got := routePermission(v.method, v.path); got != v.want {
			t.Errorf("%s %s: %s, want %s", v.method, v.path, got, v.want)
		}
	}
}
