package repository

import "testing"

func TestManualBoardPlanAliases(t *testing.T) {
	id1, id2 := 1, 2
	for _, tc := range []struct {
		name  string
		plans []BoardLoad
		want  bool
	}{
		{"unimported", []BoardLoad{{Manual: true, PlanID: "manual", Number: " 001-Abc "}, {PlanID: "gross", Number: "001-abc"}}, true},
		{"resolved manual with unlinked accounting plan", []BoardLoad{{Manual: true, PlanID: "manual", Number: "001", LoadID: &id1}, {PlanID: "gross", Number: "001"}}, false},
		{"different number", []BoardLoad{{Manual: true, PlanID: "manual", Number: "001"}, {PlanID: "gross", Number: "002"}}, false},
		{"leading zero preserved", []BoardLoad{{Manual: true, PlanID: "manual", Number: "001"}, {PlanID: "gross", Number: "1"}}, false},
		{"ambiguous with pinned source", []BoardLoad{{Manual: true, PlanID: "manual", Number: "001"}, {PlanID: "gross", Number: "001", LoadID: &id1}}, false},
		{"distinct sources", []BoardLoad{{Manual: true, PlanID: "manual", Number: "001", LoadID: &id1}, {PlanID: "gross", Number: "001", LoadID: &id2}}, false},
		{"missing source", []BoardLoad{{Manual: true, PlanID: "manual", Number: "001", SourceMissing: true}, {PlanID: "gross", Number: "001"}}, false},
		{"multiple manual identities", []BoardLoad{{Manual: true, PlanID: "manual", Number: "001"}, {Manual: true, PlanID: "manual2", Number: "001"}, {PlanID: "gross", Number: "001"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := manualBoardPlanAliases(tc.plans)
			if (got["gross"] == "manual") != tc.want {
				t.Fatalf("aliases: %v", got)
			}
		})
	}
}

func TestPreferredGrossBoardQueuePlans(t *testing.T) {
	manual := BoardLoad{Manual: true, PlanID: "manual", Number: "001"}
	old := BoardLoad{PlanID: "old", Number: "001", Date: "2026-09-01"}
	gross := BoardLoad{PlanID: "gross", Number: "001", Date: "2026-11-10", Slot: 2}
	plans := []BoardLoad{manual, old, gross}
	aliases := manualBoardPlanAliases(plans)
	got := preferredGrossBoardQueuePlans(plans, aliases, boardLoadState{}, "2026-11-10")
	if got["manual"] != "gross" {
		t.Fatalf("preferred: %v", got)
	}
	got = preferredGrossBoardQueuePlans(plans[:2], aliases, boardLoadState{}, "2026-11-10")
	if len(got) != 0 {
		t.Fatalf("old history displaced deliberate manual queue: %v", got)
	}
}
