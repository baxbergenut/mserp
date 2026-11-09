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
	state := newGrossBoardDayModes()
	rows, err := tx.Query(ctx, `SELECT slot,deleted,coalesce(load_number,''),load_record_id,load_trip_id,coalesce(day_status,''),coalesce(original_rate::text,''),coalesce(driver_rate::text,''),coalesce(miles::text,''),coalesce(entered_original_rate::text,''),coalesce(entered_miles::text,''),note FROM `+grossBoardEntriesSQL+` e WHERE driver_id=$1 AND service_date=$2::date`, day.driver, day.date)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var e GrossBoardEntry
		if err = rows.Scan(&e.Slot, &e.Deleted, &e.LoadNumber, &e.LoadRecordID, &e.LoadTripID, &e.DayStatus, &e.OriginalRate, &e.DriverRate, &e.Miles, &e.EnteredOriginalRate, &e.EnteredMiles, &e.Note); err != nil {
			rows.Close()
			return state, err
		}
		state.put(fmt.Sprintf("slot:%d", e.Slot), e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return state, err
	}
	rows, err = tx.Query(ctx, `SELECT kind,text FROM gross_board_planning_entries WHERE driver_id=$1 AND service_date=$2::date AND btrim(text)<>''`, day.driver, day.date)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var kind, text string
		if err = rows.Scan(&kind, &text); err != nil {
			rows.Close()
			return state, err
		}
		state.statuses["planning:"+kind] = text
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return state, err
	}
	// A transferred-in load is protected content too; its source row is elsewhere.
	var transferred bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gross_board_transfers WHERE driver_id=$1 AND dispatch_date=$2::date AND active)`, day.driver, day.date).Scan(&transferred)
	if transferred {
		state.loads["transfer"] = "protected"
	}
	return state, err
}
func validateGrossBoardDayModes(ctx context.Context, tx pgx.Tx, days []grossBoardDay, entries []GrossBoardEntry, planning []GrossBoardPlanningEntry) error {
	for _, day := range days {
		before, err := readGrossBoardDayModes(ctx, tx, day)
		if err != nil {
			return err
		}
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
