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
