package main

import "testing"

func TestOilSource(t *testing.T) {
	s := snapshot{Values: [][]string{{"Truck", "Last Change Date", "Last Change ODO", "Current ODO", "Result", "Type of oil", "Information"}, {"003 ", "10/2/2026", "906,991", "913,523", "6532", "10w 30", "No DEF"}}}
	rows, err := parseSource(s, "2026-10-15")
	if err != nil || len(rows) != 1 || rows[0].Unit != "003" || rows[0].Date != "2026-10-02" || rows[0].Miles != 906991 {
		t.Fatal(rows, err)
	}
	again, _ := parseSource(s, "2026-10-15")
	if rows[0].ID != again[0].ID {
		t.Fatal("import identity is not stable")
	}
	s.Values[1][1] = "10/2/26"
	short, err := parseSource(s, "2026-10-15")
	if err != nil || short[0].ID != rows[0].ID {
		t.Fatal("source's two-digit year", short, err)
	}
	s.Values = append(s.Values, s.Values[1])
	if _, err := parseSource(s, "2026-10-15"); err == nil {
		t.Fatal("duplicate source accepted")
	}
	s.Values = s.Values[:2]
	s.Values[1][1] = "10/16/2026"
	if _, err := parseSource(s, "2026-10-15"); err == nil {
		t.Fatal("future completion accepted")
	}
}
