package repository

import (
	"slices"
	"sort"
)

func boardDispatcherIDs(drivers []DriverBoardDriver) []string {
	ids := []string{}
	for _, d := range drivers {
		if !slices.Contains(ids, d.DispatcherID) {
			ids = append(ids, d.DispatcherID)
		}
	}
	return ids
}

func pinUnassigned(ids []string) []string {
	if i := slices.Index(ids, ""); i > 0 {
		ids = slices.Delete(ids, i, i+1)
		ids = slices.Insert(ids, 0, "")
	}
	return ids
}

// Dispatcher preferences live in user-scoped browser storage. Ignore the retired
// shared order table; preserve source ordering and each group's shared driver order.
func applyBoardDispatcherOrder(board *DriverBoard) {
	ids := pinUnassigned(boardDispatcherIDs(board.Drivers))
	ranks := map[string]int{}
	for i, id := range ids {
		ranks[id] = i
	}
	sort.SliceStable(board.Drivers, func(i, j int) bool {
		return ranks[board.Drivers[i].DispatcherID] < ranks[board.Drivers[j].DispatcherID]
	})
}
