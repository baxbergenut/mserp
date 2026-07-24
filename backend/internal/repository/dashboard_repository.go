package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DashboardRepository struct {
	pool *pgxpool.Pool
}

func NewDashboardRepository(pool *pgxpool.Pool) *DashboardRepository {
	return &DashboardRepository{pool: pool}
}

type FinancialDashboardQuery struct {
	DateFrom *time.Time
	DateTo   *time.Time
}

type FinancialDashboardPeriod struct {
	Kind     string  `json:"kind"`
	DateFrom *string `json:"dateFrom"`
	DateTo   *string `json:"dateTo"`
}

type FinancialDashboardTotals struct {
	Gross                 float64 `json:"gross"`
	DriverPay             float64 `json:"driverPay"`
	Fuel                  float64 `json:"fuel"`
	Tolls                 float64 `json:"tolls"`
	DeductedFuel          float64 `json:"deductedFuel"`
	DeductedTolls         float64 `json:"deductedTolls"`
	KnownExpenses         float64 `json:"knownExpenses"`
	EstimatedProfit       float64 `json:"estimatedProfit"`
	EstimatedProfitMargin float64 `json:"estimatedProfitMargin"`
	Miles                 float64 `json:"miles"`
	LoadCount             int     `json:"loadCount"`
	RevenuePerMile        float64 `json:"revenuePerMile"`
	UnattributedTolls     float64 `json:"unattributedTolls"`
}

type FinancialExpense struct {
	Category  string   `json:"category"`
	Amount    *float64 `json:"amount"`
	Available bool     `json:"available"`
	Note      string   `json:"note"`
}

type DriverFinancialBreakdown struct {
	DriverID        string   `json:"driverId"`
	DriverName      string   `json:"driverName"`
	IsOwnerOperator bool     `json:"isOwnerOperator"`
	DeductsExpenses bool     `json:"deductsExpenses"`
	PayType         string   `json:"payType"`
	PayRate         float64  `json:"payRate"`
	Gross           float64  `json:"gross"`
	Pay             float64  `json:"pay"`
	Fuel            float64  `json:"fuel"`
	Tolls           float64  `json:"tolls"`
	Miles           float64  `json:"miles"`
	LoadCount       int      `json:"loadCount"`
	LoadNumbers     []string `json:"loadNumbers"`
	RevenuePerMile  float64  `json:"revenuePerMile"`
	Settlement      float64  `json:"settlement"`
	Contribution    float64  `json:"contribution"`
}

type DispatcherFinancialBreakdown struct {
	DispatcherID   *string  `json:"dispatcherId"`
	DispatcherName string   `json:"dispatcherName"`
	Gross          float64  `json:"gross"`
	DriverCount    int      `json:"driverCount"`
	LoadCount      int      `json:"loadCount"`
	Pay            *float64 `json:"pay"`
}

type FinancialDashboardMethodology struct {
	Gross     string `json:"gross"`
	DriverPay string `json:"driverPay"`
	Fuel      string `json:"fuel"`
	Tolls     string `json:"tolls"`
	Profit    string `json:"profit"`
	Week      string `json:"week"`
}

type FinancialDashboard struct {
	Period         FinancialDashboardPeriod       `json:"period"`
	AvailableWeeks []string                       `json:"availableWeeks"`
	Totals         FinancialDashboardTotals       `json:"totals"`
	Expenses       []FinancialExpense             `json:"expenses"`
	Drivers        []DriverFinancialBreakdown     `json:"drivers"`
	Dispatchers    []DispatcherFinancialBreakdown `json:"dispatchers"`
	Methodology    FinancialDashboardMethodology  `json:"methodology"`
}

