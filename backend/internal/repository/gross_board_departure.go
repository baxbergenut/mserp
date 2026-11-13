package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Keep the departure week's empty assignment tail in the last dispatcher row.
// This is a Gross Board display projection only; dated financial assignments,
// employment history and explicit transfer credits remain authoritative.
func decorateGrossBoardDepartureAssignments(ctx context.Context, tx pgx.Tx, board *GrossBoard) error {
	rows, err := tx.Query(ctx, `SELECT d.id::text,least(d.termination_date,coalesce((departure_dispatcher.unassigned_at AT TIME ZONE 'America/New_York')::date,d.termination_date))::text,
 coalesce(dp.id::text,''),coalesce(dp.full_name,'Unassigned'),coalesce(t.id::text,''),coalesce(t.unit_number,'')
 FROM drivers d `+driverBoardDispatcherJoinSQL+`
 LEFT JOIN dispatchers dp ON dp.id=departure_dispatcher.dispatcher_id
 `+driverBoardTruckJoinSQL+` LEFT JOIN trucks t ON t.id=a.truck_id
 WHERE NOT d.active AND d.termination_date >= $1::date AND d.termination_date < $1::date+7`, board.WeekStart)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, date string
		var last DailyAssignment
		if err = rows.Scan(&id, &date, &last.DispatcherID, &last.DispatcherName, &last.TruckID, &last.TruckUnit); err != nil {
			return err
		}
		for i := range board.Drivers {
			d := &board.Drivers[i]
			if d.ID != id {
				continue
			}
			for j := range d.Assignments {
				a := &d.Assignments[j]
				if a.Date < date {
					continue
				}
				if a.DispatcherID == "" {
					a.DispatcherID = last.DispatcherID
					a.DispatcherName = last.DispatcherName
				}
				if a.TruckID == "" {
					a.TruckID = last.TruckID
					a.TruckUnit = last.TruckUnit
				}
			}
		}
	}
	return rows.Err()
}
