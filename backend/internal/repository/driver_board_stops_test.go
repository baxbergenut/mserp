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
	if _, err := advanceBoardDelivery(&state, view, "pickup", false); !errors.Is(err, ErrBoardLoadSelection) {
		t.Fatal("accepted pickup as next delivery", err)
	}
	if more, err := advanceBoardDelivery(&state, view, "third", false); err != nil || !more || state.StopKey != "third" {
		t.Fatal("could not choose out-of-order delivery", state, err)
	}
	view.StopKey = state.StopKey
	if _, err := advanceBoardDelivery(&state, view, "missing", true); !errors.Is(err, ErrBoardLoadSelection) || len(state.CompletedStops) != 0 {
		t.Fatal("invalid choice changed progress", state, err)
	}
	for _, key := range []string{"first", "second"} {
		more, err := advanceBoardDelivery(&state, view, "", true)
		if err != nil || !more || state.StopKey != key {
			t.Fatal("skipped remaining delivery", key, state, err)
		}
		view.StopKey = state.StopKey
	}
	if state.DestinationSource {
		t.Fatal("invented a missing destination")
	}
	if more, err := advanceBoardDelivery(&state, view, "", true); err != nil || more || len(state.CompletedStops) != 3 {
		t.Fatal("did not finish after every delivery", state, err)
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
	if _, err := applyBoardProgressStop(&state, view, "unknown", true); !errors.Is(err, ErrBoardLoadSelection) || state.StopKey != "del2" || len(state.CompletedStops) != 1 {
		t.Fatal("invalid selection changed progress", state, err)
	}
	if status, err := applyBoardProgressStop(&state, view, "pu", true); err != nil || status != "DISPATCHED" || state.StopKey != "pu" || !state.DestinationSource || len(state.CompletedStops) != 1 {
		t.Fatal("pickup did not restore Dispatched without completing delivery", state, status, err)
	}
	view.StopKey = "pu"
	if status, err := applyBoardProgressStop(&state, view, "del1", false); err != nil || status != "RESERVED" || len(state.CompletedStops) != 0 {
		t.Fatal("completed delivery could not be revisited", state, status, err)
	}
	view.StopKey = "del1"
	if _, err := applyBoardProgressStop(&state, view, "del1", true); err != nil || len(state.CompletedStops) != 0 {
		t.Fatal("selecting current destination completed it", state, err)
	}
	view.Next = nil
	if status, err := applyBoardProgressStop(&state, view, "del2", true); err != nil || status != "ENROUTE" || !state.CompletedStops[prefix+"del1"] || state.DestinationSource {
		t.Fatal("delivery progression lost receipt or invented address", state, status, err)
	}
}
