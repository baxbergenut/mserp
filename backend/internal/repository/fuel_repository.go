package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/relay"
)

type FuelRepository struct {
	pool *pgxpool.Pool
}

func NewFuelRepository(pool *pgxpool.Pool) *FuelRepository {
	return &FuelRepository{pool: pool}
}

type FuelTransaction struct {
	ID                 string    `json:"id"`
	RelayTransactionID string    `json:"relayTransactionId"`
	DriverID           string    `json:"driverId"`
	DriverName         string    `json:"driverName"`
	RelayDriverID      string    `json:"relayDriverId"`
	RelayIntegrationID *string   `json:"relayIntegrationId"`
	PurchasedAt        time.Time `json:"purchasedAt"`
	MerchantName       string    `json:"merchantName"`
	LocationName       string    `json:"locationName"`
	City               string    `json:"city"`
	State              string    `json:"state"`
	Timezone           string    `json:"timezone"`
	TotalAmountPaid    float64   `json:"totalAmountPaid"`
	TotalRetailPrice   float64   `json:"totalRetailPrice"`
	TotalAmountSaved   float64   `json:"totalAmountSaved"`
	CashAdvance        *float64  `json:"cashAdvance"`
	CurrencyCode       string    `json:"currencyCode"`
	FuelAmount         float64   `json:"fuelAmount"`
	DEFAmount          float64   `json:"defAmount"`
	OtherAmount        float64   `json:"otherAmount"`
	FuelVolume         float64   `json:"fuelVolume"`
	DEFVolume          float64   `json:"defVolume"`
	FuelCodeType       *string   `json:"fuelCodeType"`
	IsDirectBill       bool      `json:"isDirectBill"`
}

