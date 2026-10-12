package httpapi

import "testing"

func TestDriverTeamPermissions(t *testing.T) {
	for _, tc := range []struct{ method, want string }{{"GET", "fleet.read"}, {"PUT", "payroll.write"}} {
		if got := routePermission(tc.method, "/drivers/00000000-0000-0000-0000-000000000001/team"); got != tc.want {
			t.Fatalf("%s requires %s, got %s", tc.method, tc.want, got)
		}
	}
}

func TestTeamDriverRequestHasNoCompensation(t *testing.T) {
	team := true
	request := driverRequest{FullName: "New Secondary", IsTeamDriver: &team, PayType: "", PayRate: -1, EscrowAmount: "bad", Active: true}
	input, err := request.validate()
	if err != nil || input.PayRate != 0 || input.PayType != "cpm" || input.EscrowAmount != "" {
		t.Fatalf("team compensation must be ignored: %+v %v", input, err)
	}
	request.IsTeamDriver = nil
	if _, err = request.validate(); err == nil {
		t.Fatal("solo pay validation was skipped")
	}
	if got := routePermission("GET", "/drivers/team-trucks"); got != "fleet.read" {
		t.Fatal("team truck permission", got)
	}
}
