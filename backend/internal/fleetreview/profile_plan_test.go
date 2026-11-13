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
