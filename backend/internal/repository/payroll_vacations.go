package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type vacationWeek struct {
	drivers map[string]bool
	trucks  map[string]bool
}

// Fees are weekly, so a partial vacation week retains its ordinary fee. Use
// actual availability transitions in New York calendar dates, never the planned
// return date or today's status to rewrite a historical week. An untracked
// legacy status applies only from the current week; its earlier start is unknown.
func readVacationWeek(ctx context.Context, q chargeQuery, week string) (vacationWeek, error) {
	return readVacationState(ctx, q, week, true)
}

func readVacationState(ctx context.Context, q chargeQuery, week string, includeTrucks bool) (vacationWeek, error) {
	memo := payrollReadMemo(ctx, q)
	if memo != nil && includeTrucks {
		if v, ok := memo.vacations[week]; ok {
			return v, nil
		}
	}
	v := vacationWeek{drivers: map[string]bool{}, trucks: map[string]bool{}}
	query := `WITH status_history AS MATERIALIZED (
 SELECT * FROM fleet_status_history WHERE entity_kind='driver' AND track='availability'
), ` + effectiveFleetStatusHistoryCTE + `, vacation_drivers AS MATERIALIZED (
 SELECT d.id FROM drivers d
 WHERE coalesce((SELECT h.status FROM effective_status_history h
   WHERE h.entity_kind='driver' AND h.entity_id=d.id AND h.track='availability'
   AND h.effective_at < (($1::date+1)::timestamp AT TIME ZONE 'America/New_York')
   ORDER BY h.effective_at DESC,h.id DESC LIMIT 1),
   CASE WHEN $1::date>=date_trunc('week',now() AT TIME ZONE 'America/New_York')::date
   AND NOT EXISTS(SELECT 1 FROM effective_status_history h WHERE h.entity_kind='driver' AND h.entity_id=d.id AND h.track='availability')
   THEN d.availability_status ELSE 'active' END)='vacation'
 AND NOT EXISTS(SELECT 1 FROM effective_status_history h
   WHERE h.entity_kind='driver' AND h.entity_id=d.id AND h.track='availability' AND h.status<>'vacation'
   AND h.effective_at >= (($1::date+1)::timestamp AT TIME ZONE 'America/New_York')
   AND h.effective_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
)`
	if includeTrucks {
		query += `, assignments AS MATERIALIZED (
 SELECT truck_id,driver_id FROM truck_driver_assignments
 WHERE assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York')
 AND (unassigned_at IS NULL OR unassigned_at > ($1::date::timestamp AT TIME ZONE 'America/New_York'))
), operators AS MATERIALIZED (
 SELECT a.truck_id,a.driver_id FROM assignments a
 UNION SELECT a.truck_id,team.team_driver_id FROM assignments a
 JOIN driver_teams team ON team.main_driver_id=a.driver_id
 AND team.start_week<=$1::date AND (team.end_week IS NULL OR team.end_week>$1::date)
), crews AS (
 SELECT o.truck_id,bool_and(v.id IS NOT NULL) on_vacation
 FROM operators o JOIN drivers d ON d.id=o.driver_id
 LEFT JOIN vacation_drivers v ON v.id=d.id
 WHERE d.active OR d.termination_date >= $1::date GROUP BY o.truck_id
)
 SELECT 'driver',id::text FROM vacation_drivers
 UNION ALL SELECT 'truck',t.id::text FROM trucks t ` + truckOwnershipForWeekSQL + `
 LEFT JOIN crews c ON c.truck_id=t.id
 WHERE coalesce(c.on_vacation,i.driver_id IN (SELECT id FROM vacation_drivers))`
	} else {
		query += ` SELECT 'driver',id::text FROM vacation_drivers`
	}
	rows, err := q.Query(ctx, query, week)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		if err = rows.Scan(&kind, &id); err != nil {
			return v, err
		}
		if kind == "driver" {
			v.drivers[id] = true
		} else {
			v.trucks[id] = true
		}
	}
	if err = rows.Err(); err == nil && memo != nil && includeTrucks {
		if memo.vacations == nil {
			memo.vacations = map[string]vacationWeek{}
		}
		memo.vacations[week] = v
	}
	return v, err
}

// Apply after routing, carry and frozen overlays: those paths can recreate idle
// statements. Actual work and immutable settlements remain accessible.
func filterVacationPay(ctx context.Context, tx pgx.Tx, kind string, report *DriverPayWeek) error {
	v, err := readVacationWeek(ctx, tx, report.WeekStart)
	if err != nil {
		return err
	}
	kept := []DriverPayDriver{}
	for _, d := range report.Drivers {
		away := v.drivers[d.ID]
		if kind == "investor" {
			away = v.trucks[d.ID]
		}
		if !away || len(d.Loads) > 0 || len(d.Layovers) > 0 || d.Settlement != nil && d.Settlement.Finalized {
			kept = append(kept, d)
		}
	}
	report.Drivers = kept
	setup := []InvestorTruckSetup{}
	for _, row := range report.SetupRequired {
		if !v.trucks[row.TruckID] {
			setup = append(setup, row)
		}
	}
	report.SetupRequired = setup
	return nil
}
