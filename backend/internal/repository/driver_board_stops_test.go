package repository

import (
	"errors"
	"testing"
)

func TestBoardDeliveryProgress(t *testing.T) {
	loadID := 42
	view := BoardLoads{Current: &BoardLoad{LoadID: &loadID, Stops: []BoardStop{
		{Key: "pickup", Type: "pickup", Location: "Warehouse"},
		{Key: "first", Type: "delivery", Location: "First delivery"},
		{Key: "second", Type: "delivery"}, // Missing address must not skip a delivery.
		{Key: "third", Type: "delivery", Location: "Third delivery"},
	}}, StopKey: "pickup"}
	state := boardLoadState{}
	if _, err := applyBoardProgressStop(&state, view, "third", true); err != nil || state.StopKey != "third" {
		t.Fatal("could not choose out-of-order delivery", state, err)
	}
	view.StopKey = state.StopKey
	if _, err := applyBoardProgressStop(&state, view, "missing", false); !errors.Is(err, ErrBoardLoadSelection) || len(state.CompletedStops) != 0 {
		t.Fatal("invalid choice changed progress", state, err)
	}
	for _, key := range []string{"first", "second"} {
		if !advanceBoardStop(&state, view, "delivery", true) || state.StopKey != key {
			t.Fatal("skipped remaining delivery", key, state)
		}
		view.StopKey = state.StopKey
	}
	if state.DestinationSource {
		t.Fatal("invented a missing destination")
	}
	if advanceBoardStop(&state, view, "delivery", true) || len(state.CompletedStops) != 3 {
		t.Fatal("did not finish after every delivery", state)
	}
}

func TestBoardProgressStopSelection(t *testing.T) {
	id := 43
	view := BoardLoads{Current: &BoardLoad{LoadID: &id, Stops: []BoardStop{
		{Key: "pu", Type: "pickup", Location: "Pickup address"},
		{Key: "del1", Type: "delivery", Location: "First delivery"},
		{Key: "del2", Type: "delivery"},
	}}, StopKey: "del2", Next: []BoardLoad{{PlanID: "next"}}}
	prefix := boardCompletionKey(*view.Current) + "/"
	state := boardLoadState{Current: view.Current, StopKey: view.StopKey, CompletedStops: map[string]bool{prefix + "del1": true}}
	if _, err := applyBoardProgressStop(&state, view, "unknown", false); !errors.Is(err, ErrBoardLoadSelection) || state.StopKey != "del2" || len(state.CompletedStops) != 1 {
		t.Fatal("invalid selection changed progress", state, err)
	}
	if status, err := applyBoardProgressStop(&state, view, "pu", false); err != nil || status != "DISPATCHED" || state.StopKey != "pu" || !state.DestinationSource || len(state.CompletedStops) != 1 {
		t.Fatal("pickup did not restore Dispatched without completing delivery", state, status, err)
	}
	view.StopKey = "pu"
	if status, err := applyBoardProgressStop(&state, view, "del1", true); err != nil || status != "RESERVED" || len(state.CompletedStops) != 0 {
		t.Fatal("completed delivery could not be revisited", state, status, err)
	}
	view.StopKey = "del1"
	if _, err := applyBoardProgressStop(&state, view, "del1", false); err != nil || len(state.CompletedStops) != 0 {
		t.Fatal("selecting current destination completed it", state, err)
	}
	view.Next = nil
	if status, err := applyBoardProgressStop(&state, view, "del2", false); err != nil || status != "ENROUTE" || !state.CompletedStops[prefix+"del1"] || state.DestinationSource {
		t.Fatal("delivery progression lost receipt or invented address", state, status, err)
	}
}

func TestBoardPickupProgress(t *testing.T) {
	view := BoardLoads{Current: &BoardLoad{PlanID: "multi", Stops: []BoardStop{
		{Key: "first", Type: "pickup", Location: "Atlanta, GA"},
		{Key: "second", Type: "pickup"},
		{Key: "third", Type: "pickup", Location: "Dallas, TX"},
		{Key: "delivery", Type: "delivery", Location: "Miami, FL"},
	}}, StopKey: "first"}
	state := boardLoadState{}
	if status, err := applyBoardProgressStop(&state, view, "third", true); err != nil || status != "DISPATCHED" ||
		state.StopKey != "third" || !state.CompletedStops["plan:multi/first"] {
		t.Fatal("pickup choice did not stay dispatched or record the prior pickup", state, status, err)
	}
	view.StopKey = state.StopKey
	if !advanceBoardStop(&state, view, "pickup", true) || state.StopKey != "second" || state.DestinationSource {
		t.Fatal("skipped an unfinished pickup with a missing address", state)
	}
	view.StopKey = state.StopKey
	if advanceBoardStop(&state, view, "pickup", true) || len(state.CompletedStops) != 3 ||
		!advanceBoardStop(&state, view, "delivery", false) || state.StopKey != "delivery" {
		t.Fatal("did not move to delivery after all pickups", state)
	}
	view.StopKey = state.StopKey
	if status, err := applyBoardProgressStop(&state, view, "first", false); err != nil || status != "DISPATCHED" ||
		state.StopKey != "first" || state.CompletedStops["plan:multi/first"] || len(state.CompletedStops) != 2 {
		t.Fatal("revisiting a pickup did not reopen only that stop", state, status, err)
	}
}

func TestBoardSinglePickupWithoutActiveStopAdvancesToDelivery(t *testing.T) {
	view := BoardLoads{Current: &BoardLoad{Stops: []BoardStop{
		{Key: "pickup", Type: "pickup"},
		{Key: "delivery", Type: "delivery", Location: "Miami, FL"},
	}}}
	state := boardLoadState{}
	if advanceBoardStop(&state, view, "pickup", true) ||
		!advanceBoardStop(&state, view, "delivery", false) || state.StopKey != "delivery" {
		t.Fatal("single pickup with no active stop should advance directly to delivery", state)
	}
}
