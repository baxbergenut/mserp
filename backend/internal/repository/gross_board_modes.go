package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type grossBoardDay struct{ driver, date string }
type grossBoardDayModes struct{ loads, statuses map[string]string }

func newGrossBoardDayModes() grossBoardDayModes {
	return grossBoardDayModes{map[string]string{}, map[string]string{}}
}
func boardEntryMode(e GrossBoardEntry) (string, string) {
	if e.Deleted {
		return "", ""
	}
	if e.DayStatus != "" {
		return "status", e.DayStatus
	}
	if strings.TrimSpace(e.LoadNumber) != "" || e.LoadRecordID != nil || e.OriginalRate != "" || e.DriverRate != "" || e.Miles != "" || e.EnteredOriginalRate != "" || e.EnteredMiles != "" || (e.Note != nil && strings.TrimSpace(*e.Note) != "") {
		id, trip := "", ""
		if e.LoadRecordID != nil {
			id = fmt.Sprint(*e.LoadRecordID)
		}
		if e.LoadTripID != nil {
			trip = fmt.Sprint(*e.LoadTripID)
		}
		return "load", strings.TrimSpace(e.LoadNumber) + ":" + id + ":" + trip
	}
	return "", ""
}
func (s grossBoardDayModes) put(key string, e GrossBoardEntry) {
	delete(s.loads, key)
	delete(s.statuses, key)
	kind, identity := boardEntryMode(e)
	if kind == "load" {
		s.loads[key] = identity
	}
	if kind == "status" {
		s.statuses[key] = identity
	}
}
func (s grossBoardDayModes) clone() grossBoardDayModes {
	n := newGrossBoardDayModes()
	for k, v := range s.loads {
		n.loads[k] = v
	}
	for k, v := range s.statuses {
		n.statuses[k] = v
	}
	return n
}

// Existing mixed historical days remain editable only without adding/replacing
// either conflicting identity. Clearing either side always remains possible.
func (s grossBoardDayModes) mixed() bool {
	if len(s.loads) > 0 && len(s.statuses) > 0 {
		return true
	}
	fixed, planning := false, false
	for key := range s.statuses {
		if strings.HasPrefix(key, "planning:") {
			planning = true
		} else {
			fixed = true
		}
	}
	return fixed && planning
}

