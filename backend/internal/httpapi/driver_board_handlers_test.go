package httpapi

import (
	"mserp/internal/repository"
	"strings"
	"testing"
)

func TestDriverBoardValidation(t *testing.T) {
	good := repository.DriverBoardEntry{DriverID: "00000000-0000-0000-0000-000000000001", Status: "ENROUTE"}
	for _, tc := range []struct {
		name    string
		entries []repository.DriverBoardEntry
		valid   bool
	}{
		{"valid", []repository.DriverBoardEntry{good}, true}, {"empty", nil, false},
		{"duplicate", []repository.DriverBoardEntry{good, good}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := driverBoardRequest{Entries: tc.entries}
			if (request.validate() == nil) != tc.valid {
				t.Fatal("unexpected validation")
			}
		})
	}
	for _, mutate := range []func(*repository.DriverBoardEntry){
		func(e *repository.DriverBoardEntry) { e.Status = "unknown" }, func(e *repository.DriverBoardEntry) { e.Version = -1 },
		func(e *repository.DriverBoardEntry) { e.DriverID = "invalid" },
		func(e *repository.DriverBoardEntry) { e.Notes = strings.Repeat("a", 5001) }, func(e *repository.DriverBoardEntry) { e.Notes = "nul\x00" },
		func(e *repository.DriverBoardEntry) { e.TrailerNote = strings.Repeat("a", 5001) }, func(e *repository.DriverBoardEntry) { e.TrailerNote = "nul\x00" },
	} {
		entry := good
		mutate(&entry)
		request := driverBoardRequest{Entries: []repository.DriverBoardEntry{entry}}
		if request.validate() == nil {
			t.Errorf("accepted invalid entry %+v", entry)
		}
	}
	good.Notes = "Typing a "
	request := driverBoardRequest{Entries: []repository.DriverBoardEntry{good}}
	if err := request.validate(); err != nil || request.Entries[0].Notes != good.Notes {
		t.Fatal("immediate autosave must preserve typed spaces", err)
	}
	if routePermission("GET", "/driver-board") != "driver_board.read" || routePermission("PUT", "/driver-board") != "driver_board.write" {
		t.Fatal("driver board must have explicit access controls")
	}
	if routePermission("POST", "/driver-board/order") != "driver_board.write" {
		t.Fatal("driver ordering requires board write permission")
	}
	if routePermission("POST", "/driver-board/dispatcher-order") != "driver_board.write" {
		t.Fatal("dispatcher ordering requires board write permission")
	}
	for _, action := range []struct{ method, path string }{{"GET", "/driver-board/events"}, {"GET", "/driver-board/presence"}, {"PUT", "/driver-board/presence"}, {"DELETE", "/driver-board/presence/tab"}} {
		if routePermission(action.method, action.path) != "driver_board.read" {
			t.Fatal("presence requires board visibility", action)
		}
	}
}

func TestDriverBoardSheetStatuses(t *testing.T) {
	for _, status := range []string{"READY", "REST", "COVERED", "DISPATCHED", "ENROUTE", "RESERVED", "VACATION", "DEADHEAD", "HOME", "SHOP", "STOP", "In the YARD", "Hold for Load", "Left", "Family issue", "Issue with PU/Del", "At PU", "Health issue", "At DEL", "HOME (YARD)", "NEW DRIVER", "34 RESET", "ACCIDENT"} {
		request := driverBoardRequest{Entries: []repository.DriverBoardEntry{{DriverID: "00000000-0000-0000-0000-000000000001", Status: status, StatusEdited: true}}}
		if err := request.validate(); err != nil {
			t.Errorf("sheet status %q: %v", status, err)
		}
	}
	for _, status := range []string{"RESET", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "LOAD CANCELLED", "REJECTED"} {
		request := driverBoardRequest{Entries: []repository.DriverBoardEntry{{DriverID: "00000000-0000-0000-0000-000000000001", Status: status, Notes: "unrelated edit"}}}
		if err := request.validate(); err != nil {
			t.Errorf("existing %q blocked unrelated edit: %v", status, err)
		}
		request.Entries[0].StatusEdited = true
		if request.validate() == nil {
			t.Errorf("offered legacy %q as a new driver status", status)
		}
	}
}
