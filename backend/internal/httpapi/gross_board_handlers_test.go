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