func (r *DashboardRepository) GetFinancialDashboard(
	ctx context.Context,
	query FinancialDashboardQuery,
) (FinancialDashboard, error) {
	dashboard := FinancialDashboard{
		AvailableWeeks: make([]string, 0),
		Drivers:        make([]DriverFinancialBreakdown, 0),
		Dispatchers:    make([]DispatcherFinancialBreakdown, 0),
		Methodology: FinancialDashboardMethodology{
			Gross:     "Invoiced loads only. Weekly reports include pickups from Monday through Sunday when delivery is no later than the following Monday.",
			DriverPay: "Percentage-based owner-operators receive their gross share, less fuel and toll deductions. CPM owner-operators receive CPM pay with no expense deductions.",
			Fuel:      "Diesel is deducted only from percentage-based owner-operators. CPM owner-operator and company-driver fuel remains a company expense. DEF, other products, cash advances, and fees are excluded.",
			Tolls:     "Tolls are deducted only from percentage-based owner-operators. Other driver tolls remain company expenses. Tolls are attributed using truck history, then a nearby load when needed.",
			Profit:    "Percentage-based owner-operator contribution is the retained gross percentage. CPM owner-operator and company-driver contribution also subtracts pay, diesel, and tolls. Dispatcher pay and maintenance are not included yet.",
			Week:      "Weekly reports run Monday through Sunday in America/New_York.",
		},
	}

	if err := r.loadAvailableWeeks(ctx, &dashboard); err != nil {
		return FinancialDashboard{}, err
	}

	if query.DateFrom == nil {
		var weekStart time.Time
		if len(dashboard.AvailableWeeks) > 0 {
			var err error
			weekStart, err = time.Parse(time.DateOnly, dashboard.AvailableWeeks[0])
			if err != nil {
				return FinancialDashboard{}, err
			}
		} else {
			location, err := time.LoadLocation("America/New_York")
			if err != nil {
				return FinancialDashboard{}, err
			}
			now := time.Now().In(location)
			daysSinceMonday := (int(now.Weekday()) + 6) % 7
			weekStart = time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, location)
		}
		query.DateFrom = &weekStart
	}
	if query.DateTo == nil {
		dateTo := query.DateFrom.AddDate(0, 0, 7)
		query.DateTo = &dateTo
	}

	dateFrom := query.DateFrom.Format(time.DateOnly)
	dateTo := query.DateTo.AddDate(0, 0, -1).Format(time.DateOnly)
	dashboard.Period = FinancialDashboardPeriod{
		Kind: "week", DateFrom: &dateFrom, DateTo: &dateTo,
	}

	if err := r.loadFinancialTotals(ctx, query, &dashboard); err != nil {
		return FinancialDashboard{}, err
	}
	if err := r.loadDriverFinancials(ctx, query, &dashboard); err != nil {
		return FinancialDashboard{}, err
	}
	if err := r.loadDispatcherFinancials(ctx, query, &dashboard); err != nil {
		return FinancialDashboard{}, err
	}

	totals := &dashboard.Totals
	totals.KnownExpenses = totals.DriverPay + totals.Fuel + totals.Tolls
	totals.EstimatedProfit = totals.Gross - totals.KnownExpenses
	if totals.Gross > 0 {
		totals.EstimatedProfitMargin = totals.EstimatedProfit / totals.Gross * 100
	}
	if totals.Miles > 0 {
		totals.RevenuePerMile = totals.Gross / totals.Miles
	}

	driverPay := totals.DriverPay
	fuel := totals.Fuel
	tolls := totals.Tolls
	dashboard.Expenses = []FinancialExpense{
		{Category: "Driver pay / shares", Amount: &driverPay, Available: true, Note: "CPM and company-driver pay plus percentage owner-operator gross shares before deductions."},
		{Category: "Fuel", Amount: &fuel, Available: true, Note: "Company-paid diesel, including CPM owner-operators. Percentage owner-operator fuel reduces their settlement."},
		{Category: "Tolls", Amount: &tolls, Available: true, Note: "Company-paid tolls, including CPM owner-operators. Percentage owner-operator tolls reduce their settlement."},
		{Category: "Maintenance", Available: false, Note: "Maintenance costs are not tracked yet."},
		{Category: "Dispatcher pay", Available: false, Note: "Waiting for the dispatcher pay formula."},
	}

	return dashboard, nil
}

