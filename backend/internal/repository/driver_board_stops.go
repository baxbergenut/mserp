package repository

import "strings"

// Explicit choices may return to a pickup or revisit any delivery. Returning to
// pickup does not complete the delivery being left; revisiting a delivery reopens
// that stop while retaining the other load-scoped completion receipts.
func applyBoardProgressStop(state *boardLoadState, view BoardLoads, chosen string, complete bool) (string, error) {
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
	if strings.EqualFold(target.Type, "delivery") {
		status = boardProgressStatus("ENROUTE", view.Next)
		prefix := boardCompletionKey(*view.Current) + "/"
		if complete && chosen != view.StopKey {
			for _, stop := range view.Current.Stops {
				if stop.Key == view.StopKey && strings.EqualFold(stop.Type, "delivery") {
					if state.CompletedStops == nil {
						state.CompletedStops = map[string]bool{}
					}
					state.CompletedStops[prefix+stop.Key] = true
				}
			}
		}
		delete(state.CompletedStops, prefix+chosen)
	}
	state.Current, state.StopKey = view.Current, chosen
	state.DestinationSource = strings.TrimSpace(target.Location) != ""
	state.DestinationManual = false
	return status, nil
}

// Complete only the selected delivery. Stop receipts belong to a source load,
// so a reviewed out-of-order destination never skips the other deliveries.
func advanceBoardDelivery(state *boardLoadState, view BoardLoads, chosen string, complete bool) (bool, error) {
	state.Current = view.Current
	if view.Current == nil {
		if chosen != "" {
			return false, ErrBoardLoadSelection
		}
		useBoardStop(state, "delivery")
		return false, nil
	}
	prefix := boardCompletionKey(*view.Current) + "/"
	remaining := []BoardStop{}
	completed := ""
	for _, stop := range view.Current.Stops {
		if !strings.EqualFold(stop.Type, "delivery") {
			continue
		}
		if complete && stop.Key == view.StopKey {
			completed = prefix + stop.Key
			continue
		}
		if !state.CompletedStops[prefix+stop.Key] {
			remaining = append(remaining, stop)
		}
	}
	var next *BoardStop
	for i := range remaining {
		if chosen == "" || remaining[i].Key == chosen {
			next = &remaining[i]
			break
		}
	}
	if chosen != "" && next == nil {
		return false, ErrBoardLoadSelection
	}
	if completed != "" {
		if state.CompletedStops == nil {
			state.CompletedStops = map[string]bool{}
		}
		state.CompletedStops[completed] = true
	}
	if next == nil {
		useBoardStop(state, "delivery")
		return false, nil
	}
	state.StopKey = next.Key
	state.DestinationSource = strings.TrimSpace(next.Location) != ""
	state.DestinationManual = false
	return true, nil
}
