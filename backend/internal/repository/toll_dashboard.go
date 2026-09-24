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
	States   []TollDashboardPoint `json:"states"`
	Unmapped []TollDashboardPoint `json:"unmapped"`
}

// Aggregate numeric money in PostgreSQL; floats are only used for JSON transport.
// Posting dates match the transaction filters. Source equipment units preserve
// historical identity independently of current fleet assignments.
// State attribution is an inference from the toll agency, never the billing
// network or plate state. Cross-state operators (RIVE, DRBA, DRPA, DRJTBC,
// PANYNJ, UBP) and unrecognized codes intentionally remain unmapped.
// Agency identities/geography checked against:
// https://e-zpassgroup.org/members
// https://www.prepass.com/wp-content/uploads/2017/09/PrePassApplicationSet-4-27-2016.pdf
// https://www.txdot.gov/discover/toll-roads-managed-lanes/txdot-toll-roads.html
// https://floridasturnpike.com/about/frequently-asked-questions/
// https://www.codot.gov/programs/expresslanes
// https://pikepass.com/pikepass
// https://www.ksturnpike.com/uploads/reports-resources/FY25-Budget-in-Brief-web.pdf
const tollDashboardSQL = `
WITH agency_states(agency, state) AS (
	VALUES ('PTC', 'PA'), ('WVPEDTA', 'WV'), ('ILTOLL', 'IL'), ('CHICAGO', 'IL'),
		('ITRCC', 'IN'), ('OTC', 'OH'), ('OTIC', 'OH'), ('MDTA', 'MD'),
		('NYSTA', 'NY'), ('NYSBA', 'NY'), ('MTAB&T', 'NY'),
		('NJTP', 'NJ'), ('GSP', 'NJ'), ('DELDOT', 'DE'),
		('MASSDOT', 'MA'), ('MASSPIKE', 'MA'), ('META', 'ME'),
		('NHDOT', 'NH'), ('NH', 'NH'), ('NCTA', 'NC'), ('VDOT', 'VA'),
		('RITBA', 'RI'), ('SRTAGA', 'GA'), ('CFX', 'FL'), ('FTE', 'FL'), ('MDX', 'FL'),
		('NTTA', 'TX'), ('HCTRA', 'TX'), ('CTRMA', 'TX'), ('FBCTRA', 'TX'),
		('KTA', 'KS'), ('OTA', 'OK'), ('COEXP', 'CO')
), filtered AS (
	SELECT posting_date, amount, equipment_unit, agency
	FROM tolls
	WHERE posting_date BETWEEN $1::date AND $2::date
		AND (prepass_environment IS NULL OR prepass_environment = 'production')
), geography AS (
	SELECT COALESCE(s.state, '') AS state, f.agency, f.amount
	FROM filtered f LEFT JOIN agency_states s ON s.agency = upper(trim(f.agency))
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
UNION ALL SELECT 'states', state, SUM(amount), COUNT(*), 0 FROM geography WHERE state <> '' GROUP BY state
UNION ALL SELECT 'unmapped', agency, SUM(amount), COUNT(*), 0 FROM geography WHERE state = '' GROUP BY agency
ORDER BY 1, 2`

func (r *TollRepository) GetDashboard(ctx context.Context, dateFrom, dateTo time.Time) (TollDashboard, error) {
	dashboard := TollDashboard{
		DateFrom: dateFrom.Format(time.DateOnly), DateTo: dateTo.Format(time.DateOnly),
		Monthly: []TollDashboardPoint{}, Weekly: []TollDashboardPoint{},
		Agencies: []TollDashboardPoint{}, Trucks: []TollDashboardPoint{},
		States: []TollDashboardPoint{}, Unmapped: []TollDashboardPoint{},
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
		case "states":
			dashboard.States = append(dashboard.States, point)
		case "unmapped":
			dashboard.Unmapped = append(dashboard.Unmapped, point)
		}
	}
	return dashboard, rows.Err()
}
