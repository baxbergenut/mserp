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
	rows, err := tx.Query(ctx, `WITH days AS (
 SELECT $1::date+n AS value,(($1::date+n+1)::timestamp AT TIME ZONE 'America/New_York') AS cutoff
 FROM generate_series(0,6) n
), dispatcher_days AS MATERIALIZED (
 SELECT DISTINCT ON (h.driver_id,day.value) h.driver_id,day.value,h.dispatcher_id,h.dispatcher_name
 FROM driver_dispatcher_assignments h JOIN days day ON (NOT h.start_known OR h.assigned_at<day.cutoff)
 AND (h.unassigned_at IS NULL OR h.unassigned_at>=day.cutoff)
 WHERE h.driver_id=ANY($2::uuid[]) AND (NOT h.start_known OR h.unassigned_at IS NULL OR h.assigned_at<h.unassigned_at)
 ORDER BY h.driver_id,day.value,h.assigned_at DESC
), truck_days AS MATERIALIZED (
 SELECT DISTINCT ON (a.driver_id,day.value) a.driver_id,day.value,a.truck_id
 FROM truck_driver_assignments a JOIN days day ON a.assigned_at<day.cutoff
 AND (a.unassigned_at IS NULL OR a.unassigned_at>=day.cutoff)
 WHERE a.driver_id=ANY($2::uuid[]) AND (a.unassigned_at IS NULL OR a.assigned_at<a.unassigned_at)
 ORDER BY a.driver_id,day.value,a.assigned_at DESC
)
SELECT d.id::text,day.value::text,
 coalesce(h.dispatcher_id::text,''),coalesce(dp.full_name,h.dispatcher_name,'Unassigned'),
 coalesce(a.truck_id::text,''),coalesce(t.unit_number,'')
 FROM drivers d CROSS JOIN days day
 LEFT JOIN dispatcher_days h ON h.driver_id=d.id AND h.value=day.value
 LEFT JOIN dispatchers dp ON dp.id=h.dispatcher_id
 LEFT JOIN truck_days a ON a.driver_id=d.id AND a.value=day.value
 LEFT JOIN trucks t ON t.id=a.truck_id
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
