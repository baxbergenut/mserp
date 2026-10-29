package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type PayDayStatus struct {
	Date   string `json:"date"`
	Status string `json:"status"`
}

// Day labels are display metadata, never loads or payable amounts. Resolve the
// truck at the recorded day, rather than using today's operator or assignment.
func decoratePayDayStatuses(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	ids := []string{}
	for _, d := range report.Drivers {
		if d.Settlement == nil || !d.Settlement.Finalized || d.DayStatuses == nil {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	destination := `SELECT driver_id::text id,service_date,day_status FROM routed`
	if kind == "investor" {
		destination = `SELECT assignment.truck_id id,e.service_date,e.day_status FROM routed e
 JOIN LATERAL (
 SELECT min(a.truck_id::text) truck_id FROM truck_driver_assignments a
 WHERE a.driver_id=e.driver_id
 AND a.assigned_at < ((e.service_date+1)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at >= ((e.service_date+1)::timestamp AT TIME ZONE 'America/New_York'))
 HAVING count(DISTINCT a.truck_id)=1
 ) assignment ON true`
	}
	rows, err := tx.Query(ctx, `WITH statuses AS (
 SELECT driver_id,service_date,day_status FROM `+grossBoardEntriesSQL+` e
 WHERE NOT deleted AND day_status<>'' AND service_date >= $1::date AND service_date < $1::date+7
 ), routed AS (
 SELECT coalesce(team.main_driver_id,e.driver_id) driver_id,e.service_date,e.day_status
 FROM statuses e LEFT JOIN driver_teams team ON team.team_driver_id=e.driver_id
 AND team.start_week <= $1::date AND (team.end_week IS NULL OR $1::date < team.end_week)
 ), destinations AS (`+destination+`)
 SELECT DISTINCT id,service_date::text,day_status FROM destinations WHERE id=ANY($2::text[]) ORDER BY 1,2,3`, report.WeekStart, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[string][]PayDayStatus{}
	for rows.Next() {
		var id string
		var status PayDayStatus
		if err = rows.Scan(&id, &status.Date, &status.Status); err != nil {
			return err
		}
		byID[id] = append(byID[id], status)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range report.Drivers {
		d := &report.Drivers[i]
		if d.Settlement != nil && d.Settlement.Finalized && d.DayStatuses != nil {
			continue
		}
		d.DayStatuses = append([]PayDayStatus{}, byID[d.ID]...)
	}
	return nil
}
