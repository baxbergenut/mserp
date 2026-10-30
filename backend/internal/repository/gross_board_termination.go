package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// The termination date may still contain work. Only the following days in that
// same week receive an empty-day default; no stored plans or versions change.
// Share the projection with payroll's display-only day labels.
const terminationDaysSQL = `SELECT d.id driver_id, day::date service_date
 FROM drivers d
 CROSS JOIN LATERAL generate_series(d.termination_date+1,$1::date+6,interval '1 day') day
 WHERE NOT d.active AND d.termination_date >= $1::date AND d.termination_date < $1::date+7
 AND NOT EXISTS (SELECT 1 FROM ` + grossBoardEntriesSQL + ` e
 WHERE e.driver_id=d.id AND e.service_date=day::date AND NOT e.deleted
 AND (btrim(e.load_number)<>'' OR e.load_record_id IS NOT NULL OR e.day_status<>''
 OR e.original_rate IS NOT NULL OR e.driver_rate IS NOT NULL OR e.miles IS NOT NULL
 OR e.entered_original_rate IS NOT NULL OR e.entered_miles IS NOT NULL OR coalesce(e.note,'')<>''))
 AND NOT EXISTS (SELECT 1 FROM gross_board_transfers moved WHERE moved.driver_id=d.id AND moved.active
 AND (moved.source_date=day::date OR moved.dispatch_date=day::date OR moved.settlement_date=day::date))`

func decorateGrossBoardTerminations(ctx context.Context, tx pgx.Tx, board *GrossBoard) error {
	rows, err := tx.Query(ctx, `SELECT driver_id::text,service_date::text FROM (`+terminationDaysSQL+`) days`, board.WeekStart)
	if err != nil {
		return err
	}
	defer rows.Close()
	visible := map[string]bool{}
	for _, d := range board.Drivers {
		visible[d.ID] = true
	}
	for rows.Next() {
		var id, date string
		if err := rows.Scan(&id, &date); err != nil {
			return err
		}
		if !visible[id] {
			continue
		}
		found := false
		for i := range board.Entries {
			e := &board.Entries[i]
			if e.DriverID == id && e.Date == date && e.Slot == 0 {
				e.DayStatus = "LEFT"
				found = true
				break
			}
		}
		if !found {
			board.Entries = append(board.Entries, GrossBoardEntry{DriverID: id, Date: date, SourceDate: date, DayStatus: "LEFT", Trips: []GrossBoardTrip{}})
		}
	}
	return rows.Err()
}
