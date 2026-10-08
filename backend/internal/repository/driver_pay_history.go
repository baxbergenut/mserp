package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type DriverPayHistoryRow struct {
	WeekStart string          `json:"weekStart"`
	Driver    DriverPayDriver `json:"driver"`
}

func (r *DriverPayRepository) History(ctx context.Context, driver string, page Pagination) (Page[DriverPayHistoryRow], error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Page[DriverPayHistoryRow]{}, err
	}
	defer tx.Rollback(ctx)
	ctx = payrollReadContext(ctx, tx)
	rows, err := tx.Query(ctx, `WITH weeks AS (
 SELECT date_trunc('week',service_date)::date AS week FROM `+grossBoardEntriesSQL+` e WHERE driver_id=$1 AND NOT deleted AND btrim(load_number)<>''
 UNION SELECT week_start FROM driver_pay_weeks WHERE driver_id=$1
 UNION SELECT p.week_start FROM expense_payments p JOIN expenses e ON e.id=p.expense_id WHERE e.charge_driver_id=$1
 UNION SELECT date_trunc('week',expense_date)::date FROM expenses WHERE charge_driver_id=$1 AND NOT driver_settled
 UNION SELECT generate_series(start_week,least(coalesce(end_week,$2::date),$2::date),'7 days')::date FROM driver_charge_schedules WHERE driver_id=$1
 UNION SELECT p.week_start FROM driver_escrow_payments p JOIN driver_escrows e ON e.id=p.escrow_id WHERE e.driver_id=$1
 UNION SELECT date_trunc('week',start_date)::date FROM driver_escrows WHERE driver_id=$1
 UNION SELECT r.week_start FROM driver_escrow_releases r JOIN driver_escrows e ON e.id=r.escrow_id WHERE e.driver_id=$1 AND NOT r.cancelled
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
	selectedWeeks := weeks[page.Offset():min(page.Offset()+page.PageSize, len(weeks))]
	// Frozen statements are self-contained; opening history should not rebuild
	// source loads, current tariffs or unrelated fleet reports for them.
	frozen := map[string]DriverPayDriver{}
	rows, err = tx.Query(ctx, `SELECT s.week_start::text,s.report,s.version,s.finalized_at,coalesce(u.username,''),s.reopened_at,s.reason FROM payroll_settlements s LEFT JOIN app_users u ON u.id=s.finalized_by WHERE s.driver_id=$1 AND s.finalized AND s.week_start::text=ANY($2::text[])`, driver, selectedWeeks)
	if err != nil {
		return Page[DriverPayHistoryRow]{}, err
	}
	for rows.Next() {
		var w string
		var body []byte
		meta := PayrollSettlement{Finalized: true}
		if err = rows.Scan(&w, &body, &meta.Version, &meta.FinalizedAt, &meta.FinalizedBy, &meta.ReopenedAt, &meta.Reason); err != nil {
			rows.Close()
			return Page[DriverPayHistoryRow]{}, err
		}
		var d DriverPayDriver
		if err = json.Unmarshal(body, &d); err != nil {
			rows.Close()
			return Page[DriverPayHistoryRow]{}, err
		}
		d.Settlement = &meta
		frozen[w] = d
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Page[DriverPayHistoryRow]{}, err
	}
	result := []DriverPayHistoryRow{}
	for _, w := range selectedWeeks {
		if d, ok := frozen[w]; ok {
			result = append(result, DriverPayHistoryRow{w, d})
			continue
		}
		week, _ := time.Parse(time.DateOnly, w)
		report, e := r.cachedWeek(ctx, tx, "driver", week, func() (DriverPayWeek, error) { return readDriverPayWeek(ctx, tx, week, "") })
		if e != nil {
			return Page[DriverPayHistoryRow]{}, e
		}
		for _, d := range report.Drivers {
			if d.ID == driver {
				result = append(result, DriverPayHistoryRow{w, d})
			}
		}
	}
	return NewPage(result, len(weeks), page), tx.Commit(ctx)
}
