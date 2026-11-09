package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// GrossBoardPlanningEntry is dispatch-only free text, never a load or a payroll
// source. Empty text is a versioned tombstone, retained for safe recreation.
type GrossBoardPlanningEntry struct {
	DriverID string `json:"driverId"`
	Date     string `json:"date"`
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Version  int    `json:"version"`
}

// SavePlanningChanges does not enter the financial source gate or touch load
// slots. Shared day-mode locks prevent races with load saves; the unique row
// lock then serializes absent-row creation as well as updates.
// Unchanged retries return the current version without a write/notification.
func (r *GrossBoardRepository) SavePlanningChanges(ctx context.Context, entries []GrossBoardPlanningEntry, actor string) ([]GrossBoardPlanningEntry, error) {
	entries = append([]GrossBoardPlanningEntry{}, entries...)
	for i := range entries {
		if err := ValidatePlanningEntry(&entries[i]); err != nil {
			return nil, err
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	days := grossBoardDays(nil, entries)
	if err = lockGrossBoardDays(ctx, tx, days); err != nil {
		return nil, err
	}
	if err = validateGrossBoardDayModes(ctx, tx, days, nil, entries); err != nil {
		return nil, err
	}
	order := make([]int, len(entries))
	result := append([]GrossBoardPlanningEntry{}, entries...)
	for i := range entries {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := entries[order[i]], entries[order[j]]
		return a.DriverID+a.Date+a.Kind < b.DriverID+b.Date+b.Kind
	})
	for _, i := range order {
		e := entries[i]
		// DO NOTHING is intentionally separate from SELECT FOR UPDATE: under
		// READ COMMITTED an insert racing another creator must read its commit.
		_, err = tx.Exec(ctx, `INSERT INTO gross_board_planning_entries(driver_id,service_date,kind,text,version,updated_by)
 VALUES($1,$2::date,$3,'',0,NULLIF($4,'')::uuid) ON CONFLICT DO NOTHING`, e.DriverID, e.Date, e.Kind, actor)
		if err != nil {
			return nil, err
		}
		var current GrossBoardPlanningEntry
		err = tx.QueryRow(ctx, `SELECT driver_id::text,service_date::text,kind,text,version
 FROM gross_board_planning_entries WHERE driver_id=$1 AND service_date=$2::date AND kind=$3 FOR UPDATE`, e.DriverID, e.Date, e.Kind).
			Scan(&current.DriverID, &current.Date, &current.Kind, &current.Text, &current.Version)
		if err != nil {
			return nil, err
		}
		if current.Version != e.Version && (current.Text != e.Text || e.Version > current.Version) {
			return nil, ErrGrossBoardConflict
		}
		if current.Text != e.Text {
			err = tx.QueryRow(ctx, `UPDATE gross_board_planning_entries SET text=$4,version=version+1,updated_at=now(),updated_by=NULLIF($5,'')::uuid
 WHERE driver_id=$1 AND service_date=$2::date AND kind=$3 RETURNING version`, e.DriverID, e.Date, e.Kind, e.Text, actor).Scan(&current.Version)
			if err != nil {
				return nil, err
			}
			current.Text = e.Text
		}
		result[i] = current
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func readGrossBoardPlanning(ctx context.Context, tx pgx.Tx, week time.Time) ([]GrossBoardPlanningEntry, error) {
	entries := []GrossBoardPlanningEntry{}
	rows, err := tx.Query(ctx, `SELECT driver_id::text,service_date::text,kind,text,version
 FROM gross_board_planning_entries WHERE service_date >= $1::date AND service_date < $1::date+7
 ORDER BY driver_id,service_date,kind`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry GrossBoardPlanningEntry
		if err = rows.Scan(&entry.DriverID, &entry.Date, &entry.Kind, &entry.Text, &entry.Version); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// ValidatePlanningEntry also protects non-HTTP callers from mixing statuses or
// load fields into this store. Week/UUID/batch checks belong to the HTTP boundary.
func ValidatePlanningEntry(entry *GrossBoardPlanningEntry) error {
	if entry.Kind != "note" && entry.Kind != "destination" {
		return errors.New("planning kind must be note or destination")
	}
	entry.Text = strings.TrimSpace(entry.Text)
	if len([]rune(entry.Text)) > 1000 || strings.ContainsRune(entry.Text, '\x00') {
		return errors.New("planning text must be 1000 characters or fewer and cannot contain null characters")
	}
	if entry.Version < 0 || entry.Version > 2147483646 {
		return errors.New("invalid planning entry version")
	}
	return nil
}
