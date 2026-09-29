package repository

import (
	"context"
	"time"
)

type DriverPayHistoryRow struct {
	WeekStart string          `json:"weekStart"`
	Driver    DriverPayDriver `json:"driver"`
}

func (r *DriverPayRepository) History(ctx context.Context, driver string, page Pagination) (Page[DriverPayHistoryRow], error) {
	rows, err := r.pool.Query(ctx, `WITH weeks AS (
 SELECT date_trunc('week',service_date)::date AS week FROM `+grossBoardEntriesSQL+` e WHERE driver_id=$1 AND NOT deleted AND btrim(load_number)<>''
 UNION SELECT week_start FROM driver_pay_weeks WHERE driver_id=$1
 UNION SELECT p.week_start FROM expense_payments p JOIN expenses e ON e.id=p.expense_id WHERE e.charge_driver_id=$1
 UNION SELECT date_trunc('week',expense_date)::date FROM expenses WHERE charge_driver_id=$1 AND NOT driver_settled
 UNION SELECT generate_series(start_week,least(coalesce(end_week,$2::date),$2::date),'7 days')::date FROM driver_charge_schedules WHERE driver_id=$1
 UNION SELECT week_start FROM payroll_settlements WHERE driver_id=$1
 ) SELECT week::text FROM weeks WHERE week IS NOT NULL AND week<=$2::date ORDER BY week DESC`, driver, ChargeCurrentWeek())
	if err != nil {
		return Page[DriverPayHistoryRow]{}, err
	}
	weeks := []string{}
	for rows.Next() {
		var week string
		if err = rows.Scan(&week); err != nil {
			rows.Close()
			return Page[DriverPayHistoryRow]{}, err
		}
		weeks = append(weeks, week)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Page[DriverPayHistoryRow]{}, err
	}
	page = page.Normalize(len(weeks))
	result := []DriverPayHistoryRow{}
	for _, w := range weeks[page.Offset():min(page.Offset()+page.PageSize, len(weeks))] {
		week, _ := time.Parse(time.DateOnly, w)
		report, e := r.GetDriverWeek(ctx, week, driver)
		if e != nil {
			return Page[DriverPayHistoryRow]{}, e
		}
		for _, d := range report.Drivers {
			if d.ID == driver {
				result = append(result, DriverPayHistoryRow{w, d})
			}
		}
	}
	return NewPage(result, len(weeks), page), nil
}
