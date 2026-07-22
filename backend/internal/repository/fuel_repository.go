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
		strings.TrimSpace(transaction.Location.Timezone),
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

func (r *FuelRepository) ListTransactionsPage(ctx context.Context, query FuelPageQuery) (FuelPage, error) {
	const where = `
	WHERE ($1 = '' OR concat_ws(' ', driver_name, relay_integration_id,
		merchant_name, location_name, city, state, relay_transaction_id)
		ILIKE '%' || $1 || '%')
	AND ($2 = '' OR driver_name = $2)
	AND ($3 = '' OR state = $3)
	AND ($4 = '' OR ($4 = 'fuel' AND fuel_amount > 0)
		OR ($4 = 'def' AND def_amount > 0) OR ($4 = 'other' AND other_amount > 0))
	AND ($5::date IS NULL OR (purchased_at AT TIME ZONE COALESCE(NULLIF(timezone, ''), 'America/New_York'))::date >= $5)
	AND ($6::date IS NULL OR (purchased_at AT TIME ZONE COALESCE(NULLIF(timezone, ''), 'America/New_York'))::date <= $6)`
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
