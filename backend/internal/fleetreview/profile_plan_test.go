package fleetreview

import "testing"

func TestHistoricalDriverDeferral(t *testing.T) {
	source := Snapshot{Drivers: []Record{
		{"id": "historical", "status": "terminated"},
		{"id": "active", "status": "active"},
	}}
	plan := buildProfilePlan(State{}, source, Snapshot{})
	if plan.Review != 1 || plan.Matching != 0 || len(plan.Rows) != 2 ||
		plan.Rows[0].Deferred != historicalDriverWithoutMatch || len(plan.Rows[0].Changes) != 0 {
		t.Fatalf("unexpected preview: %+v", plan)
	}

	frozen := profilePlan{Review: 1, Rows: []profileRow{{
		Kind: "driver", LocalID: "historical", Review: []string{historicalDriverWithoutMatch},
	}}}
	hash := digest(frozen)
	display := frozen.forDisplay()
	if display.Review != 0 || display.Rows[0].Deferred != historicalDriverWithoutMatch || digest(frozen) != hash {
		t.Fatal("display must defer historical rows while preserving the frozen plan")
	}
}

func TestConfirmedDriverIdentityKeepsOtherReviewReasons(t *testing.T) {
	plan := profilePlan{Review: 4, Rows: []profileRow{
		{Kind: "driver", LocalID: "driver", Review: []string{uniqueFleetScopeIdentity}},
		{Kind: "driver", RemoteID: "candidate", Review: []string{"No verified Clustie match"}},
		{Kind: "driver", LocalID: "other", Review: []string{uniqueFleetScopeIdentity, "Review employment"}},
		{Kind: "truck", LocalID: "truck", Review: []string{uniqueFleetScopeIdentity}},
	}}
	hash := digest(plan)
	display := plan.forDisplay()
	display.applyIdentityConfirmations(Record{"driver": "candidate", "other": "second", "truck": "vehicle"})
	if display.Review != 2 || len(display.Rows) != 3 || !display.Rows[0].IdentityConfirmed ||
		display.Rows[0].RemoteID != "candidate" || len(display.Rows[0].Review) != 0 ||
		len(display.Rows[1].Review) != 1 || display.Rows[1].Review[0] != "Review employment" ||
		display.Rows[2].IdentityConfirmed || digest(plan) != hash {
		t.Fatalf("unexpected plan display: %+v", display)
	}
}

func TestTruckMakeAbbreviations(t *testing.T) {
	for _, tc := range []struct{ local, remote, want string }{
		{"FRHT", "Freightliner", "FRHT"},
		{"International", "INTL", "INTL"},
		{"FRHT", "International", ""},
	} {
		if got := preferredTruckMake(tc.local, tc.remote); got != tc.want {
			t.Errorf("%q vs %q: got %q, want %q", tc.local, tc.remote, got, tc.want)
		}
	}

	plan := profilePlan{Review: 1, Rows: []profileRow{{
		Kind: "truck", LocalID: "local", RemoteID: "remote",
		Review: []string{conflictingTruckMake, "Review truck unit number"},
	}}}
	source := Snapshot{Trucks: []Record{{"id": "local", "vin": "1FUJGLDR9CS000001", "make": "FRHT"}}}
	receiver := Snapshot{Trucks: []Record{{"id": "remote", "vin": "1FUJGLDR9CS000001", "make": "FRHT"}}}
	display := plan.forDisplay()
	display.resolveTruckMakeConflicts(source, receiver)
	if display.Review != 1 || len(display.Rows[0].Review) != 1 || display.Rows[0].Review[0] != "Review truck unit number" || len(plan.Rows[0].Review) != 2 {
		t.Fatalf("unexpected live review flags: %+v", display)
	}
}
