package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type DailyAssignment struct {
	Date           string `json:"date"`
	DispatcherID   string `json:"dispatcherId"`
	DispatcherName string `json:"dispatcherName"`
	TruckID        string `json:"truckId"`
	TruckUnit      string `json:"truckUnit"`
}

// Resolve each calendar day's final assignment. Known zero-length corrections
// are never evidence; unknown migration baselines retain historical coverage.
func readDailyAssignments(ctx context.Context, tx pgx.Tx, week string, ids []string) (map[string][]DailyAssignment, error) {
	out := map[string][]DailyAssignment{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT d.id::text,day.value::text,
 coalesce(h.dispatcher_id::text,''),coalesce(dp.full_name,h.dispatcher_name,'Unassigned'),
 coalesce(a.truck_id::text,''),coalesce(t.unit_number,'')
 FROM drivers d CROSS JOIN LATERAL (SELECT $1::date+n AS value FROM generate_series(0,6) n) day
 LEFT JOIN LATERAL (
 SELECT dispatcher_id,dispatcher_name FROM driver_dispatcher_assignments h WHERE h.driver_id=d.id
 AND (NOT h.start_known OR h.assigned_at < ((day.value+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at >= ((day.value+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.assigned_at<h.unassigned_at)
 ORDER BY h.assigned_at DESC LIMIT 1
 ) h ON true LEFT JOIN dispatchers dp ON dp.id=h.dispatcher_id
 LEFT JOIN LATERAL (
 SELECT truck_id FROM truck_driver_assignments a WHERE a.driver_id=d.id
 AND a.assigned_at < ((day.value+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at >= ((day.value+1)::timestamp AT TIME ZONE 'America/New_York'))
 AND (a.unassigned_at IS NULL OR a.assigned_at<a.unassigned_at)
 ORDER BY a.assigned_at DESC LIMIT 1
 ) a ON true LEFT JOIN trucks t ON t.id=a.truck_id
 WHERE d.id=ANY($2::uuid[])
 AND (d.roster_start_week IS NULL OR d.roster_start_week<=day.value OR EXISTS (
 SELECT 1 FROM `+grossBoardEntriesSQL+` e WHERE e.driver_id=d.id AND e.service_date=day.value AND NOT e.deleted))
 ORDER BY d.id,day.value`, week, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var a DailyAssignment
		if err = rows.Scan(&id, &a.Date, &a.DispatcherID, &a.DispatcherName, &a.TruckID, &a.TruckUnit); err != nil {
			return nil, err
		}
		out[id] = append(out[id], a)
	}
	return out, rows.Err()
}
