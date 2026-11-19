package repository

import (
	"encoding/json"
	"testing"
)

func TestCompactBoardPreservesDifferentSnapshots(t *testing.T) {
	p := BoardLoad{PlanID: "same", Number: "load", Stops: []BoardStop{{Key: "a", Location: "live"}}}
	old := p
	old.Stops = []BoardStop{{Key: "a", Location: "historical"}}
	board := DriverBoard{Loads: map[string]BoardLoads{"driver": {Current: &old, Week: []BoardLoad{p}, Next: []BoardLoad{p}, Earlier: []BoardLoad{}, Hidden: []BoardLoad{}, Unavailable: []BoardLoad{old}, Revision: "revision"}}}
	c := CompactBoard(board)
	v := c.Loads["driver"]
	if len(c.Plans) != 2 || v.Week[0] != v.Next[0] || *v.Current == v.Week[0] || v.Revision != "revision" {
		t.Fatalf("lost queue snapshot: %+v", c)
	}
	data, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	var round struct {
		Loads map[string]struct {
			Current *int
			Week    []int
		}
	}
	if e = json.Unmarshal(data, &round); e != nil {
		t.Fatal(e)
	}
	if round.Loads["driver"].Week[0] != v.Week[0] || *round.Loads["driver"].Current != *v.Current {
		t.Fatal("embedded full arrays leaked into transport")
	}
}

func TestCompactBoardGridGrossEvidence(t *testing.T) {
	board := DriverBoard{GrossEntries: []GrossBoardEntry{{DriverID: "d", Date: "2026-11-19", Slot: 2, LoadNumber: "test", OriginalRate: "100.00", DriverRate: "90.01", Miles: "123.00", Duplicate: true, TripReview: true, Trips: []GrossBoardTrip{{ID: 5}}}}}
	data, err := json.Marshal(CompactBoardGrid(board))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		GrossEntries []map[string]any `json:"grossEntries"`
	}
	if err = json.Unmarshal(data, &result); err != nil || len(result.GrossEntries) != 1 {
		t.Fatal("gross grid entries", err)
	}
	row := result.GrossEntries[0]
	if row["driverId"] != "d" || row["loadNumber"] != "test" || row["driverRate"] != "90.01" || row["duplicate"] != true || row["tripReview"] != true || row["trips"] != nil {
		t.Fatal(row)
	}
}

func TestBoardOverviewPreservesLiveEvidence(t *testing.T) {
	p := BoardLoad{PlanID: "plan", Number: "load", Stops: []BoardStop{{Key: "stop"}}}
	full := DriverBoard{Loads: map[string]BoardLoads{"driver": {Current: &p, Week: []BoardLoad{p}, Next: []BoardLoad{p}, Earlier: []BoardLoad{p}, Hidden: []BoardLoad{p}, Unavailable: []BoardLoad{p}, Revision: "fence", StopKey: "stop"}}}
	overview := BoardOverview(full).Loads["driver"]
	if overview.Current != &p || len(overview.Week) != 1 || len(overview.Next) != 1 || overview.StopKey != "stop" || overview.Revision != "fence" || !overview.DetailsOmitted || len(overview.Hidden)+len(overview.Earlier)+len(overview.Unavailable) != 0 {
		t.Fatal("overview lost live editing evidence", overview)
	}
	if len(full.Loads["driver"].Earlier) != 1 {
		t.Fatal("overview mutated full detail response")
	}
}
