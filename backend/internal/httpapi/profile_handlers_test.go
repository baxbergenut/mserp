package httpapi

import "testing"

func TestProfilePermissions(t *testing.T) {
	for _, v := range []struct{ method, path, want string }{
		{"GET", "/drivers/id/notes", "drivers.read"}, {"POST", "/drivers/id/notes", "drivers.edit"},
		{"GET", "/trucks/id/notes", "trucks.read"}, {"POST", "/trucks/id/notes", "trucks.edit"},
		{"GET", "/trucks/id/assignments", "trucks.read"}, {"GET", "/investors/id", "investors.read"},
		{"GET", "/dispatchers/id", "dispatchers.read"}, {"GET", "/investor-pay/history", "investor_pay.read"},
	} {
		if got := routePermission(v.method, v.path); got != v.want {
			t.Errorf("%s %s: %s, want %s", v.method, v.path, got, v.want)
		}
	}
}
