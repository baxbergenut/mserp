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