func (r *DashboardRepository) loadAvailableWeeks(
	ctx context.Context,
	dashboard *FinancialDashboard,
) error {
	rows, err := r.pool.Query(ctx, financialDashboardBaseSQL+`
		SELECT DISTINCT date_trunc('week', pickup_date::timestamp)::date AS week_start
		FROM load_events
		WHERE pickup_date IS NOT NULL
		  AND delivery_date IS NOT NULL
		  AND delivery_date <= date_trunc('week', pickup_date::timestamp)::date + 7
		ORDER BY week_start DESC`, nil, nil)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var week time.Time
		if err := rows.Scan(&week); err != nil {
			return err
		}
		dashboard.AvailableWeeks = append(dashboard.AvailableWeeks, week.Format(time.DateOnly))
	}
	return rows.Err()
}

func (r *DashboardRepository) loadFinancialTotals(
	ctx context.Context,
	query FinancialDashboardQuery,
	dashboard *FinancialDashboard,
) error {
	return r.pool.QueryRow(ctx, financialDashboardBaseSQL+`
		SELECT
			COALESCE((SELECT SUM(total_pay) FROM period_loads), 0)::float8,
			COALESCE((
				SELECT SUM(CASE
					WHEN d.pay_type = 'cpm' THEN pl.total_miles * d.pay_rate
					WHEN d.pay_type = 'gross_percentage' THEN pl.total_pay * d.pay_rate / 100
					ELSE 0
				END)
				FROM period_loads pl
				LEFT JOIN drivers d ON d.id = pl.driver_id
			), 0)::float8,
			COALESCE((
				SELECT SUM(pf.spend)
				FROM period_fuel pf
				LEFT JOIN drivers d ON d.id = pf.driver_id
				WHERE NOT COALESCE(d.is_owner_operator AND d.pay_type = 'gross_percentage', false)
			), 0)::float8,
			COALESCE((
				SELECT SUM(pt.amount)
				FROM period_tolls pt
				LEFT JOIN drivers d ON d.id = pt.driver_id
				WHERE NOT COALESCE(d.is_owner_operator AND d.pay_type = 'gross_percentage', false)
			), 0)::float8,
			COALESCE((
				SELECT SUM(pf.spend)
				FROM period_fuel pf
				JOIN drivers d ON d.id = pf.driver_id
				WHERE d.is_owner_operator
				  AND d.pay_type = 'gross_percentage'
			), 0)::float8,
			COALESCE((
				SELECT SUM(pt.amount)
				FROM period_tolls pt
				JOIN drivers d ON d.id = pt.driver_id
				WHERE d.is_owner_operator
				  AND d.pay_type = 'gross_percentage'
			), 0)::float8,
			COALESCE((SELECT SUM(total_miles) FROM period_loads), 0)::float8,
			(SELECT COUNT(*) FROM period_loads)::int,
			COALESCE((SELECT SUM(amount) FROM period_tolls WHERE driver_id IS NULL), 0)::float8`,
		query.DateFrom,
		query.DateTo,
	).Scan(
		&dashboard.Totals.Gross,
		&dashboard.Totals.DriverPay,
		&dashboard.Totals.Fuel,
		&dashboard.Totals.Tolls,
		&dashboard.Totals.DeductedFuel,
		&dashboard.Totals.DeductedTolls,
		&dashboard.Totals.Miles,
		&dashboard.Totals.LoadCount,
		&dashboard.Totals.UnattributedTolls,
	)
}

