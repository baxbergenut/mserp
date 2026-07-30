package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/prepass"
)

type TollRepository struct {
	pool *pgxpool.Pool
}

func NewTollRepository(pool *pgxpool.Pool) *TollRepository {
	return &TollRepository{pool: pool}
}

type Toll struct {
	ID                 string   `json:"id"`
	TruckID            *string  `json:"truckId"`
	TruckUnit          string   `json:"truckUnit"`
	PostingDate        string   `json:"postingDate"`
	InvoiceDate        string   `json:"invoiceDate"`
	CustomerID         string   `json:"customerId"`
	Source             string   `json:"source"`
	ReadType           string   `json:"readType"`
	PrePassTagID       *string  `json:"prePassTagId"`
	TransponderOrPlate string   `json:"transponderOrPlate"`
	EquipmentUnit      string   `json:"equipmentUnit"`
	Agency             string   `json:"agency"`
	EntryPlaza         *string  `json:"entryPlaza"`
	EntryDate          *string  `json:"entryDate"`
	EntryTime          *string  `json:"entryTime"`
	ExitPlaza          string   `json:"exitPlaza"`
	ExitDate           string   `json:"exitDate"`
	ExitTime           string   `json:"exitTime"`
	TollClass          string   `json:"tollClass"`
	Miles              *float64 `json:"miles"`
	Amount             float64  `json:"amount"`
	ReportFileName     string   `json:"reportFileName"`
}

