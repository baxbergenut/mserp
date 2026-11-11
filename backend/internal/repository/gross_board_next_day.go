package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type GrossBoardNextDayDestination struct {
	DriverID string `json:"driverId"`
	Date     string `json:"date"`
	Text     string `json:"text"`
}

// Suggestions are derived for dispatch reads only. They never occupy a load
// slot or become planning text that could appear in a pay statement.
func readGrossBoardNextDayDestinations(ctx context.Context, tx pgx.Tx, week time.Time, board GrossBoard) ([]GrossBoardNextDayDestination, error) {
	occupied := make(map[grossBoardDay]bool)
	for _, p := range board.PlanningEntries {
		// A destination tombstone is an explicit dismissal for this day.
		if p.Text != "" || p.Kind == "destination" {
			occupied[grossBoardDay{p.DriverID, p.Date}] = true
		}
	}
	next := make(map[string]string, 6)
	for i := 0; i < 6; i++ {
		next[week.AddDate(0, 0, i).Format(time.DateOnly)] = week.AddDate(0, 0, i+1).Format(time.DateOnly)
	}
	last := make(map[grossBoardDay]GrossBoardEntry)
	for _, e := range board.Entries {
		kind, _ := boardEntryMode(e)
		if kind != "" || e.TransferOut {
			occupied[grossBoardDay{e.DriverID, e.Date}] = true
		}
		// Use the last load on the prior day. If that number is unresolved,
		// an earlier delivery does not establish where the driver finished.
		if day := next[e.Date]; day != "" && kind == "load" && !e.TransferOut && e.LoadNumber != "" {
			key := grossBoardDay{e.DriverID, day}
			if previous, ok := last[key]; !ok || previous.Slot < e.Slot {
				last[key] = e
			}
		}
	}
	candidates := make(map[grossBoardDay]int)
	for day, e := range last {
		if !occupied[day] && e.LoadRecordID != nil && !e.SourceMissing {
			candidates[day] = *e.LoadRecordID
		}
	}
	// Sunday is outside the current snapshot. Look up its last placed load
	// only for drivers whose Monday is empty, with the same exact resolution.
	monday := week.Format(time.DateOnly)
	var mondayDrivers []string
	for _, d := range board.Drivers {
		if !occupied[grossBoardDay{d.ID, monday}] {
			mondayDrivers = append(mondayDrivers, d.ID)
		}
	}
	if len(mondayDrivers) != 0 {
		rows, err := tx.Query(ctx, `
 SELECT e.driver_id::text,matched.id
 FROM (SELECT DISTINCT ON (g.driver_id) g.* FROM `+grossBoardEntriesSQL+` g
       WHERE g.service_date=$1::date-1 AND g.driver_id=ANY($2::uuid[])
         AND NOT g.deleted AND g.day_status='' AND btrim(g.load_number)<>''
         AND NOT EXISTS (SELECT 1 FROM gross_board_transfers t WHERE t.active AND t.driver_id=g.driver_id
                         AND t.source_date=g.service_date AND t.source_slot=g.slot)
       ORDER BY g.driver_id,g.slot DESC) e `+grossBoardResolvedLoad+` WHERE matched.id IS NOT NULL`, week, mondayDrivers)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var driver string
			var id int
			if err = rows.Scan(&driver, &id); err != nil {
				rows.Close()
				return nil, err
			}
			candidates[grossBoardDay{driver, monday}] = id
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	ids := make([]int, 0, len(candidates))
	for _, id := range candidates {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT l.id,l.raw_payload->'stops' FROM loads l WHERE l.id=ANY($1)
 AND NOT EXISTS (SELECT 1 FROM load_source_availability a WHERE a.load_id=l.id AND NOT a.present)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	destinations := make(map[int]string, len(ids))
	for rows.Next() {
		var id int
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		stops := loadStops(raw)
		for i := len(stops) - 1; i >= 0; i-- {
			if stop := stops[i]; stop.Type == "delivery" {
				if stop.City != "" && stop.State != "" {
					destinations[id] = stop.City + ", " + stop.State
				}
				break
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := make([]GrossBoardNextDayDestination, 0, len(candidates))
	for _, d := range board.Drivers {
		for i := 0; i < 7; i++ {
			date := week.AddDate(0, 0, i).Format(time.DateOnly)
			if text := destinations[candidates[grossBoardDay{d.ID, date}]]; text != "" {
				result = append(result, GrossBoardNextDayDestination{DriverID: d.ID, Date: date, Text: text})
			}
		}
	}
	return result, nil
}
