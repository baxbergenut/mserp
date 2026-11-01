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
	var report DriverPayWeek
	var err error
	facts, _ := ctx.Value(payrollRoutingFactsKey{}).(*payrollRoutingFactsState)
	if facts != nil && payrollNormalizedInputs(ctx) {
		report, err = readPreparedDriverFact(ctx, tx, week, facts, "driver_cost_base", func() (DriverPayWeek, error) {
			return readDriverPayCostBaseWeek(ctx, tx, week, true)
		})
		if err != nil {
			return report, err
		}
		rows, err := tx.Query(ctx, `SELECT driver_id::text,version,fuel_override::text,toll_override::text FROM driver_pay_weeks WHERE week_start=$1 AND ($2::uuid[] IS NULL OR driver_id=ANY($2::uuid[]))`, week, payrollDriverIDs(ctx))
		if err != nil {
			return report, err
		}
		edits := map[string]DriverPayEdits{}
		for rows.Next() {
			var e DriverPayEdits
			if err = rows.Scan(&e.DriverID, &e.Version, &e.FuelOverride, &e.TollOverride); err != nil {
				rows.Close()
				return report, err
			}
			e.WeekStart = report.WeekStart
			edits[e.DriverID] = e
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return report, err
		}
		for i, d := range report.Drivers {
			if e, ok := edits[d.ID]; ok {
				report.Drivers[i].Edits = e
			}
		}
	} else {
		report, err = readDriverPayCostBaseWeek(ctx, tx, week, false)
		if err != nil {
			return report, err
		}
	}
	if payrollCostDetails(ctx) {
		index := map[string]int{}
		for i, d := range report.Drivers {
			index[d.ID] = i
		}
		items, e := readNativeCostTransactions(ctx, tx, report.WeekStart)
		if e != nil {
			return report, e
		}
		for _, item := range items {
			if i, ok := index[item.driverID]; ok && item.driverAttributed {
				report.Drivers[i].costTransactions = append(report.Drivers[i].costTransactions, item)
			}
		}
	}
	return report, nil
}

func readDriverPayCostBaseWeek(ctx context.Context, tx pgx.Tx, week time.Time, facts bool) (DriverPayWeek, error) {
	report := DriverPayWeek{WeekStart: week.Format(time.DateOnly), Drivers: []DriverPayDriver{}}
	editsTable := "driver_pay_weeks"
	if facts {
		editsTable = `(SELECT null::uuid driver_id,null::date week_start,null::int version,null::numeric fuel_override,null::numeric toll_override WHERE false)`
	}
	rows, err := tx.Query(ctx, driverPayCostsSQL+payDriverFactsSQL+`
 SELECT d.id::text,d.full_name,d.pay_type,d.pay_rate::text,(d.pay_employment_type='owner_operator'),
 coalesce(f.total,0)::text,coalesce(t.total,0)::text,coalesce(w.version,0),w.fuel_override::text,w.toll_override::text
 FROM pay_drivers d LEFT JOIN weekly_fuel f ON f.driver_id=d.id LEFT JOIN weekly_tolls t ON t.driver_id=d.id
 LEFT JOIN `+editsTable+` w ON w.driver_id=d.id AND w.week_start=$1::date ORDER BY d.full_name,d.id`, week, "", payrollDriverIDs(ctx))
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
	rows, err = tx.Query(ctx, `SELECT driver_id::text,service_date::text,slot,load_number FROM `+payrollBoardEntriesSQL+` e
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
	if err = rows.Err(); err != nil {
		return report, err
	}
	rows.Close()
	return report, nil
}