func (r *TollRepository) ListTolls(ctx context.Context) ([]Toll, error) {
	rows, err := r.pool.Query(ctx, selectTollsSQL+`
		ORDER BY t.posting_date DESC, t.exit_date DESC, t.exit_time DESC, t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]Toll, 0)
	for rows.Next() {
		value, err := scanToll(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

type TollPageQuery struct {
	Pagination Pagination
	Search     string
	Unit       string
	Agency     string
	PostFrom   *time.Time
	PostTo     *time.Time
}

type TollFilterOptions struct {
	Units    []string `json:"units"`
	Agencies []string `json:"agencies"`
}

type TollSummary struct {
	Amount     float64 `json:"amount"`
	TruckCount int     `json:"truckCount"`
}

type TollPage struct {
	Page[Toll]
	Options TollFilterOptions `json:"options"`
	Summary TollSummary       `json:"summary"`
}

func (r *TollRepository) ListTollsPage(ctx context.Context, query TollPageQuery) (TollPage, error) {
	const joins = `
		FROM tolls t
		LEFT JOIN trucks tr ON tr.id = t.truck_id
		LEFT JOIN toll_reports report ON report.id = t.report_id`
	const where = `
	WHERE ($1 = '' OR concat_ws(' ', COALESCE(tr.unit_number, t.equipment_unit),
		t.agency, t.entry_plaza, t.exit_plaza, t.prepass_tag_id,
		t.transponder_or_plate, COALESCE(report.file_name, 'PrePass API'))
		ILIKE '%' || $1 || '%')
	AND ($2 = '' OR COALESCE(tr.unit_number, t.equipment_unit) = $2)
	AND ($3 = '' OR t.agency = $3)
	AND ($4::date IS NULL OR t.posting_date >= $4)
	AND ($5::date IS NULL OR t.posting_date <= $5)`
	args := []any{query.Search, query.Unit, query.Agency, query.PostFrom, query.PostTo}
	var total int
	summary := TollSummary{}
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(t.amount)::float8, 0), count(DISTINCT t.truck_id)`+
		joins+where, args...).Scan(&total, &summary.Amount, &summary.TruckCount); err != nil {
		return TollPage{}, err
	}
	query.Pagination = query.Pagination.Normalize(total)
	pageArgs := append(args, query.Pagination.PageSize, query.Pagination.Offset())
	rows, err := r.pool.Query(ctx, selectTollsSQL+where+`
		ORDER BY t.posting_date DESC, t.exit_date DESC, t.exit_time DESC, t.id
		LIMIT $6 OFFSET $7`, pageArgs...)
	if err != nil {
		return TollPage{}, err
	}
	defer rows.Close()
	values := make([]Toll, 0, query.Pagination.PageSize)
	for rows.Next() {
		value, scanErr := scanToll(rows)
		if scanErr != nil {
			return TollPage{}, scanErr
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return TollPage{}, err
	}
	options := TollFilterOptions{}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(array_agg(
				DISTINCT COALESCE(tr.unit_number, t.equipment_unit)
				ORDER BY COALESCE(tr.unit_number, t.equipment_unit)
			), '{}'),
			COALESCE(array_agg(DISTINCT t.agency ORDER BY t.agency), '{}')
		FROM tolls t LEFT JOIN trucks tr ON tr.id = t.truck_id`).Scan(
		&options.Units, &options.Agencies,
	); err != nil {
		return TollPage{}, err
	}
	return TollPage{Page: NewPage(values, total, query.Pagination), Options: options, Summary: summary}, nil
}

const selectTollsSQL = `
SELECT t.id, t.truck_id, COALESCE(tr.unit_number, t.equipment_unit),
	to_char(t.posting_date, 'YYYY-MM-DD'), to_char(t.invoice_date, 'YYYY-MM-DD'),
	t.customer_id, t.source, t.read_type, t.prepass_tag_id,
	t.transponder_or_plate, t.equipment_unit, t.agency, t.entry_plaza,
	to_char(t.entry_date, 'YYYY-MM-DD'), to_char(t.entry_time, 'HH24:MI:SS'),
	t.exit_plaza, to_char(t.exit_date, 'YYYY-MM-DD'),
	to_char(t.exit_time, 'HH24:MI:SS'), t.toll_class,
	t.miles::float8, t.amount::float8, COALESCE(report.file_name, 'PrePass API')
FROM tolls t
LEFT JOIN trucks tr ON tr.id = t.truck_id
LEFT JOIN toll_reports report ON report.id = t.report_id`

func scanToll(row rowScanner) (Toll, error) {
	var value Toll
	err := row.Scan(
		&value.ID, &value.TruckID, &value.TruckUnit,
		&value.PostingDate, &value.InvoiceDate, &value.CustomerID,
		&value.Source, &value.ReadType, &value.PrePassTagID,
		&value.TransponderOrPlate, &value.EquipmentUnit, &value.Agency,
		&value.EntryPlaza, &value.EntryDate, &value.EntryTime,
		&value.ExitPlaza, &value.ExitDate, &value.ExitTime,
		&value.TollClass, &value.Miles, &value.Amount, &value.ReportFileName,
	)
	return value, err
}

type TollSyncDayResult struct {
	Saved     int
	Unmatched int
}

func (r *TollRepository) CompletedDays(
	ctx context.Context,
	environment string,
	startDate time.Time,
	endDate time.Time,
) (map[string]struct{}, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sync_date
		FROM prepass_toll_sync_days
		WHERE prepass_environment = $1 AND sync_date BETWEEN $2 AND $3`,
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

func (r *TollRepository) ReconcileTruckAssignments(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tolls
		SET truck_id = trucks.id
		FROM trucks
		WHERE tolls.truck_id IS NULL
			AND trucks.unit_number = tolls.equipment_unit`)
	return err
}

func (r *TollRepository) UpsertDay(
	ctx context.Context,
	environment string,
	day time.Time,
	transactions []prepass.Transaction,
	syncedAt time.Time,
	markComplete bool,
) (TollSyncDayResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return TollSyncDayResult{}, err
	}
	defer tx.Rollback(ctx)

	truckIDs := make(map[string]string)
	truckRows, err := tx.Query(ctx, `SELECT unit_number, id FROM trucks`)
	if err != nil {
		return TollSyncDayResult{}, err
	}
	for truckRows.Next() {
		var unit, id string
		if scanErr := truckRows.Scan(&unit, &id); scanErr != nil {
			truckRows.Close()
			return TollSyncDayResult{}, scanErr
		}
		truckIDs[unit] = id
	}
	if err := truckRows.Err(); err != nil {
		truckRows.Close()
		return TollSyncDayResult{}, err
	}
	truckRows.Close()

	result := TollSyncDayResult{}
	for _, transaction := range transactions {
		value, err := mapPrePassToll(environment, transaction)
		if err != nil {
			return TollSyncDayResult{}, fmt.Errorf(
				"map PrePass toll %d: %w",
				transaction.TollID,
				err,
			)
		}
		truckID := optionalTruckID(truckIDs[value.EquipmentUnit])
		if truckID == nil {
			result.Unmatched++
		}
		if err := upsertPrePassToll(ctx, tx, environment, truckID, value); err != nil {
			return TollSyncDayResult{}, fmt.Errorf(
				"upsert PrePass toll %d: %w",
				transaction.TollID,
				err,
			)
		}
		result.Saved++
	}

	if markComplete {
		if _, err := tx.Exec(ctx, `
			INSERT INTO prepass_toll_sync_days (
				prepass_environment, sync_date, transaction_count, fetched_at
			) VALUES ($1, $2, $3, $4)
			ON CONFLICT (prepass_environment, sync_date) DO UPDATE SET
				transaction_count = EXCLUDED.transaction_count,
				fetched_at = EXCLUDED.fetched_at`,
			environment,
			day.Format(time.DateOnly),
			len(transactions),
			syncedAt,
		); err != nil {
			return TollSyncDayResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return TollSyncDayResult{}, err
	}
	return result, nil
}

type prepassTollValue struct {
	TollID             int64
	PostingDate        time.Time
	InvoiceDate        time.Time
	CustomerID         string
	Source             string
	ReadType           string
	PrePassTagID       *string
	TransponderOrPlate string
	EquipmentUnit      string
	Agency             string
	EntryPlaza         *string
	EntryDate          *time.Time
	EntryTime          *string
	ExitPlaza          string
	ExitDate           time.Time
	ExitTime           string
	TollClass          string
	AmountCents        int64
	Fingerprint        string
}

func mapPrePassToll(environment string, transaction prepass.Transaction) (prepassTollValue, error) {
	if transaction.TollID <= 0 {
		return prepassTollValue{}, errors.New("tollId is required")
	}
	postingAt, err := requiredPrePassTime(transaction.PostDateTime, "postDateTime")
	if err != nil {
		return prepassTollValue{}, err
	}

	var entryDate *time.Time
	var entryTime *string
	var entryAt *time.Time
	if strings.TrimSpace(transaction.EntryDateTime) != "" {
		parsedEntryAt, err := requiredPrePassTime(transaction.EntryDateTime, "entryDateTime")
		if err != nil {
			return prepassTollValue{}, err
		}
		entryAt = &parsedEntryAt
		value := encodedDateOnly(parsedEntryAt)
		clock := parsedEntryAt.Format("15:04:05")
		entryDate = &value
		entryTime = &clock
	}
	invoiceAt, err := optionalPrePassTime(
		transaction.InvoiceDateTime,
		postingAt,
		"invoiceDateTime",
	)
	if err != nil {
		return prepassTollValue{}, err
	}
	exitFallback := postingAt
	if entryAt != nil {
		exitFallback = *entryAt
	}
	exitAt, err := optionalPrePassTime(
		transaction.ExitDateTime,
		exitFallback,
		"exitDateTime",
	)
	if err != nil {
		return prepassTollValue{}, err
	}

	customerID := firstNonEmpty(transaction.AccountNumber.String(), "Unknown")
	equipmentUnit := normalizeTruckUnit(transaction.VehicleNumber)
	if equipmentUnit == "" {
		unassignedID := firstNonEmpty(
			transaction.PlateNumber,
			transaction.DeviceNumber,
			transaction.PPDeviceID,
			"Unknown",
		)
		equipmentUnit = normalizeTruckUnit("Unassigned " + unassignedID)
	}
	transponderOrPlate := firstNonEmpty(
		transaction.DeviceNumber,
		transaction.PlateNumber,
		transaction.PPDeviceID,
		"Unknown",
	)
	agency := firstNonEmpty(
		transaction.TollAgencyCode,
		transaction.TollAgencyName,
		transaction.BillingAgencyCode,
		"Unknown",
	)
	amountCents, err := decimalToCents(transaction.TollCharge.String())
	if err != nil {
		return prepassTollValue{}, fmt.Errorf("tollCharge: %w", err)
	}
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf(
		"prepass:%s:%d",
		environment,
		transaction.TollID,
	)))

	return prepassTollValue{
		TollID:             transaction.TollID,
		PostingDate:        encodedDateOnly(postingAt),
		InvoiceDate:        encodedDateOnly(invoiceAt),
		CustomerID:         customerID,
		Source:             firstNonEmpty(transaction.BillingAgencyCode, "PrePass"),
		ReadType:           firstNonEmpty(transaction.ReadType, "UNKNOWN"),
		PrePassTagID:       optionalString(transaction.PPDeviceID),
		TransponderOrPlate: transponderOrPlate,
		EquipmentUnit:      equipmentUnit,
		Agency:             agency,
		EntryPlaza: optionalString(firstNonEmpty(
			transaction.EntryPlazaCode,
			transaction.EntryPlazaName,
		)),
		EntryDate: entryDate,
		EntryTime: entryTime,
		ExitPlaza: firstNonEmpty(
			transaction.ExitPlazaCode,
			transaction.ExitPlazaName,
			"Unknown",
		),
		ExitDate:    encodedDateOnly(exitAt),
		ExitTime:    exitAt.Format("15:04:05"),
		TollClass:   firstNonEmpty(transaction.TollClass, "Unknown"),
		AmountCents: amountCents,
		Fingerprint: fmt.Sprintf("%x", fingerprint),
	}, nil
}

func upsertPrePassToll(
	ctx context.Context,
	tx pgx.Tx,
	environment string,
	truckID *string,
	value prepassTollValue,
) error {
	// Historical CSV exports use different labels for several PrePass fields.
	// Match on the stable values shared by both formats before inserting a new
	// API-backed row, so the first API backfill does not duplicate old charges.
	command, err := tx.Exec(ctx, `
		UPDATE tolls
		SET prepass_environment = $1,
			prepass_toll_id = $2,
			truck_id = COALESCE(truck_id, $3)
		WHERE id = (
			SELECT id
			FROM tolls
			WHERE prepass_toll_id IS NULL
				AND posting_date = $4
				AND equipment_unit = $5
				AND amount = $6::numeric
				AND exit_date = $7
				AND exit_time = $8::time
			ORDER BY created_at, id
			LIMIT 1
		)`,
		environment,
		value.TollID,
		truckID,
		value.PostingDate,
		value.EquipmentUnit,
		formatCents(value.AmountCents),
		value.ExitDate,
		value.ExitTime,
	)
	if err != nil {
		return err
	}
	if command.RowsAffected() > 0 {
		return nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO tolls (
			report_id, truck_id, posting_date, invoice_date, customer_id,
			source, read_type, prepass_tag_id, transponder_or_plate,
			equipment_unit, agency, entry_plaza, entry_date, entry_time,
			exit_plaza, exit_date, exit_time, toll_class, miles, amount,
			row_fingerprint, prepass_environment, prepass_toll_id
		) VALUES (
			NULL, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			$12, $13::time, $14, $15, $16::time, $17, NULL, $18::numeric,
			$19, $20, $21
		)
		ON CONFLICT (prepass_environment, prepass_toll_id) DO UPDATE SET
			truck_id = COALESCE(EXCLUDED.truck_id, tolls.truck_id),
			posting_date = EXCLUDED.posting_date,
			invoice_date = EXCLUDED.invoice_date,
			customer_id = EXCLUDED.customer_id,
			source = EXCLUDED.source,
			read_type = EXCLUDED.read_type,
			prepass_tag_id = EXCLUDED.prepass_tag_id,
			transponder_or_plate = EXCLUDED.transponder_or_plate,
			equipment_unit = EXCLUDED.equipment_unit,
			agency = EXCLUDED.agency,
			entry_plaza = EXCLUDED.entry_plaza,
			entry_date = EXCLUDED.entry_date,
			entry_time = EXCLUDED.entry_time,
			exit_plaza = EXCLUDED.exit_plaza,
			exit_date = EXCLUDED.exit_date,
			exit_time = EXCLUDED.exit_time,
			toll_class = EXCLUDED.toll_class,
			amount = EXCLUDED.amount`,
		truckID,
		value.PostingDate,
		value.InvoiceDate,
		value.CustomerID,
		value.Source,
		value.ReadType,
		value.PrePassTagID,
		value.TransponderOrPlate,
		value.EquipmentUnit,
		value.Agency,
		value.EntryPlaza,
		value.EntryDate,
		value.EntryTime,
		value.ExitPlaza,
		value.ExitDate,
		value.ExitTime,
		value.TollClass,
		formatCents(value.AmountCents),
		value.Fingerprint,
		environment,
		value.TollID,
	)
	return err
}

func requiredPrePassTime(value, field string) (time.Time, error) {
	parsed, err := prepass.ParseTimestamp(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s has invalid timestamp %q", field, value)
	}
	return parsed, nil
}

func optionalPrePassTime(
	value string,
	fallback time.Time,
	field string,
) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return requiredPrePassTime(value, field)
}

func encodedDateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func decimalToCents(value string) (int64, error) {
	amount, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return 0, fmt.Errorf("invalid decimal %q", value)
	}
	amount.Mul(amount, big.NewRat(100, 1))
	if !amount.IsInt() || !amount.Num().IsInt64() {
		return 0, fmt.Errorf("amount %q does not have valid cents", value)
	}
	return amount.Num().Int64(), nil
}

func formatCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalTruckID(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
