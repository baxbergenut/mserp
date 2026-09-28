package repository

import (
	"strings"
	"testing"
	"time"
)

func TestRelaySuggestionsExplainSharedContacts(t *testing.T) {
	email, phone := "james@example.com", "(470) 334-4443"
	task := RelayIdentityTask{Name: "Burligh James", Email: &email, Phone: &phone}
	drivers := []RelaySuggestion{
		{DriverID: "compatible", Name: "James Lee Burligh", Email: &email, Phone: pointerTo("+14703344443")},
		{DriverID: "shared", Name: "Different Person", Email: &email},
		{DriverID: "unrelated", Name: "Another Driver"},
	}
	result := relaySuggestions(task, drivers)
	if len(result) != 2 || result[0].DriverID != "compatible" || len(result[0].Reasons) != 3 {
		t.Fatalf("ranking: %+v", result)
	}
	if !strings.Contains(strings.Join(result[1].Reasons, " "), "Names differ") {
		t.Fatal("shared contact must be explained")
	}
	task.RejectedDriverIDs = []string{"compatible"}
	result = relaySuggestions(task, drivers)
	if len(result) != 1 || result[0].DriverID != "shared" {
		t.Fatal("rejected suggestion reappeared")
	}
}

func TestUnassignedFuelHasDataQualityHint(t *testing.T) {
	flag := classifyTransaction(coverageTransaction{kind: "fuel", positiveCharge: true, production: true}, nil, nil, nil, time.Now())
	if flag == nil || flag.Status != "data_issue" || !strings.Contains(flag.Reason, "unassigned") {
		t.Fatalf("unexpected flag: %+v", flag)
	}
}
