package repository

import (
	"strings"
	"testing"
	"time"
)

func TestTransactionLoadCoverage(t *testing.T) {
	now := time.Date(2026, 9, 23, 18, 0, 0, 0, time.UTC)
	date := func(value string) *time.Time {
		v, err := time.Parse(time.RFC3339, value+"T00:01:00Z")
		if err != nil {
			t.Fatal(err)
		}
		return &v
	}
	load := func(start, end string) coverageLoad {
		return makeCoverageLoad(1, "L1", "001", "driver", "Test Driver", "", "delivered", date(start), date(end), nil, nil, now)
	}
	base := load("2026-09-10", "2026-09-12")
	for _, test := range []struct{ day, status string }{
		{"2026-09-09", ""}, {"2026-09-10", ""}, {"2026-09-12", ""}, {"2026-09-13", ""},
		{"2026-09-14", "review"}, {"2026-09-08", "data_issue"},
	} {
		t.Run(test.day, func(t *testing.T) {
			tx := coverageTransaction{kind: "toll", units: []string{" 001 "}, date: coverageDate(*date(test.day)), dateValid: true, positiveCharge: true, production: true}
			f := classifyTransaction(tx, map[string][]coverageLoad{"001": {base}}, nil, &now, now)
			if test.status == "" {
				if f != nil {
					t.Fatalf("covered date flagged: %+v", f)
				}
				return
			}
			if f == nil || f.Status != test.status {
				t.Fatalf("want %s, got %+v", test.status, f)
			}
		})
	}
	tx := coverageTransaction{kind: "fuel", driverID: "driver", units: []string{"001"}, date: coverageDate(*date("2026-09-16")), dateValid: true, positiveCharge: true, production: true}
	byUnit := map[string][]coverageLoad{"001": {base, load("2026-09-20", "2026-09-21")}}
	f := classifyTransaction(tx, byUnit, nil, &now, now)
	if f == nil || f.Status != "review" || f.PreviousLoad == nil || f.NextLoad == nil {
		t.Fatalf("gap between loads must flag: %+v", f)
	}
	byUnit["001"] = append(byUnit["001"], load("2026-09-16", "2026-09-17"))
	if f := classifyTransaction(tx, byUnit, nil, &now, now); f != nil {
		t.Fatalf("new load must clear flag: %+v", f)
	}
	byUnit["001"] = []coverageLoad{base}
	other := load("2026-09-16", "2026-09-17")
	other.TruckUnit = "002"
	f = classifyTransaction(tx, byUnit, map[string][]coverageLoad{"driver": {other}}, &now, now)
	if f == nil || f.Status != "data_issue" || f.RelatedLoad == nil {
		t.Fatalf("driver mismatch: %+v", f)
	}
	oldSync := now.Add(-49 * time.Hour)
	f = classifyTransaction(tx, byUnit, nil, &oldSync, now)
	if f == nil || f.Status != "data_issue" || !strings.Contains(f.Reason, "out of date") {
		t.Fatalf("stale: %+v", f)
	}
	for _, units := range [][]string{nil, {"001", "002"}, {"1"}} {
		tx.units = units
		f = classifyTransaction(tx, byUnit, nil, &now, now)
		if f == nil || f.Status != "data_issue" {
			t.Fatalf("ambiguous/missing history: %+v", f)
		}
	}
	tx.units = []string{"001"}
	tx.dateValid = false
	if f := classifyTransaction(tx, byUnit, nil, &now, now); f == nil || f.Status != "data_issue" {
		t.Fatalf("invalid timezone: %+v", f)
	}
	tx.dateValid = true
	tx.positiveCharge = false
	if f := classifyTransaction(tx, byUnit, nil, &now, now); f != nil {
		t.Fatal("refund flagged")
	}
	tx.positiveCharge, tx.production = true, false
	if f := classifyTransaction(tx, byUnit, nil, &now, now); f != nil {
		t.Fatal("test data flagged")
	}

	t.Run("uncertain load dates", func(t *testing.T) {
		for _, bad := range []coverageLoad{
			load("2026-09-17", "2026-09-15"),
			load("2026-09-15", "3202-09-17"),
			makeCoverageLoad(2, "L2", "001", "driver", "", "", "in_transit", date("2026-09-15"), nil, nil, nil, now),
		} {
			tx.production = true
			f := classifyTransaction(tx, map[string][]coverageLoad{"001": {base, bad}}, nil, &now, now)
			if f == nil || f.Status != "data_issue" || f.RelatedLoad == nil {
				t.Fatalf("invalid interval: %+v", f)
			}
		}
	})
	t.Run("appointment fallback and canceled loads", func(t *testing.T) {
		l := makeCoverageLoad(2, "L2", "001", "driver", "", "", "booked", nil, nil, date("2026-09-16"), date("2026-09-17"), now)
		if l.problem || !l.AppointmentFallback || l.PickupDate != "2026-09-16" {
			t.Fatalf("appointment UTC date mapping: %+v", l)
		}
		if assessLoadCoverage(tx.date, []coverageLoad{l}).matched == nil {
			t.Fatal("appointment load did not cover")
		}
		l.excluded = true
		if assessLoadCoverage(tx.date, []coverageLoad{l}).matched != nil {
			t.Fatal("canceled load covered")
		}
	})
}
