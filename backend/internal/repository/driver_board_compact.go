package repository

import (
	"encoding/json"
	"slices"
	"sort"
)

type CompactBoardLoads struct {
	BoardLoads
	Current     *int  `json:"current"`
	Week        []int `json:"week"`
	Next        []int `json:"next"`
	Earlier     []int `json:"earlier"`
	Hidden      []int `json:"hidden"`
	Unavailable []int `json:"unavailable"`
}
type CompactDriverBoard struct {
	DriverBoard
	Plans []BoardLoad                  `json:"plans"`
	Loads map[string]CompactBoardLoads `json:"loads"`
}

// The Status Board grid uses weekly gross entries for totals and current-load
// suggestions. Full Gross Board editing and historical details have their own API.
type BoardGrossEntry struct {
	DriverID     string `json:"driverId"`
	Date         string `json:"date"`
	Slot         int    `json:"slot"`
	LoadNumber   string `json:"loadNumber"`
	DayStatus    string `json:"dayStatus"`
	OriginalRate string `json:"originalRate"`
	DriverRate   string `json:"driverRate"`
	Miles        string `json:"miles"`
	Duplicate    bool   `json:"duplicate"`
	Deleted      bool   `json:"deleted"`
	TripReview   bool   `json:"tripReview"`
}

type DriverBoardGrid struct {
	CompactDriverBoard
	GrossEntries []BoardGrossEntry `json:"grossEntries"`
}

func CompactBoardGrid(board DriverBoard) DriverBoardGrid {
	return DriverBoardGrid{CompactDriverBoard: CompactBoard(board), GrossEntries: gridGrossEntries(board.GrossEntries)}
}

// Keep plans inline so each live row can be replaced independently of other drivers.
type DriverBoardStreamGrid struct {
	DriverBoard
	GrossEntries []BoardGrossEntry `json:"grossEntries"`
}

func BoardStreamGrid(board DriverBoard) DriverBoardStreamGrid {
	return DriverBoardStreamGrid{DriverBoard: BoardGridOverview(board), GrossEntries: gridGrossEntries(board.GrossEntries)}
}

func gridGrossEntries(entries []GrossBoardEntry) []BoardGrossEntry {
	grid := make([]BoardGrossEntry, len(entries))
	for i, e := range entries {
		grid[i] = BoardGrossEntry{DriverID: e.DriverID, Date: e.Date, Slot: e.Slot, LoadNumber: e.LoadNumber,
			DayStatus: e.DayStatus, OriginalRate: e.OriginalRate, DriverRate: e.DriverRate, Miles: e.Miles,
			Duplicate: e.Duplicate, Deleted: e.Deleted, TripReview: e.TripReview}
	}
	return grid
}

// Deduplicate by complete value, not plan ID: a removed current selection can
// carry a historical stop snapshot that differs from this week's live plan.
func CompactBoard(board DriverBoard) CompactDriverBoard {
	out := CompactDriverBoard{DriverBoard: board, Plans: []BoardLoad{}, Loads: map[string]CompactBoardLoads{}}
	seen := map[string]int{}
	add := func(p BoardLoad) int {
		body, _ := json.Marshal(p)
		key := string(body)
		if i, ok := seen[key]; ok {
			return i
		}
		i := len(out.Plans)
		seen[key] = i
		out.Plans = append(out.Plans, p)
		return i
	}
	list := func(plans []BoardLoad) []int {
		if plans == nil {
			return nil
		}
		ids := make([]int, 0, len(plans))
		for _, p := range plans {
			ids = append(ids, add(p))
		}
		return ids
	}
	keys := make([]string, 0, len(board.Loads))
	for k := range board.Loads {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := board.Loads[k]
		c := CompactBoardLoads{BoardLoads: v, Week: list(v.Week), Next: list(v.Next), Earlier: list(v.Earlier), Hidden: list(v.Hidden), Unavailable: list(v.Unavailable)}
		if v.Current != nil {
			i := add(*v.Current)
			c.Current = &i
		}
		out.Loads[k] = c
	}
	return out
}

// BoardOverview retains all live editing/automation evidence. Historical picker
// groups remain available through the per-driver Loads endpoint.
func BoardOverview(board DriverBoard) DriverBoard {
	loads := make(map[string]BoardLoads, len(board.Loads))
	for id, v := range board.Loads {
		v.Earlier = []BoardLoad{}
		v.Hidden = []BoardLoad{}
		v.Unavailable = []BoardLoad{}
		v.DetailsOmitted = true
		loads[id] = v
	}
	board.Loads = loads
	return board
}

// BoardGridOverview keeps completed weekly load identities for the inline
// number match. Completed choices cannot become a draft's current load.
// Active loads and the detailed Loads drawer keep their stop information.
func BoardGridOverview(board DriverBoard) DriverBoard {
	board = BoardOverview(board)
	for id, loads := range board.Loads {
		loads.Week = slices.Clone(loads.Week)
		for i := range loads.Week {
			if loads.Week[i].Completed {
				loads.Week[i].Stops = []BoardStop{}
			}
		}
		board.Loads[id] = loads
	}
	return board
}