func (r *DashboardRepository) loadDriverFinancials(
	ctx context.Context,
	query FinancialDashboardQuery,
	dashboard *FinancialDashboard,
) error {
	rows, err := r.pool.Query(ctx, financialDashboardBaseSQL+`
		, load_by_driver AS (
			SELECT driver_id,
				SUM(total_pay) AS gross,
				SUM(total_miles) AS miles,
				COUNT(*)::int AS load_count,
				array_agg(DISTINCT load_id ORDER BY load_id) AS load_numbers
			FROM period_loads
			WHERE driver_id IS NOT NULL
			GROUP BY driver_id
		), fuel_by_driver AS (
			SELECT driver_id, SUM(spend) AS fuel
			FROM period_fuel
			WHERE driver_id IS NOT NULL
			GROUP BY driver_id
		), toll_by_driver AS (
			SELECT driver_id, SUM(amount) AS tolls
			FROM period_tolls
			WHERE driver_id IS NOT NULL
			GROUP BY driver_id
		), driver_ids AS (
			SELECT driver_id FROM load_by_driver
			UNION
			SELECT driver_id FROM fuel_by_driver
			UNION
			SELECT driver_id FROM toll_by_driver
		)
		SELECT d.id, d.full_name, d.is_owner_operator, d.pay_type, d.pay_rate::float8,
			COALESCE(l.gross, 0)::float8,
			CASE
				WHEN d.pay_type = 'cpm' THEN COALESCE(l.miles, 0) * d.pay_rate
				WHEN d.pay_type = 'gross_percentage' THEN COALESCE(l.gross, 0) * d.pay_rate / 100
				ELSE 0
			END::float8 AS pay,
			COALESCE(f.fuel, 0)::float8,
			COALESCE(t.tolls, 0)::float8,
			COALESCE(l.miles, 0)::float8,
			COALESCE(l.load_count, 0)::int,
			COALESCE(l.load_numbers, ARRAY[]::text[])
		FROM driver_ids ids
		JOIN drivers d ON d.id = ids.driver_id
		LEFT JOIN load_by_driver l ON l.driver_id = d.id
		LEFT JOIN fuel_by_driver f ON f.driver_id = d.id
		LEFT JOIN toll_by_driver t ON t.driver_id = d.id
		ORDER BY COALESCE(l.gross, 0) DESC, d.full_name`,
		query.DateFrom,
		query.DateTo,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var driver DriverFinancialBreakdown
		if err := rows.Scan(
			&driver.DriverID,
			&driver.DriverName,
			&driver.IsOwnerOperator,
			&driver.PayType,
			&driver.PayRate,
			&driver.Gross,
			&driver.Pay,
			&driver.Fuel,
			&driver.Tolls,
			&driver.Miles,
			&driver.LoadCount,
			&driver.LoadNumbers,
		); err != nil {
			return err
		}
		if driver.Miles > 0 {
			driver.RevenuePerMile = driver.Gross / driver.Miles
		}
		driver.DeductsExpenses = driver.IsOwnerOperator && driver.PayType == "gross_percentage"
		driver.Settlement, driver.Contribution = driverSettlementAndContribution(
			driver.DeductsExpenses,
			driver.Gross,
			driver.Pay,
			driver.Fuel,
			driver.Tolls,
		)
		dashboard.Drivers = append(dashboard.Drivers, driver)
	}
	return rows.Err()
}

func driverSettlementAndContribution(
	deductsExpenses bool,
	gross float64,
	pay float64,
	fuel float64,
	tolls float64,
) (settlement float64, contribution float64) {
	if deductsExpenses {
		return pay - fuel - tolls, gross - pay
	}
	return pay, gross - pay - fuel - tolls
}

func (r *DashboardRepository) loadDispatcherFinancials(
	ctx context.Context,
	query FinancialDashboardQuery,
	dashboard *FinancialDashboard,
) error {
	rows, err := r.pool.Query(ctx, financialDashboardBaseSQL+`
		SELECT pl.dispatcher_id,
			COALESCE(NULLIF(MAX(pl.dispatcher_name), ''), 'Unassigned') AS dispatcher_name,
			SUM(pl.total_pay)::float8 AS gross,
			COUNT(DISTINCT pl.driver_id)::int AS driver_count,
			COUNT(*)::int AS load_count
		FROM period_loads pl
		GROUP BY pl.dispatcher_id
		ORDER BY gross DESC, dispatcher_name`,
		query.DateFrom,
		query.DateTo,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var dispatcher DispatcherFinancialBreakdown
		if err := rows.Scan(
			&dispatcher.DispatcherID,
			&dispatcher.DispatcherName,
			&dispatcher.Gross,
			&dispatcher.DriverCount,
			&dispatcher.LoadCount,
		); err != nil {
			return err
		}
		dashboard.Dispatchers = append(dashboard.Dispatchers, dispatcher)
	}
	return rows.Err()
}

