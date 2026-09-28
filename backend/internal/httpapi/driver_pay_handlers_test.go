package httpapi

import (
	"mserp/internal/repository"
	"testing"
)

func TestDriverPayValidation(t *testing.T) {
	valid := func() repository.DriverPayEdits {
		return repository.DriverPayEdits{DriverID: "00000000-0000-0000-0000-000000000001", WeekStart: "2026-09-28", Comments: map[string]string{"2026-09-28:1:load-a": "OK"}, Adjustments: []repository.DriverPayAdjustment{{ID: "00000000-0000-0000-0000-000000000002", Kind: "deduction", Name: "Advance", Amount: "10.25"}}}
	}
	e := valid()
	if err := validateDriverPayEdits(&e); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		change func(*repository.DriverPayEdits)
	}{
		{"week", func(e *repository.DriverPayEdits) { e.WeekStart = "2026-09-29" }},
		{"negative", func(e *repository.DriverPayEdits) { e.Adjustments[0].Amount = "-5" }},
		{"precision", func(e *repository.DriverPayEdits) { e.Adjustments[0].Amount = "1.005" }},
		{"zero", func(e *repository.DriverPayEdits) { e.Adjustments[0].Amount = "0.00" }},
		{"kind", func(e *repository.DriverPayEdits) { e.Adjustments[0].Kind = "fuel" }},
		{"duplicate", func(e *repository.DriverPayEdits) { e.Adjustments = append(e.Adjustments, e.Adjustments[0]) }},
		{"comment week", func(e *repository.DriverPayEdits) { e.Comments["2026-10-05:0:other"] = "Bad" }},
		{"comment slot", func(e *repository.DriverPayEdits) { e.Comments["2026-09-28:100:other"] = "Bad" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := valid()
			c.change(&e)
			if validateDriverPayEdits(&e) == nil {
				t.Fatal("expected invalid edits")
			}
		})
	}
}

func TestGrossBoardMultipleSlots(t *testing.T) {
	e := repository.GrossBoardEntry{DriverID: "00000000-0000-0000-0000-000000000001", Date: "2026-09-28", LoadNumber: "A"}
	second := e
	second.Slot = 1
	second.LoadNumber = "B"
	r := grossBoardRequest{WeekStart: "2026-09-28", Entries: []repository.GrossBoardEntry{e, second}}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	r.Entries[1].Slot = 100
	if r.validate() == nil {
		t.Fatal("expected slot limit")
	}
	r.Entries[1].Slot = 1
	r.Entries[1].Deleted = true
	if err := r.validate(); err != nil || r.Entries[1].LoadNumber != "" {
		t.Fatal("deletion not canonicalized")
	}
}
