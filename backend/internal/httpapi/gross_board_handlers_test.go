package httpapi

import (
	"mserp/internal/repository"
	"testing"
)

func TestGrossBoardValidation(t *testing.T) {
	valid := func() grossBoardRequest {
		return grossBoardRequest{WeekStart: "2026-09-28", Entries: []repository.GrossBoardEntry{{DriverID: "00000000-0000-0000-0000-000000000001", Date: "2026-09-28", LoadNumber: "  HOME  ", OriginalRate: "0.10", DriverRate: "-0.20", Miles: "100.25"}}}
	}
	request := valid()
	if err := request.validate(); err != nil {
		t.Fatal(err)
	}
	if request.Entries[0].LoadNumber != "HOME" {
		t.Fatal("text was not trimmed")
	}
	tests := []struct {
		name   string
		mutate func(*grossBoardRequest)
	}{
		{"Sunday week", func(r *grossBoardRequest) { r.WeekStart = "2026-09-27" }},
		{"outside week", func(r *grossBoardRequest) { r.Entries[0].Date = "2026-10-05" }},
		{"bad date", func(r *grossBoardRequest) { r.Entries[0].Date = "2026-02-30" }},
		{"negative mileage", func(r *grossBoardRequest) { r.Entries[0].Miles = "-1" }},
		{"negative entered mileage", func(r *grossBoardRequest) { r.Entries[0].EnteredMiles = "-1" }},
		{"invalid entered rate", func(r *grossBoardRequest) { r.Entries[0].EnteredOriginalRate = "NaN" }},
		{"rounding", func(r *grossBoardRequest) { r.Entries[0].OriginalRate = "0.001" }},
		{"overflow", func(r *grossBoardRequest) { r.Entries[0].OriginalRate = "10000000000" }},
		{"NaN", func(r *grossBoardRequest) { r.Entries[0].DriverRate = "NaN" }},
		{"bad driver", func(r *grossBoardRequest) { r.Entries[0].DriverID = "bad" }},
		{"bad version", func(r *grossBoardRequest) { r.Entries[0].Version = -1 }},
		{"duplicate", func(r *grossBoardRequest) { r.Entries = append(r.Entries, r.Entries[0]) }},
		{"alternate uuid duplicate", func(r *grossBoardRequest) {
			e := r.Entries[0]
			e.DriverID = "00000000000000000000000000000001"
			r.Entries = append(r.Entries, e)
		}},
		{"no changes", func(r *grossBoardRequest) { r.Entries = nil }},
		{"unknown status", func(r *grossBoardRequest) { r.Entries[0].DayStatus = "BROKEN" }},
		{"status with load values", func(r *grossBoardRequest) { r.Entries[0].DayStatus = "SHOP" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := valid()
			test.mutate(&r)
			if r.validate() == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestGrossBoardStatuses(t *testing.T) {
	for _, status := range []string{"SHOP", "HOME", "RESET", "IN TRANSIT", "REJECTED", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "NEW DRIVER", "DEADHEAD"} {
		r := grossBoardRequest{WeekStart: "2026-09-28", Entries: []repository.GrossBoardEntry{{DriverID: "00000000-0000-0000-0000-000000000001", Date: "2026-09-28", DayStatus: status}}}
		if err := r.validate(); err != nil {
			t.Fatalf("valid status %s rejected: %v", status, err)
		}
		id := 1
		r.Entries[0].LoadRecordID = &id
		if r.validate() == nil {
			t.Fatal("status with linked load accepted")
		}
	}
}
