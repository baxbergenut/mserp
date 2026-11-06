package repository

import "strings"

func hasMultipleBoardPickups(stops []BoardStop) bool {
	found := false
	for _, stop := range stops {
		if strings.EqualFold(stop.Type, "pickup") {
			if found {
				return true
			}
			found = true
		}
	}
	return false
}

// Explicit choices may revisit a stop. Returning to a pickup leaves the
// delivery being left open, while leaving a pickup can complete that pickup.
func applyBoardProgressStop(state *boardLoadState, view BoardLoads, chosen string, dispatched bool) (string, error) {
	if view.Current == nil || chosen == "" {
		return "", ErrBoardLoadSelection
	}
	var target *BoardStop
	for i := range view.Current.Stops {
		stop := &view.Current.Stops[i]
		if stop.Key == chosen && (strings.EqualFold(stop.Type, "pickup") || strings.EqualFold(stop.Type, "delivery")) {
			target = stop
			break
		}
	}
	if target == nil {
		return "", ErrBoardLoadSelection
	}
	status := "DISPATCHED"
	delivery := strings.EqualFold(target.Type, "delivery")
	if delivery {
		status = boardProgressStatus("ENROUTE", view.Next)
	}
	prefix := boardCompletionKey(*view.Current) + "/"
	if chosen != view.StopKey {
		if dispatched {
			completeBoardStop(state, view, "pickup", prefix)
		} else if delivery {
			completeBoardStop(state, view, "delivery", prefix)
		}
	}
	delete(state.CompletedStops, prefix+chosen)
	setBoardProgressStop(state, view.Current, *target)
	return status, nil
}

func completeBoardStop(state *boardLoadState, view BoardLoads, kind, prefix string) {
	if view.StopKey == "" {
		return
	}
	// A single pickup has no separate progress to track.
	if kind == "pickup" && !hasMultipleBoardPickups(view.Current.Stops) {
		return
	}
	for _, stop := range view.Current.Stops {
		if stop.Key == view.StopKey && strings.EqualFold(stop.Type, kind) {
			if state.CompletedStops == nil {
				state.CompletedStops = make(map[string]bool)
			}
			state.CompletedStops[prefix+stop.Key] = true
			return
		}
	}
}

func setBoardProgressStop(state *boardLoadState, load *BoardLoad, stop BoardStop) {
	state.Current, state.StopKey = load, stop.Key
	state.DestinationSource = strings.TrimSpace(stop.Location) != ""
	state.DestinationManual = false
}

// Complete the active stop and select the first unfinished stop of this kind.
func advanceBoardStop(state *boardLoadState, view BoardLoads, kind string, complete bool) bool {
	state.Current = view.Current
	if view.Current == nil {
		useBoardStop(state, kind)
		return false
	}
	if kind == "pickup" && !hasMultipleBoardPickups(view.Current.Stops) {
		return false
	}
	prefix := boardCompletionKey(*view.Current) + "/"
	if complete {
		completeBoardStop(state, view, kind, prefix)
	}
	for _, stop := range view.Current.Stops {
		if strings.EqualFold(stop.Type, kind) && !(complete && stop.Key == view.StopKey) && !state.CompletedStops[prefix+stop.Key] {
			setBoardProgressStop(state, view.Current, stop)
			return true
		}
	}
	state.StopKey = ""
	state.DestinationSource = false
	state.DestinationManual = false
	return false
}
