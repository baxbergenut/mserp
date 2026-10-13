package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"mserp/internal/db"
)

// Historical fuel/toll carry only needs classification, source totals, saved
// overrides and load identities for mixed owner/hired-driver routing. It never
// needs load payloads, payment capacity, or the remaining statement columns.
func readDriverPayCostWeek(ctx context.Context, tx pgx.Tx, week time.Time) (DriverPayWeek, error) {
	defer db.Phase(ctx, "cost_source")()
	report := DriverPayWeek{WeekStart: week.Format(time.DateOnly), Drivers: []DriverPayDriver{}}
	rows, err := tx.Query(ctx, driverPayCostsSQL+payDriverFactsSQL+`
 SELECT d.id::text,d.full_name,d.pay_type,d.pay_rate::text,(d.pay_employment_type='owner_operator'),
 coalesce(f.total,0)::text,coalesce(t.total,0)::text,coalesce(w.version,0),w.fuel_override::text,w.toll_override::text
 FROM pay_drivers d LEFT JOIN weekly_fuel f ON f.driver_id=d.id LEFT JOIN weekly_tolls t ON t.driver_id=d.id
 LEFT JOIN driver_pay_weeks w ON w.driver_id=d.id AND w.week_start=$1::date ORDER BY d.full_name,d.id`, week, "", payrollDriverIDs(ctx))
	if err != nil {
		return report, err
	}
	index := map[string]int{}
	for rows.Next() {
		var d DriverPayDriver
		if err = rows.Scan(&d.ID, &d.FullName, &d.PayType, &d.PayRate, &d.IsOwnerOperator, &d.FuelTotal, &d.TollTotal, &d.Edits.Version, &d.Edits.FuelOverride, &d.Edits.TollOverride); err != nil {
			rows.Close()
			return report, err
		}
		d.Edits.DriverID, d.Edits.WeekStart = d.ID, report.WeekStart
		d.Loads = []DriverPayLoad{}
		index[d.ID] = len(report.Drivers)
		report.Drivers = append(report.Drivers, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	rows, err = tx.Query(ctx, `SELECT driver_id::text,service_date::text,slot,load_number FROM `+grossBoardEntriesSQL+` e
 WHERE NOT deleted AND load_number<>'' AND (btrim(load_number)<>'' OR day_status='SHOP') AND service_date>=$1::date AND service_date<$1::date+7
 AND ($2::uuid[] IS NULL OR driver_id=ANY($2::uuid[])) ORDER BY driver_id,service_date,slot`, week, payrollDriverIDs(ctx))
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var l DriverPayLoad
		if err = rows.Scan(&id, &l.Date, &l.Slot, &l.LoadNumber); err != nil {
			return report, err
		}
		if i, ok := index[id]; ok {
			l.CommentKey = payCommentKey(l.Date, l.Slot, l.LoadNumber)
			report.Drivers[i].Loads = append(report.Drivers[i].Loads, l)
		}
	}
	return report, rows.Err()
}
