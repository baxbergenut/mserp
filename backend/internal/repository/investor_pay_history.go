package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

type InvestorStatementWeek struct {
	WeekStart string            `json:"weekStart"`
	Trucks    []DriverPayDriver `json:"trucks"`
}

// Historical summaries use the same read-only calculation and frozen reports as
// Investor Pay. Current ownership is never used to relabel an older statement.
func (r *DriverPayRepository) InvestorHistory(ctx context.Context, owner string, page Pagination) (Page[InvestorStatementWeek], error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Page[InvestorStatementWeek]{}, err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investors WHERE id=$1)`, owner).Scan(&exists); err != nil {
		return Page[InvestorStatementWeek]{}, err
	}
	if !exists {
		return Page[InvestorStatementWeek]{}, pgx.ErrNoRows
	}
	rows, err := tx.Query(ctx, `WITH terms AS (
 SELECT owner_id,week_start,lead(week_start) OVER(PARTITION BY truck_id ORDER BY week_start) AS next_week FROM truck_settlement_terms
 ), weeks AS (
 SELECT generate_series(week_start,least(coalesce(next_week-7,$2::date),$2::date),'7 days')::date AS week FROM terms WHERE owner_id=$1
 UNION SELECT week_start FROM investor_pay_weeks WHERE owner_id=$1
 UNION SELECT $2::date
 ) SELECT week::text FROM weeks ORDER BY week DESC`, owner, ChargeCurrentWeek())
	if err != nil {
		return Page[InvestorStatementWeek]{}, err
	}
	weeks := []string{}
	for rows.Next() {
		var w string
		if err = rows.Scan(&w); err != nil {
			rows.Close()
			return Page[InvestorStatementWeek]{}, err
		}
		weeks = append(weeks, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Page[InvestorStatementWeek]{}, err
	}
	page = page.Normalize(len(weeks))
	result := []InvestorStatementWeek{}
	for _, w := range weeks[page.Offset():min(page.Offset()+page.PageSize, len(weeks))] {
		week, _ := time.Parse(time.DateOnly, w)
		report, e := readInvestorPay(ctx, tx, week)
		if e != nil {
			return Page[InvestorStatementWeek]{}, e
		}
		entry := InvestorStatementWeek{WeekStart: w, Trucks: []DriverPayDriver{}}
		for _, d := range report.Drivers {
			if d.InvestorID == owner {
				entry.Trucks = append(entry.Trucks, d)
			}
		}
		result = append(result, entry)
	}
	return NewPage(result, len(weeks), page), tx.Commit(ctx)
}