func (r *FuelRepository) CompletedDays(
	ctx context.Context,
	environment string,
	startDate time.Time,
	endDate time.Time,
) (map[string]struct{}, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sync_date
		FROM relay_fuel_sync_days
		WHERE relay_environment = $1 AND sync_date BETWEEN $2 AND $3`,
		environment,
		startDate.Format(time.DateOnly),
		endDate.Format(time.DateOnly),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	days := make(map[string]struct{})
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			return nil, err
		}
		days[day.Format(time.DateOnly)] = struct{}{}
	}
	return days, rows.Err()
}

func (r *FuelRepository) UpsertDay(
	ctx context.Context,
	environment string,
	day time.Time,
	transactions []relay.Transaction,
	syncedAt time.Time,
	markComplete bool,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, transaction := range transactions {
		if err := upsertFuelTransaction(ctx, tx, environment, transaction, syncedAt); err != nil {
			return fmt.Errorf("upsert Relay transaction %q: %w", transaction.TransactionID, err)
		}
	}

	if markComplete {
		if _, err := tx.Exec(ctx, `
			INSERT INTO relay_fuel_sync_days (
				relay_environment, sync_date, transaction_count, fetched_at
			) VALUES ($1, $2, $3, $4)
			ON CONFLICT (relay_environment, sync_date) DO UPDATE SET
				transaction_count = EXCLUDED.transaction_count,
				fetched_at = EXCLUDED.fetched_at`,
			environment,
			day.Format(time.DateOnly),
			len(transactions),
			syncedAt,
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func upsertFuelTransaction(
	ctx context.Context,
	tx pgx.Tx,
	environment string,
	transaction relay.Transaction,
	syncedAt time.Time,
) error {
	transactionID := strings.TrimSpace(transaction.TransactionID)
	if transactionID == "" {
		return errors.New("missing transaction_id")
	}
	if strings.TrimSpace(transaction.Driver.ID) == "" {
		return errors.New("missing driver.id")
	}

	driverID, err := ensureRelayDriver(ctx, tx, environment, transaction.Driver)
	if err != nil {
		return err
	}

	prompts, err := json.Marshal(transaction.Prompts)
	if err != nil {
		return err
	}
	rawPayload, err := json.Marshal(transaction)
	if err != nil {
		return err
	}

	var fuelPolicyID, fuelPolicyName any
	if transaction.FuelPolicy != nil {
		fuelPolicyID = nullIfBlank(transaction.FuelPolicy.ID)
		fuelPolicyName = nullIfBlank(transaction.FuelPolicy.Name)
	}

	var localTransactionID string
	err = tx.QueryRow(ctx, `
		INSERT INTO fuel_transactions (
			relay_environment, relay_transaction_id, driver_id,
			relay_driver_id, relay_integration_id, purchased_at,
			relay_fuel_code, fuel_code_type, total_amount_paid,
			total_retail_price, total_amount_saved, cash_advance,
			is_direct_bill, currency_code, merchant_id, merchant_name,
			merchant_number, location_id, location_name, merchant_location_id,
			address, city, state, postal_code, latitude, longitude, timezone,
			fuel_policy_id, fuel_policy_name, prompts, raw_payload, synced_at
		) VALUES (
			$1, $2, $3,
			$4, $5, $6,
			$7, $8, $9,
			$10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19, $20,
			$21, $22, $23, $24, $25, $26, $27,
			$28, $29, $30, $31, $32
		)
		ON CONFLICT (relay_environment, relay_transaction_id) DO UPDATE SET
			driver_id = EXCLUDED.driver_id,
			relay_driver_id = EXCLUDED.relay_driver_id,
			relay_integration_id = EXCLUDED.relay_integration_id,
			purchased_at = EXCLUDED.purchased_at,
			relay_fuel_code = EXCLUDED.relay_fuel_code,
			fuel_code_type = EXCLUDED.fuel_code_type,
			total_amount_paid = EXCLUDED.total_amount_paid,
			total_retail_price = EXCLUDED.total_retail_price,
			total_amount_saved = EXCLUDED.total_amount_saved,
			cash_advance = EXCLUDED.cash_advance,
			is_direct_bill = EXCLUDED.is_direct_bill,
			currency_code = EXCLUDED.currency_code,
			merchant_id = EXCLUDED.merchant_id,
			merchant_name = EXCLUDED.merchant_name,
			merchant_number = EXCLUDED.merchant_number,
			location_id = EXCLUDED.location_id,
			location_name = EXCLUDED.location_name,
			merchant_location_id = EXCLUDED.merchant_location_id,
			address = EXCLUDED.address,
			city = EXCLUDED.city,
			state = EXCLUDED.state,
			postal_code = EXCLUDED.postal_code,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			timezone = EXCLUDED.timezone,
			fuel_policy_id = EXCLUDED.fuel_policy_id,
			fuel_policy_name = EXCLUDED.fuel_policy_name,
			prompts = EXCLUDED.prompts,
			raw_payload = EXCLUDED.raw_payload,
			synced_at = EXCLUDED.synced_at
		RETURNING id`,
		environment,
		transactionID,
		driverID,
		strings.TrimSpace(transaction.Driver.ID),
		nullableRelayString(transaction.Driver.IntegrationID),
		transaction.CreatedAt.UTC(),
		nullableRelayString(transaction.RelayFuelCode),
		nullableRelayString(transaction.FuelCodeType),
		amountOrZero(transaction.TotalAmountPaid),
		amountOrZero(transaction.TotalRetailPrice),
		amountOrZero(transaction.TotalAmountSaved),
		nullableRelayString(transaction.CashAdvance),
		transaction.IsDirectBill,
		strings.ToUpper(strings.TrimSpace(transaction.CurrencyCode)),
		strings.TrimSpace(transaction.Merchant.ID),
		strings.TrimSpace(transaction.Merchant.Name),
		strings.TrimSpace(transaction.Merchant.Number),
		strings.TrimSpace(transaction.Location.ID),
		strings.TrimSpace(transaction.Location.Name),
		strings.TrimSpace(transaction.Location.FuelMerchantLocationID),
		strings.TrimSpace(transaction.Location.Address),
		strings.TrimSpace(transaction.Location.City),
		strings.ToUpper(strings.TrimSpace(transaction.Location.State)),
		strings.TrimSpace(transaction.Location.ZipCode),
		transaction.Location.Latitude,
		transaction.Location.Longitude,
		normalizeFuelTimezone(transaction.Location.Timezone),
		fuelPolicyID,
		fuelPolicyName,
		prompts,
		rawPayload,
		syncedAt.UTC(),
	).Scan(&localTransactionID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM fuel_transaction_fees WHERE fuel_transaction_id = $1`, localTransactionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM fuel_transaction_items WHERE fuel_transaction_id = $1`, localTransactionID); err != nil {
		return err
	}

	lineNumber := 0
	for _, item := range transaction.FuelItems {
		if _, err := tx.Exec(ctx, `
			INSERT INTO fuel_transaction_items (
				fuel_transaction_id, line_number, item_kind, category,
				description, product_code, quantity, unit_of_measure,
				retail_price_per_unit, discounted_price_per_unit,
				total_retail_price, total_amount_paid
			) VALUES ($1, $2, 'fuel', $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			localTransactionID,
			lineNumber,
			strings.ToLower(strings.TrimSpace(item.FuelType)),
			nullIfBlank(item.FuelTypeDescription),
			nullIfBlank(item.FuelProductCode),
			nullableAmount(item.Volume),
			nullIfBlank(item.VolumeUOM),
			nullableAmount(item.RetailPricePerUnit),
			nullableAmount(item.DiscountedPricePerUnit),
			nullableAmount(item.TotalRetailPrice),
			amountOrZero(item.TotalDiscountedPrice),
		); err != nil {
			return err
		}
		if err := insertFuelFees(ctx, tx, localTransactionID, &lineNumber, item.Fees); err != nil {
			return err
		}
		lineNumber++
	}

	for _, item := range transaction.Products {
		if _, err := tx.Exec(ctx, `
			INSERT INTO fuel_transaction_items (
				fuel_transaction_id, line_number, item_kind, category,
				description, quantity, retail_price_per_unit, total_amount_paid
			) VALUES ($1, $2, 'product', $3, $4, $5, $6, $7)`,
			localTransactionID,
			lineNumber,
			strings.ToLower(strings.TrimSpace(item.ProductType)),
			nullIfBlank(item.ProductTypeDescription),
			nullableAmount(item.Quantity),
			nullableAmount(item.PricePerUnit),
			amountOrZero(item.PurchasePriceTotal),
		); err != nil {
			return err
		}
		if err := insertFuelFees(ctx, tx, localTransactionID, &lineNumber, item.Fees); err != nil {
			return err
		}
		lineNumber++
	}

	return insertFuelFees(ctx, tx, localTransactionID, nil, transaction.Fees)
}

