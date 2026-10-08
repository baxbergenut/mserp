package repository

import (
	"encoding/json"
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
