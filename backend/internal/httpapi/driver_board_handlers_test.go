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
	for _, action := range []struct{ method, path string }{{"GET", "/driver-board/events"}, {"GET", "/driver-board/presence"}, {"PUT", "/driver-board/presence"}, {"DELETE", "/driver-board/presence/tab"}} {
		if routePermission(action.method, action.path) != "driver_board.read" {
			t.Fatal("presence requires board visibility", action)
		}
	}
}
