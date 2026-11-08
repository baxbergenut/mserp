package repository

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Only a proven departure from this exact source record can retire a stale
// Dispatched plan. Use the latest departure, not any historical appearance:
// selecting it again and clearing/replacing it before working it is a correction.
// The audit's before snapshot proves progress before replacement. A clear,
// unmatched replacement, pickup-only selection, or missing evidence proves nothing.
func readPreviouslyActiveBoardLoads(ctx context.Context, tx pgx.Tx, plans map[string][]BoardLoad, manual map[string][]BoardLoad) (map[string]map[int]int64, error) {
	ids, sources := []string{}, []string{}
	seen := map[string]bool{}
	for _, groups := range []map[string][]BoardLoad{plans, manual} {
		for driver, list := range groups {
			for _, p := range list {
				if p.LoadID == nil || !strings.EqualFold(strings.TrimSpace(p.SourceStatus), "dispatched") {
					continue
				}
				source := strconv.Itoa(*p.LoadID)
				key := driver + "/" + source
				if seen[key] {
					continue
				}
				seen[key] = true
				ids, sources = append(ids, driver), append(sources, source)
			}
		}
	}
	result := map[string]map[int]int64{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := tx.Query(ctx, `SELECT d.driver_id,d.id,d.previous,d.following,
 coalesce(d.before_values->>'status',(SELECT h.after_values->>'status' FROM driver_board_history h
 WHERE h.driver_id=d.driver_id AND h.id<d.id AND h.after_values ? 'status' ORDER BY h.id DESC LIMIT 1),''),
 coalesce(d.before_values->>'currentLoad',(SELECT h.after_values->>'currentLoad' FROM driver_board_history h
 WHERE h.driver_id=d.driver_id AND h.id<d.id AND h.after_values ? 'currentLoad' ORDER BY h.id DESC LIMIT 1),'')
 FROM unnest($1::uuid[],$2::text[]) AS candidate(driver_id,source_id)
 JOIN LATERAL (
 SELECT h.driver_id,h.id,h.before_values,
 (h.before_values->>'loadPlan')::jsonb AS previous,
 (h.after_values->>'loadPlan')::jsonb AS following
 FROM driver_board_history h
 WHERE h.driver_id=candidate.driver_id AND h.before_values ? 'loadPlan' AND h.after_values ? 'loadPlan'
 AND (h.before_values->>'loadPlan')::jsonb->'current'->>'loadId'=candidate.source_id
 AND (h.before_values->>'loadPlan')::jsonb->'current'->>'loadId' IS DISTINCT FROM
     (h.after_values->>'loadPlan')::jsonb->'current'->>'loadId'
 ORDER BY h.id DESC LIMIT 1
 ) d ON true`, ids, sources)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var driver, status, number string
		var event int64
		var before, after boardLoadState
		if err = rows.Scan(&driver, &event, &before, &after, &status, &number); err != nil {
			return nil, err
		}
		if !boardPreviouslyActiveDeparture(before, after, status, number) {
			continue
		}
		if result[driver] == nil {
			result[driver] = map[int]int64{}
		}
		result[driver][*before.Current.LoadID] = event
	}
	return result, rows.Err()
}

func boardPreviouslyActiveDeparture(before, after boardLoadState, status, number string) bool {
	if before.Current == nil || before.Current.LoadID == nil || after.Current == nil || after.Current.LoadID == nil ||
		*before.Current.LoadID == *after.Current.LoadID || !strings.EqualFold(strings.TrimSpace(before.Current.Number), strings.TrimSpace(number)) {
		return false
	}
	if status == "ENROUTE" || status == "RESERVED" {
		return true
	}
	if before.DestinationSource && before.StopKey != "" {
		for _, stop := range before.Current.Stops {
			if stop.Key == before.StopKey && strings.EqualFold(stop.Type, "delivery") {
				return true
			}
		}
	}
	return false
}

func annotatePreviouslyActive(p *BoardLoad, events map[int]int64, state boardLoadState) {
	if p.LoadID == nil || !strings.EqualFold(strings.TrimSpace(p.SourceStatus), "dispatched") {
		return
	}
	event := events[*p.LoadID]
	if event == 0 || state.RequeuedLoads[boardCompletionKey(*p)] == event {
		return
	}
	p.PreviouslyActive, p.previousActiveEvent = true, event
}
