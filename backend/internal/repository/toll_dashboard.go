package repository

import (
	"context"
	"time"
)

type TollDashboardTotals struct {
	Spend            float64 `json:"spend"`
	TransactionCount int     `json:"transactionCount"`
	TruckCount       int     `json:"truckCount"`
}

type TollDashboardPoint struct {
	Label            string  `json:"label"`
	Spend            float64 `json:"spend"`
	TransactionCount int     `json:"transactionCount"`
}

type TollDashboard struct {
	DateFrom string               `json:"dateFrom"`
	DateTo   string               `json:"dateTo"`
	Totals   TollDashboardTotals  `json:"totals"`
	Monthly  []TollDashboardPoint `json:"monthly"`
	Weekly   []TollDashboardPoint `json:"weekly"`
	Agencies []TollDashboardPoint `json:"agencies"`
	Trucks   []TollDashboardPoint `json:"trucks"`
}

// Aggregate numeric money in PostgreSQL; floats are only used for JSON transport.
// Posting dates match the transaction filters. Source equipment units preserve
// historical identity independently of current fleet assignments.
const tollDashboardSQL = `
WITH filtered AS (
	SELECT posting_date, amount, equipment_unit, agency
	FROM tolls
	WHERE posting_date BETWEEN $1::date AND $2::date
		AND (prepass_environment IS NULL OR prepass_environment = 'production')
), monthly AS (
	SELECT date_trunc('month', posting_date)::date AS period,
		SUM(amount) AS spend, COUNT(*) AS transactions
	FROM filtered GROUP BY 1
), weekly AS (
	SELECT date_trunc('week', posting_date)::date AS period,
		SUM(amount) AS spend, COUNT(*) AS transactions
	FROM filtered GROUP BY 1
), agencies AS (
	SELECT agency AS label, SUM(amount) AS spend, COUNT(*) AS transactions
	FROM filtered GROUP BY agency ORDER BY spend DESC, agency LIMIT 10
), trucks AS (
	SELECT equipment_unit AS label, SUM(amount) AS spend, COUNT(*) AS transactions
	FROM filtered GROUP BY equipment_unit ORDER BY spend DESC, equipment_unit LIMIT 10
)
SELECT 'totals', '', COALESCE(SUM(amount), 0), COUNT(*), COUNT(DISTINCT equipment_unit)
FROM filtered
UNION ALL
SELECT 'monthly', to_char(period, 'YYYY-MM-DD'), COALESCE(spend, 0), COALESCE(transactions, 0), 0
FROM generate_series(date_trunc('month', $1::date)::timestamp,
	date_trunc('month', $2::date)::timestamp, interval '1 month') AS dates(period)
LEFT JOIN monthly USING (period)
UNION ALL
SELECT 'weekly', to_char(period, 'YYYY-MM-DD'), COALESCE(spend, 0), COALESCE(transactions, 0), 0
FROM generate_series(date_trunc('week', $1::date)::timestamp,
	date_trunc('week', $2::date)::timestamp, interval '1 week') AS dates(period)
LEFT JOIN weekly USING (period)
UNION ALL SELECT 'agencies', label, spend, transactions, 0 FROM agencies
UNION ALL SELECT 'trucks', label, spend, transactions, 0 FROM trucks
ORDER BY 1, 2`

func (r *TollRepository) GetDashboard(ctx context.Context, dateFrom, dateTo time.Time) (TollDashboard, error) {
	dashboard := TollDashboard{
		DateFrom: dateFrom.Format(time.DateOnly), DateTo: dateTo.Format(time.DateOnly),
		Monthly: []TollDashboardPoint{}, Weekly: []TollDashboardPoint{},
		Agencies: []TollDashboardPoint{}, Trucks: []TollDashboardPoint{},
	}
	rows, err := r.pool.Query(ctx, tollDashboardSQL, dashboard.DateFrom, dashboard.DateTo)
	if err != nil {
		return TollDashboard{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var section string
		var point TollDashboardPoint
		var truckCount int
		if err := rows.Scan(&section, &point.Label, &point.Spend, &point.TransactionCount, &truckCount); err != nil {
			return TollDashboard{}, err
		}
		switch section {
		case "totals":
			dashboard.Totals = TollDashboardTotals{Spend: point.Spend, TransactionCount: point.TransactionCount, TruckCount: truckCount}
		case "monthly":
			dashboard.Monthly = append(dashboard.Monthly, point)
		case "weekly":
			dashboard.Weekly = append(dashboard.Weekly, point)
		case "agencies":
			dashboard.Agencies = append(dashboard.Agencies, point)
		case "trucks":
			dashboard.Trucks = append(dashboard.Trucks, point)
		}
	}
	return dashboard, rows.Err()
}