var financialDashboardBaseSQL = `
WITH load_events AS (
	SELECT l.*,
		(COALESCE(
			l.pickup_time,
			l.pickup_appointment_time
		) AT TIME ZONE 'America/New_York')::date AS pickup_date,
		(COALESCE(
			l.delivery_time,
			l.delivery_appointment_time
		) AT TIME ZONE 'America/New_York')::date AS delivery_date,
		(COALESCE(
			l.delivery_time,
			l.delivery_appointment_time,
			l.pickup_time,
			l.pickup_appointment_time
		) AT TIME ZONE 'America/New_York')::date AS activity_date
	FROM loads l
	WHERE lower(trim(l.status)) = 'invoiced'
	  AND COALESCE(
			l.delivery_time,
			l.delivery_appointment_time,
			l.pickup_time,
			l.pickup_appointment_time
		  ) IS NOT NULL
), fuel_events AS (
	SELECT t.driver_id,
		(t.purchased_at AT TIME ZONE ` + fuelTimezoneExpression("t.timezone") + `)::date AS purchased_on,
		COALESCE(SUM(i.total_amount_paid), 0) AS spend
	FROM fuel_transactions t
	JOIN fuel_transaction_items i ON i.fuel_transaction_id = t.id
	WHERE i.item_kind = 'fuel'
	  AND lower(i.category) <> 'def'
	GROUP BY t.id, t.driver_id, purchased_on
), toll_events AS (
	SELECT t.id, t.amount, t.exit_date,
		COALESCE(assignment.driver_id, nearby_load.driver_id) AS driver_id
	FROM tolls t
	LEFT JOIN LATERAL (
		SELECT a.driver_id
		FROM truck_driver_assignments a
		WHERE a.truck_id = t.truck_id
		  AND (a.assigned_at AT TIME ZONE 'America/New_York')::date <= t.exit_date
		  AND (
			a.unassigned_at IS NULL
			OR t.exit_date < (a.unassigned_at AT TIME ZONE 'America/New_York')::date
		  )
		ORDER BY a.assigned_at DESC
		LIMIT 1
	) assignment ON true
	LEFT JOIN LATERAL (
		SELECT l.driver_id
		FROM load_events l
		WHERE assignment.driver_id IS NULL
		  AND l.driver_id IS NOT NULL
		  AND upper(trim(l.truck_unit)) = upper(trim(t.equipment_unit))
		  AND abs(l.activity_date - t.exit_date) <= 7
		ORDER BY abs(l.activity_date - t.exit_date), l.activity_date DESC
		LIMIT 1
	) nearby_load ON true
), period_loads AS (
	SELECT *
	FROM load_events
	WHERE (
		$1::date IS NULL
		OR (
			pickup_date >= ($1 AT TIME ZONE 'America/New_York')::date
			AND pickup_date < ($2 AT TIME ZONE 'America/New_York')::date
			AND delivery_date <= ($2 AT TIME ZONE 'America/New_York')::date
		)
	  )
), period_fuel AS (
	SELECT *
	FROM fuel_events
	WHERE ($1::date IS NULL OR purchased_on >= ($1 AT TIME ZONE 'America/New_York')::date)
	  AND ($2::date IS NULL OR purchased_on < ($2 AT TIME ZONE 'America/New_York')::date)
), period_tolls AS (
	SELECT *
	FROM toll_events
	WHERE ($1::date IS NULL OR exit_date >= ($1 AT TIME ZONE 'America/New_York')::date)
	  AND ($2::date IS NULL OR exit_date < ($2 AT TIME ZONE 'America/New_York')::date)
)`