func ensureRelayDriver(
	ctx context.Context,
	tx pgx.Tx,
	environment string,
	driver relay.TransactionDriver,
) (string, error) {
	relayDriverID := strings.TrimSpace(driver.ID)
	integrationID := nullableRelayString(driver.IntegrationID)

	var driverID string
	err := tx.QueryRow(ctx, `
		SELECT driver_id
		FROM relay_driver_links
		WHERE relay_environment = $1
		  AND relay_driver_id = $2`, environment, relayDriverID).Scan(&driverID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	fullName := strings.TrimSpace(strings.Join([]string{driver.FirstName, driver.LastName}, " "))
	if fullName == "" {
		fullName = "Relay Driver " + relayDriverID
	}
	if errors.Is(err, pgx.ErrNoRows) {
		driverID, err = ensureDriver(ctx, tx, fullName, nil)
		if err != nil {
			return "", err
		}
	}

	phone := nullIfBlank(driver.Phone)
	var email any
	if driver.Email != nil {
		email = nullIfBlank(*driver.Email)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE drivers
		SET phone = COALESCE(phone, $2),
			email = COALESCE(email, $3),
			active = CASE WHEN dispatcher_id IS NULL THEN false ELSE active END,
			updated_at = CASE
				WHEN (phone IS NULL AND $2::text IS NOT NULL)
				  OR (email IS NULL AND $3::text IS NOT NULL)
				  OR (dispatcher_id IS NULL AND active)
				THEN now() ELSE updated_at END
		WHERE id = $1`, driverID, phone, email); err != nil {
		return "", err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO relay_driver_links (
			relay_environment, relay_driver_id, relay_integration_id, driver_id,
			relay_first_name, relay_last_name, relay_phone, relay_email
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (relay_environment, relay_driver_id) DO UPDATE SET
			relay_integration_id = EXCLUDED.relay_integration_id,
			driver_id = EXCLUDED.driver_id,
			relay_first_name = EXCLUDED.relay_first_name,
			relay_last_name = EXCLUDED.relay_last_name,
			relay_phone = EXCLUDED.relay_phone,
			relay_email = EXCLUDED.relay_email,
			updated_at = now()`,
		environment,
		relayDriverID,
		integrationID,
		driverID,
		nullIfBlank(driver.FirstName),
		nullIfBlank(driver.LastName),
		phone,
		email,
	)
	return driverID, err
}

func insertFuelFees(
	ctx context.Context,
	tx pgx.Tx,
	transactionID string,
	lineNumber *int,
	fees []relay.Fee,
) error {
	for _, fee := range fees {
		if isExcludedRelayFee(fee.Type) {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO fuel_transaction_fees (
				fuel_transaction_id, item_line_number, fee_type, amount
			) VALUES ($1, $2, $3, $4)`,
			transactionID,
			lineNumber,
			strings.TrimSpace(fee.Type),
			amountOrZero(fee.Amount),
		); err != nil {
			return err
		}
	}
	return nil
}

func isExcludedRelayFee(feeType string) bool {
	switch strings.ToLower(strings.TrimSpace(feeType)) {
	case "sender_fee", "platform_fee", "deposit", "funding", "transfer":
		return true
	default:
		return false
	}
}

func (r *FuelRepository) ListTransactions(ctx context.Context) ([]FuelTransaction, error) {
	rows, err := r.pool.Query(ctx, fuelTransactionsSQL+`
		ORDER BY purchased_at DESC, relay_transaction_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	transactions := make([]FuelTransaction, 0)
	for rows.Next() {
		transaction, err := scanFuelTransaction(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, transaction)
	}
	return transactions, rows.Err()
}

type FuelPageQuery struct {
	Pagination Pagination
	Search     string
	Driver     string
	State      string
	Category   string
	DateFrom   *time.Time
	DateTo     *time.Time
}

type FuelFilterOptions struct {
	Drivers []string `json:"drivers"`
	States  []string `json:"states"`
}

type FuelSummary struct {
	Spend   float64 `json:"spend"`
	Saved   float64 `json:"saved"`
	Gallons float64 `json:"gallons"`
}

type FuelPage struct {
	Page[FuelTransaction]
	Options FuelFilterOptions `json:"options"`
	Summary FuelSummary       `json:"summary"`
}

type FuelDashboardQuery struct {
	Year        int
	MapDateFrom time.Time
	MapDateTo   time.Time
}

type FuelDashboardTotals struct {
	Spend   float64 `json:"spend"`
	Gallons float64 `json:"gallons"`
	Saved   float64 `json:"saved"`
}

type FuelMonthlyPoint struct {
	Month             string  `json:"month"`
	Spend             float64 `json:"spend"`
	Gallons           float64 `json:"gallons"`
	PricePerGallon    float64 `json:"pricePerGallon"`
	DiscountPerGallon float64 `json:"discountPerGallon"`
}

type FuelWeeklyPoint struct {
	WeekStart        string   `json:"weekStart"`
	FuelSpend        float64  `json:"fuelSpend"`
	GrossRevenue     float64  `json:"grossRevenue"`
	Miles            float64  `json:"miles"`
	FuelToGrossRatio *float64 `json:"fuelToGrossRatio"`
	AverageFuelPrice *float64 `json:"averageFuelPrice"`
	RevenuePerMile   *float64 `json:"revenuePerMile"`
}

type FuelStatePrice struct {
	State            string  `json:"state"`
	AveragePrice     float64 `json:"averagePrice"`
	Gallons          float64 `json:"gallons"`
	TransactionCount int     `json:"transactionCount"`
}

type FuelDashboardMethodology struct {
	FuelScope        string `json:"fuelScope"`
	RevenueScope     string `json:"revenueScope"`
	RevenueDate      string `json:"revenueDate"`
	WeekStartsOn     string `json:"weekStartsOn"`
	FuelDateTimezone string `json:"fuelDateTimezone"`
}

type FuelDashboard struct {
	Year        int                      `json:"year"`
	Totals      FuelDashboardTotals      `json:"totals"`
	Monthly     []FuelMonthlyPoint       `json:"monthly"`
	Weekly      []FuelWeeklyPoint        `json:"weekly"`
	StatePrices []FuelStatePrice         `json:"statePrices"`
	Methodology FuelDashboardMethodology `json:"methodology"`
}

func (r *FuelRepository) GetDashboard(ctx context.Context, query FuelDashboardQuery) (FuelDashboard, error) {
	dashboard := FuelDashboard{
		Year:        query.Year,
		Monthly:     make([]FuelMonthlyPoint, 0, 12),
		Weekly:      make([]FuelWeeklyPoint, 0),
		StatePrices: make([]FuelStatePrice, 0),
		Methodology: FuelDashboardMethodology{
			FuelScope:        "Fuel line items only; DEF, other products, cash advances, and fees are excluded.",
			RevenueScope:     "Loads whose normalized status is invoiced; reporting begins with the first full load-data week.",
			RevenueDate:      "Delivery date, falling back to pickup date when delivery is missing.",
			WeekStartsOn:     "Monday",
			FuelDateTimezone: "Each merchant location's timezone, falling back to America/New_York.",
		},
	}

	monthRows, err := r.pool.Query(ctx, fuelDashboardBaseSQL+`
		, months AS (
			SELECT generate_series(make_date($1, 1, 1), make_date($1, 12, 1), interval '1 month')::date AS month
		), monthly AS (
			SELECT date_trunc('month', purchased_on)::date AS month,
				SUM(spend) AS spend, SUM(gallons) AS gallons, SUM(saved) AS saved
			FROM fuel_purchase_totals
			WHERE purchased_on >= make_date($1, 1, 1)
			  AND purchased_on < make_date($1 + 1, 1, 1)
			GROUP BY 1
		)
		SELECT months.month,
			COALESCE(monthly.spend, 0)::float8,
			COALESCE(monthly.gallons, 0)::float8,
			COALESCE(monthly.saved, 0)::float8,
			CASE WHEN monthly.gallons > 0 THEN (monthly.spend / monthly.gallons)::float8 ELSE 0 END,
			CASE WHEN monthly.gallons > 0 THEN (monthly.saved / monthly.gallons)::float8 ELSE 0 END
		FROM months LEFT JOIN monthly USING (month)
		ORDER BY months.month`, query.Year)
	if err != nil {
		return FuelDashboard{}, err
	}
	defer monthRows.Close()
	for monthRows.Next() {
		var month time.Time
		var point FuelMonthlyPoint
		var saved float64
		if err := monthRows.Scan(&month, &point.Spend, &point.Gallons, &saved, &point.PricePerGallon, &point.DiscountPerGallon); err != nil {
			return FuelDashboard{}, err
		}
		point.Month = month.Format("2006-01")
		dashboard.Monthly = append(dashboard.Monthly, point)
		dashboard.Totals.Spend += point.Spend
		dashboard.Totals.Gallons += point.Gallons
		dashboard.Totals.Saved += saved
	}
	if err := monthRows.Err(); err != nil {
		return FuelDashboard{}, err
	}

	weekRows, err := r.pool.Query(ctx, fuelDashboardBaseSQL+`
		, fuel_weeks AS (
			SELECT date_trunc('week', purchased_on::timestamp)::date AS week_start,
				SUM(spend) AS spend, SUM(gallons) AS gallons
			FROM fuel_purchase_totals
			WHERE purchased_on >= make_date($1, 1, 1)
			  AND purchased_on < make_date($1 + 1, 1, 1)
			GROUP BY 1
		), load_events AS (
			SELECT (COALESCE(delivery_time, delivery_appointment_time,
				pickup_time, pickup_appointment_time) AT TIME ZONE 'America/New_York')::date AS service_date,
				total_pay, COALESCE(total_miles, 0) AS total_miles
			FROM loads
			WHERE lower(trim(status)) = 'invoiced'
			  AND COALESCE(delivery_time, delivery_appointment_time, pickup_time, pickup_appointment_time) IS NOT NULL
			  AND (COALESCE(delivery_time, delivery_appointment_time, pickup_time, pickup_appointment_time)
				AT TIME ZONE 'America/New_York')::date >= make_date($1, 1, 1)
			  AND (COALESCE(delivery_time, delivery_appointment_time, pickup_time, pickup_appointment_time)
				AT TIME ZONE 'America/New_York')::date < make_date($1 + 1, 1, 1)
		), load_coverage AS (
			SELECT MIN(service_date) AS first_date FROM load_events
		), weeks AS (
			SELECT generate_series(
				GREATEST(
					date_trunc('week', make_date($1, 1, 1)::timestamp)::date,
					CASE
						WHEN first_date = date_trunc('week', first_date::timestamp)::date
						THEN first_date
						ELSE date_trunc('week', first_date::timestamp)::date + 7
					END
				),
				date_trunc('week', LEAST(CURRENT_DATE, make_date($1, 12, 31))::timestamp)::date,
				interval '1 week'
			)::date AS week_start
			FROM load_coverage
		), load_weeks AS (
			SELECT date_trunc('week', service_date::timestamp)::date AS week_start,
				SUM(total_pay) AS gross, SUM(total_miles) AS miles
			FROM load_events
			GROUP BY 1
		)
		SELECT weeks.week_start,
			COALESCE(fuel_weeks.spend, 0)::float8,
			COALESCE(load_weeks.gross, 0)::float8,
			COALESCE(load_weeks.miles, 0)::float8,
			CASE WHEN load_weeks.gross > 0 THEN (COALESCE(fuel_weeks.spend, 0) / load_weeks.gross * 100)::float8 END,
			CASE WHEN fuel_weeks.gallons > 0 THEN (fuel_weeks.spend / fuel_weeks.gallons)::float8 END,
			CASE WHEN load_weeks.miles > 0 THEN (load_weeks.gross / load_weeks.miles)::float8 END
		FROM weeks
		LEFT JOIN fuel_weeks USING (week_start)
		LEFT JOIN load_weeks USING (week_start)
		ORDER BY weeks.week_start`, query.Year)
	if err != nil {
		return FuelDashboard{}, err
	}
	defer weekRows.Close()
	for weekRows.Next() {
		var week time.Time
		var point FuelWeeklyPoint
		if err := weekRows.Scan(&week, &point.FuelSpend, &point.GrossRevenue, &point.Miles,
			&point.FuelToGrossRatio, &point.AverageFuelPrice, &point.RevenuePerMile); err != nil {
			return FuelDashboard{}, err
		}
		point.WeekStart = week.Format(time.DateOnly)
		dashboard.Weekly = append(dashboard.Weekly, point)
	}
	if err := weekRows.Err(); err != nil {
		return FuelDashboard{}, err
	}

	stateRows, err := r.pool.Query(ctx, fuelDashboardBaseSQL+`
		SELECT state, (SUM(spend) / NULLIF(SUM(gallons), 0))::float8,
			SUM(gallons)::float8, SUM(transaction_count)::int
		FROM fuel_purchase_totals
		WHERE purchased_on BETWEEN $1 AND $2
		  AND state <> '' AND gallons > 0
		GROUP BY state
		ORDER BY state`, query.MapDateFrom, query.MapDateTo)
	if err != nil {
		return FuelDashboard{}, err
	}
	defer stateRows.Close()
	for stateRows.Next() {
		var price FuelStatePrice
		if err := stateRows.Scan(&price.State, &price.AveragePrice, &price.Gallons, &price.TransactionCount); err != nil {
			return FuelDashboard{}, err
		}
		dashboard.StatePrices = append(dashboard.StatePrices, price)
	}
	if err := stateRows.Err(); err != nil {
		return FuelDashboard{}, err
	}

	return dashboard, nil
}

var fuelDashboardBaseSQL = `
WITH fuel_purchase_totals AS (
	SELECT t.id,
		(t.purchased_at AT TIME ZONE ` + fuelTimezoneExpression("t.timezone") + `)::date AS purchased_on,
		t.state,
		COALESCE(SUM(i.total_amount_paid), 0) AS spend,
		COALESCE(SUM(i.quantity) FILTER (WHERE lower(COALESCE(i.unit_of_measure, '')) = 'gallons'), 0) AS gallons,
		COALESCE(SUM(GREATEST(COALESCE(i.total_retail_price, i.total_amount_paid) - i.total_amount_paid, 0)), 0) AS saved,
		1 AS transaction_count
	FROM fuel_transactions t
	JOIN fuel_transaction_items i ON i.fuel_transaction_id = t.id
	WHERE i.item_kind = 'fuel' AND lower(i.category) <> 'def'
	GROUP BY t.id, purchased_on, t.state
)`

func (r *FuelRepository) ListTransactionsPage(ctx context.Context, query FuelPageQuery) (FuelPage, error) {
	where := `
	WHERE ($1 = '' OR concat_ws(' ', driver_name, relay_integration_id,
		merchant_name, location_name, city, state, relay_transaction_id)
		ILIKE '%' || $1 || '%')
	AND ($2 = '' OR driver_name = $2)
	AND ($3 = '' OR state = $3)
	AND ($4 = '' OR ($4 = 'fuel' AND fuel_amount > 0)
		OR ($4 = 'def' AND def_amount > 0) OR ($4 = 'other' AND other_amount > 0))
	AND ($5::date IS NULL OR (purchased_at AT TIME ZONE ` + fuelTimezoneExpression("timezone") + `)::date >= $5)
	AND ($6::date IS NULL OR (purchased_at AT TIME ZONE ` + fuelTimezoneExpression("timezone") + `)::date <= $6)`
	args := []any{query.Search, query.Driver, query.State, query.Category, query.DateFrom, query.DateTo}
	cte := "WITH transactions AS (" + fuelTransactionsSQL + ")"
	var total int
	summary := FuelSummary{}
	if err := r.pool.QueryRow(ctx, cte+`
		SELECT count(*), COALESCE(sum(total_amount_paid), 0),
			COALESCE(sum(total_amount_saved), 0),
			COALESCE(sum(fuel_volume + def_volume), 0)
		FROM transactions `+where, args...).Scan(
		&total, &summary.Spend, &summary.Saved, &summary.Gallons,
	); err != nil {
		return FuelPage{}, err
	}
	query.Pagination = query.Pagination.Normalize(total)
	pageArgs := append(args, query.Pagination.PageSize, query.Pagination.Offset())
	rows, err := r.pool.Query(ctx, cte+`
		SELECT * FROM transactions `+where+`
		ORDER BY purchased_at DESC, relay_transaction_id DESC LIMIT $7 OFFSET $8`, pageArgs...)
	if err != nil {
		return FuelPage{}, err
	}
	defer rows.Close()
	transactions := make([]FuelTransaction, 0, query.Pagination.PageSize)
	for rows.Next() {
		transaction, scanErr := scanFuelTransaction(rows)
		if scanErr != nil {
			return FuelPage{}, scanErr
		}
		transactions = append(transactions, transaction)
	}
	if err := rows.Err(); err != nil {
		return FuelPage{}, err
	}
	options := FuelFilterOptions{}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(array_agg(DISTINCT d.full_name ORDER BY d.full_name), '{}'),
			COALESCE(array_agg(DISTINCT t.state ORDER BY t.state) FILTER (WHERE t.state <> ''), '{}')
		FROM fuel_transactions t JOIN drivers d ON d.id = t.driver_id`).Scan(
		&options.Drivers, &options.States,
	); err != nil {
		return FuelPage{}, err
	}
	for index := range options.Drivers {
		options.Drivers[index] = formatPersonName(options.Drivers[index])
	}
	return FuelPage{Page: NewPage(transactions, total, query.Pagination), Options: options, Summary: summary}, nil
}

const fuelTransactionsSQL = `
SELECT
	t.id, t.relay_transaction_id, t.driver_id, d.full_name AS driver_name,
	t.relay_driver_id, t.relay_integration_id, t.purchased_at,
	t.merchant_name, t.location_name, t.city, t.state, t.timezone,
	t.total_amount_paid::float8, t.total_retail_price::float8,
	t.total_amount_saved::float8, t.cash_advance::float8, t.currency_code,
	COALESCE(items.fuel_amount, 0)::float8 AS fuel_amount,
	COALESCE(items.def_amount, 0)::float8 AS def_amount,
	COALESCE(items.other_amount, 0)::float8 AS other_amount,
	COALESCE(items.fuel_volume, 0)::float8 AS fuel_volume,
	COALESCE(items.def_volume, 0)::float8 AS def_volume,
	t.fuel_code_type, t.is_direct_bill
FROM fuel_transactions t
JOIN drivers d ON d.id = t.driver_id
LEFT JOIN LATERAL (
	SELECT
		SUM(total_amount_paid) FILTER (WHERE item_kind = 'fuel' AND category <> 'def') AS fuel_amount,
		SUM(total_amount_paid) FILTER (WHERE item_kind = 'fuel' AND category = 'def') AS def_amount,
		SUM(total_amount_paid) FILTER (WHERE item_kind = 'product') AS other_amount,
		SUM(quantity) FILTER (WHERE item_kind = 'fuel' AND category <> 'def' AND unit_of_measure = 'gallons') AS fuel_volume,
		SUM(quantity) FILTER (WHERE item_kind = 'fuel' AND category = 'def' AND unit_of_measure = 'gallons') AS def_volume
	FROM fuel_transaction_items
	WHERE fuel_transaction_id = t.id
) items ON true`

func scanFuelTransaction(row rowScanner) (FuelTransaction, error) {
	var transaction FuelTransaction
	err := row.Scan(
		&transaction.ID, &transaction.RelayTransactionID, &transaction.DriverID,
		&transaction.DriverName, &transaction.RelayDriverID,
		&transaction.RelayIntegrationID, &transaction.PurchasedAt,
		&transaction.MerchantName, &transaction.LocationName, &transaction.City,
		&transaction.State, &transaction.Timezone, &transaction.TotalAmountPaid,
		&transaction.TotalRetailPrice, &transaction.TotalAmountSaved,
		&transaction.CashAdvance, &transaction.CurrencyCode, &transaction.FuelAmount,
		&transaction.DEFAmount, &transaction.OtherAmount, &transaction.FuelVolume,
		&transaction.DEFVolume, &transaction.FuelCodeType, &transaction.IsDirectBill,
	)
	transaction.DriverName = formatPersonName(transaction.DriverName)
	return transaction, err
}

func nullIfBlank(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func nullableRelayString(value *string) any {
	if value == nil {
		return nil
	}
	return nullIfBlank(*value)
}

func nullableAmount(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func amountOrZero(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

func normalizeFuelTimezone(value string) string {
	trimmed := strings.TrimSpace(value)
	if canonical, ok := legacyFuelTimezones[trimmed]; ok {
		return canonical
	}
	return trimmed
}

func fuelTimezoneExpression(column string) string {
	return `(CASE ` + column + `
		WHEN 'US/Eastern' THEN 'America/New_York'
		WHEN 'US/Central' THEN 'America/Chicago'
		WHEN 'US/Mountain' THEN 'America/Denver'
		WHEN 'US/Pacific' THEN 'America/Los_Angeles'
		WHEN 'US/Arizona' THEN 'America/Phoenix'
		WHEN 'US/Alaska' THEN 'America/Anchorage'
		WHEN 'US/Aleutian' THEN 'America/Adak'
		WHEN 'US/Hawaii' THEN 'Pacific/Honolulu'
		WHEN 'US/East-Indiana' THEN 'America/Indiana/Indianapolis'
		WHEN 'US/Indiana-Starke' THEN 'America/Indiana/Knox'
		WHEN 'US/Michigan' THEN 'America/Detroit'
		WHEN 'US/Samoa' THEN 'Pacific/Pago_Pago'
		ELSE COALESCE(
			(SELECT z.name FROM pg_timezone_names z WHERE z.name = NULLIF(` + column + `, '') LIMIT 1),
			'America/New_York'
		)
	END)`
}

var legacyFuelTimezones = map[string]string{
	"US/Eastern":        "America/New_York",
	"US/Central":        "America/Chicago",
	"US/Mountain":       "America/Denver",
	"US/Pacific":        "America/Los_Angeles",
	"US/Arizona":        "America/Phoenix",
	"US/Alaska":         "America/Anchorage",
	"US/Aleutian":       "America/Adak",
	"US/Hawaii":         "Pacific/Honolulu",
	"US/East-Indiana":   "America/Indiana/Indianapolis",
	"US/Indiana-Starke": "America/Indiana/Knox",
	"US/Michigan":       "America/Detroit",
	"US/Samoa":          "Pacific/Pago_Pago",
}
