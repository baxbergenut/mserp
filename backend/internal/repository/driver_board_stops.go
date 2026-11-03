package repository

import "strings"

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
