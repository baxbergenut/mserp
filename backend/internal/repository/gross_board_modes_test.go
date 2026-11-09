package repository

import "testing"

func TestGrossBoardDayModes(t *testing.T) {
	empty := newGrossBoardDayModes()
	load := GrossBoardEntry{Slot: 0, LoadNumber: "12345", DriverRate: "100"}
	loads := empty.clone()
	loads.put("slot:0", load)
	two := loads.clone()
	two.put("slot:1", GrossBoardEntry{Slot: 1, LoadNumber: "67890"})
	status := empty.clone()
	status.statuses["planning:note"] = "Dallas, TX"
	mixed := loads.clone()
	mixed.statuses["planning:note"] = "Dallas, TX"
	financial := mixed.clone()
	load.DriverRate = "150"
	financial.put("slot:0", load)
	replacement := mixed.clone()
	replacement.put("slot:0", GrossBoardEntry{LoadNumber: "replacement"})
	renamed := mixed.clone()
	renamed.statuses["planning:note"] = "Another status"
	fixed := loads.clone()
	fixed.put("slot:1", GrossBoardEntry{DayStatus: "HOME"})
	fixedOnly := empty.clone()
	fixedOnly.put("slot:0", GrossBoardEntry{DayStatus: "HOME"})
	fixedAndPlanning := fixedOnly.clone()
	fixedAndPlanning.statuses["planning:note"] = "Dallas, TX"
	tests := []struct {
		name          string
		before, after grossBoardDayModes
		want          bool
	}{
		{"new planning beside fixed", fixedOnly, fixedAndPlanning, false}, {"new fixed beside planning", status, fixedAndPlanning, false},
		{"default load", empty, loads, true}, {"multiple loads", loads, two, true},
		{"empty to status", empty, status, true}, {"status to load after clear", status, loads, true},
		{"new planning on load", loads, mixed, false}, {"new load on planning", status, mixed, false},
		{"fixed status with sibling load", loads, fixed, false},
		{"legacy financial edit", mixed, financial, true}, {"legacy remove status", mixed, loads, true},
		{"legacy remove load", mixed, status, true}, {"legacy replace load forbidden", mixed, replacement, false},
		{"legacy replace status forbidden", mixed, renamed, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validGrossBoardModeChange(tt.before, tt.after); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
	for _, e := range []GrossBoardEntry{{OriginalRate: "0"}, {DriverRate: "5"}, {Miles: "1"}, {EnteredMiles: "2"}} {
		kind, _ := boardEntryMode(e)
		if kind != "load" {
			t.Fatalf("financial content not protected: %+v", e)
		}
	}
	kind, _ := boardEntryMode(GrossBoardEntry{Deleted: true, LoadNumber: "old", DayStatus: "HOME"})
	if kind != "" {
		t.Fatal("deleted entry contributes")
	}
	kind, _ = boardEntryMode(GrossBoardEntry{DayStatus: "HOME", OriginalRate: "1"})
	if kind != "status" {
		t.Fatal("fixed status must use its existing canonical clearing rule")
	}
}
