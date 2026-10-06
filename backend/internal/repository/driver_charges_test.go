package repository

import (
	"testing"
)

func TestChargeProjection(t *testing.T) {
	total := "650.00"
	s := ChargeSchedule{ID: "plan", Kind: "installment", Name: "Advance", Direction: "charge", StartWeek: "2026-09-28", Eligibility: "calendar", Total: &total, Version: 1, Phases: []ChargePhase{{WeekStart: "2026-09-28", Amount: "100.00"}}}
	rows, err := projectCharges(s, "2027-01-04", nil, false)
	if err != nil || len(rows) != 7 || rows[6].Amount != "-50.00" {
		t.Fatalf("rounding: %+v %v", rows, err)
	}
	s.Occurrences = []ChargeOccurrence{{ScheduleID: s.ID, WeekStart: s.StartWeek, Name: s.Name, Amount: "-60.00", ScheduledAmount: "-100.00", Overridden: true, Version: 1}}
	rows, err = projectCharges(s, "2027-01-04", nil, false)
	if err != nil || len(rows) != 7 || rows[6].Amount != "-50.00" || rows[1].Amount != "-140.00" {
		t.Fatalf("reduced: %+v %v", rows, err)
	}
	s.Occurrences[0].Amount = "0.00"
	rows, err = projectCharges(s, "2027-01-04", nil, false)
	if err != nil || len(rows) != 7 || rows[0].Amount != "0.00" || rows[1].Amount != "-200.00" || rows[6].Amount != "-50.00" {
		t.Fatalf("skip: %+v %v", rows, err)
	}
	s.Occurrences[0].Amount = "-650.01"
	if _, err = projectCharges(s, "2027-01-04", nil, false); err == nil {
		t.Fatal("overallocated plan accepted")
	}
	s.Occurrences = nil
	s.Eligibility = "loads"
	loads := map[string]bool{"2026-10-05": true, "2026-10-19": true}
	rows, err = projectCharges(s, "2026-10-26", loads, false)
	if err != nil || len(rows) != 2 || rows[0].WeekStart != "2026-10-05" {
		t.Fatalf("eligibility: %+v %v", rows, err)
	}
	early, _ := projectCharges(s, "2026-10-05", loads, false)
	late, _ := projectCharges(s, "2026-10-26", loads, false)
	again, _ := projectCharges(s, "2026-10-05", loads, false)
	if early[0] != again[0] || early[0] != late[0] {
		t.Fatal("read order changes projection")
	}
	// A saved decision survives removal of its Gross Board load.
	s.Occurrences = []ChargeOccurrence{{ScheduleID: s.ID, WeekStart: s.StartWeek, Amount: "-40.00", Version: 1}}
	rows, _ = projectCharges(s, "2026-09-28", nil, false)
	if len(rows) != 1 || rows[0].Amount != "-40.00" {
		t.Fatal("saved week disappeared")
	}
}
func TestChargeCountRoundingAndPause(t *testing.T) {
	for _, test := range []struct {
		total string
		count int
		last  string
	}{{"650.00", 6, "-108.35"}, {"1.00", 60, "-0.41"}, {"10.00", 3, "-3.34"}} {
		c := ChargeCreate{Kind: "installment", DriverIDs: []string{"driver"}, Name: "Advance", Total: test.total, Installments: test.count, Eligibility: "calendar", StartWeek: ChargeCurrentWeek()}
		if err := validateChargeCreate(&c); err != nil {
			t.Fatal(err)
		}
		s := ChargeSchedule{Kind: c.Kind, Name: c.Name, Direction: "charge", StartWeek: c.StartWeek, Total: &c.Total, Eligibility: c.Eligibility, InstallmentCount: c.Installments, Phases: []ChargePhase{{WeekStart: c.StartWeek, Amount: c.Amount}}}
		rows, err := projectCharges(s, "2100-12-27", nil, false)
		if err != nil || len(rows) != test.count || rows[len(rows)-1].Amount != test.last {
			t.Fatalf("%+v: %d rows, %+v %v", test, len(rows), rows, err)
		}
		s.Phases[0].Paused = true
		rows, err = projectCharges(s, "2100-12-27", nil, false)
		if err != nil || len(rows) != 0 {
			t.Fatal("paused schedule charged")
		}
	}
	for _, bad := range []string{"1e3", "NaN", "0.001", "10000000000.00", "+2"} {
		if _, err := chargeCents(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if _, err := chargeWeek("2026-09-29"); err == nil {
		t.Fatal("accepted Tuesday")
	}
}