func validGrossBoardModeChange(before, after grossBoardDayModes) bool {
	if !after.mixed() {
		return true
	}
	if !before.mixed() {
		return false
	}
	for k, v := range after.loads {
		if prior, ok := before.loads[k]; !ok || prior != v {
			return false
		}
	}
	for k, v := range after.statuses {
		if prior, ok := before.statuses[k]; !ok || prior != v {
			return false
		}
	}
	return true
}
func grossBoardDays(entries []GrossBoardEntry, planning []GrossBoardPlanningEntry) []grossBoardDay {
	set := map[grossBoardDay]bool{}
	for _, e := range entries {
		set[grossBoardDay{e.DriverID, e.Date}] = true
		if e.SplitReview != nil {
			for _, companion := range e.SplitReview.Entries {
				set[grossBoardDay{companion.Entry.DriverID, companion.Entry.Date}] = true
			}
		}
	}
	for _, e := range planning {
		set[grossBoardDay{e.DriverID, e.Date}] = true
	}
	days := make([]grossBoardDay, 0, len(set))
	for day := range set {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].driver+days[i].date < days[j].driver+days[j].date })
	return days
}
func lockGrossBoardDays(ctx context.Context, tx pgx.Tx, days []grossBoardDay) error {
	for _, day := range days {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('gross-board-day-mode:' || $1 || ':' || $2, 0))`, day.driver, day.date); err != nil {
			return err
		}
	}
	return nil
}
func readGrossBoardDayModes(ctx context.Context, tx pgx.Tx, day grossBoardDay) (grossBoardDayModes, error) {
	states, err := readGrossBoardModeStates(ctx, tx, []grossBoardDay{day})
	if err != nil {
		return grossBoardDayModes{}, err
	}
	return states[0], nil
}

// Use one snapshot for all requested days, including days with only planning
// or transferred-in loads. The existing day locks fence the subsequent writes.
func readGrossBoardModeStates(ctx context.Context, tx pgx.Tx, days []grossBoardDay) ([]grossBoardDayModes, error) {
	states := make([]grossBoardDayModes, len(days))
	if len(days) == 0 {
		return states, nil
	}
	drivers, dates := make([]string, len(days)), make([]string, len(days))
	for i, day := range days {
		drivers[i], dates[i] = day.driver, day.date
		states[i] = newGrossBoardDayModes()
	}
	rows, err := tx.Query(ctx, `WITH days AS (
 SELECT ordinal,driver::uuid AS driver_id,day::date AS service_date
 FROM unnest($1::text[], $2::text[]) WITH ORDINALITY AS input(driver,day,ordinal)
), evidence AS MATERIALIZED (
 SELECT d.*,
   (SELECT jsonb_object_agg(p.kind,p.text) FROM gross_board_planning_entries p
    WHERE p.driver_id=d.driver_id AND p.service_date=d.service_date AND btrim(p.text)<>'') AS planning,
   EXISTS(SELECT 1 FROM gross_board_transfers t
          WHERE t.driver_id=d.driver_id AND t.dispatch_date=d.service_date AND t.active) AS transferred
 FROM days d
)
SELECT d.ordinal,coalesce(e.slot,-1),coalesce(e.deleted,false),coalesce(e.load_number,''),e.load_record_id,e.load_trip_id,
 coalesce(e.day_status,''),coalesce(e.original_rate::text,''),coalesce(e.driver_rate::text,''),coalesce(e.miles::text,''),
 coalesce(e.entered_original_rate::text,''),coalesce(e.entered_miles::text,''),e.note,d.planning,d.transferred
FROM evidence d LEFT JOIN LATERAL (
 SELECT e.* FROM `+grossBoardEntriesSQL+` e
 WHERE e.driver_id=d.driver_id AND e.service_date=d.service_date OFFSET 0
) e ON true`, drivers, dates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ordinal int
		var e GrossBoardEntry
		var planning map[string]string
		var transferred bool
		if err = rows.Scan(&ordinal, &e.Slot, &e.Deleted, &e.LoadNumber, &e.LoadRecordID, &e.LoadTripID, &e.DayStatus,
			&e.OriginalRate, &e.DriverRate, &e.Miles, &e.EnteredOriginalRate, &e.EnteredMiles, &e.Note, &planning, &transferred); err != nil {
			return nil, err
		}
		state := states[ordinal-1]
		if e.Slot >= 0 {
			state.put(fmt.Sprintf("slot:%d", e.Slot), e)
		}
		for kind, text := range planning {
			state.statuses["planning:"+kind] = text
		}
		if transferred {
			state.loads["transfer"] = "protected"
		}
	}
	return states, rows.Err()
}
func validateGrossBoardDayModes(ctx context.Context, tx pgx.Tx, days []grossBoardDay, entries []GrossBoardEntry, planning []GrossBoardPlanningEntry) error {
	states, err := readGrossBoardModeStates(ctx, tx, days)
	if err != nil {
		return err
	}
	for i, day := range days {
		before := states[i]
		after := before.clone()
		for _, e := range entries {
			if e.DriverID == day.driver && e.Date == day.date {
				after.put(fmt.Sprintf("slot:%d", e.Slot), e)
			}
		}
		for _, e := range planning {
			if e.DriverID == day.driver && e.Date == day.date {
				key := "planning:" + e.Kind
				delete(after.statuses, key)
				if strings.TrimSpace(e.Text) != "" {
					after.statuses[key] = strings.TrimSpace(e.Text)
				}
			}
		}
		if !validGrossBoardModeChange(before, after) {
			return chargeInvalid("A day can contain loads or a status, not both. Clear the existing loads or status on %s before switching.", day.date)
		}
	}
	return nil
}
