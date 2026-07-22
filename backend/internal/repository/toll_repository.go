package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TollRepository struct {
	pool *pgxpool.Pool
}

func NewTollRepository(pool *pgxpool.Pool) *TollRepository {
	return &TollRepository{pool: pool}
}

type TollImportRow struct {
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
	Miles              *string
	AmountCents        int64
	Fingerprint        string
}

type UnmatchedTollUnit struct {
	UnitNumber string `json:"unitNumber"`
	RowCount   int    `json:"rowCount"`
}

type TollImportResult struct {
	ReportID         string              `json:"reportId"`
	FileName         string              `json:"fileName"`
	RowCount         int                 `json:"rowCount"`
	ImportedCount    int                 `json:"importedCount"`
	DuplicateCount   int                 `json:"duplicateCount"`
	UnmatchedCount   int                 `json:"unmatchedCount"`
	UnmatchedUnits   []UnmatchedTollUnit `json:"unmatchedUnits"`
	TotalAmount      float64             `json:"totalAmount"`
	ImportedAmount   float64             `json:"importedAmount"`
	PostingDateStart string              `json:"postingDateStart"`
	PostingDateEnd   string              `json:"postingDateEnd"`
}

type Toll struct {
	ID                 string   `json:"id"`
	TruckID            string   `json:"truckId"`
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
		JOIN trucks tr ON tr.id = t.truck_id
		JOIN toll_reports report ON report.id = t.report_id`
	const where = `
	WHERE ($1 = '' OR concat_ws(' ', tr.unit_number, t.agency, t.entry_plaza,
		t.exit_plaza, t.prepass_tag_id, t.transponder_or_plate, report.file_name)
		ILIKE '%' || $1 || '%')
	AND ($2 = '' OR tr.unit_number = $2)
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
			COALESCE(array_agg(DISTINCT tr.unit_number ORDER BY tr.unit_number), '{}'),
			COALESCE(array_agg(DISTINCT t.agency ORDER BY t.agency), '{}')
		FROM tolls t JOIN trucks tr ON tr.id = t.truck_id`).Scan(
		&options.Units, &options.Agencies,
	); err != nil {
		return TollPage{}, err
	}
	return TollPage{Page: NewPage(values, total, query.Pagination), Options: options, Summary: summary}, nil
}

const selectTollsSQL = `
SELECT t.id, t.truck_id, tr.unit_number,
	to_char(t.posting_date, 'YYYY-MM-DD'), to_char(t.invoice_date, 'YYYY-MM-DD'),
	t.customer_id, t.source, t.read_type, t.prepass_tag_id,
	t.transponder_or_plate, t.equipment_unit, t.agency, t.entry_plaza,
	to_char(t.entry_date, 'YYYY-MM-DD'), to_char(t.entry_time, 'HH24:MI:SS'),
	t.exit_plaza, to_char(t.exit_date, 'YYYY-MM-DD'),
	to_char(t.exit_time, 'HH24:MI:SS'), t.toll_class,
	t.miles::float8, t.amount::float8, report.file_name
FROM tolls t
JOIN trucks tr ON tr.id = t.truck_id
JOIN toll_reports report ON report.id = t.report_id`

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

func (r *TollRepository) ImportTolls(
	ctx context.Context,
	fileName string,
	fileSHA256 string,
	rows []TollImportRow,
	totalAmountCents int64,
) (TollImportResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return TollImportResult{}, err
	}
	defer tx.Rollback(ctx)

	unitCounts := make(map[string]int)
	for _, row := range rows {
		unitCounts[row.EquipmentUnit]++
	}
	units := make([]string, 0, len(unitCounts))
	for unit := range unitCounts {
		units = append(units, unit)
	}

	truckIDs := make(map[string]string, len(units))
	truckRows, err := tx.Query(ctx, `SELECT unit_number, id FROM trucks WHERE unit_number = ANY($1)`, units)
	if err != nil {
		return TollImportResult{}, err
	}
	for truckRows.Next() {
		var unit, id string
		if scanErr := truckRows.Scan(&unit, &id); scanErr != nil {
			truckRows.Close()
			return TollImportResult{}, scanErr
		}
		truckIDs[unit] = id
	}
	if err := truckRows.Err(); err != nil {
		truckRows.Close()
		return TollImportResult{}, err
	}
	truckRows.Close()

	unmatchedUnits := make([]UnmatchedTollUnit, 0)
	unmatchedCount := 0
	for unit, count := range unitCounts {
		if _, found := truckIDs[unit]; found {
			continue
		}
		unmatchedUnits = append(unmatchedUnits, UnmatchedTollUnit{UnitNumber: unit, RowCount: count})
		unmatchedCount += count
	}
	sort.Slice(unmatchedUnits, func(i, j int) bool {
		return unmatchedUnits[i].UnitNumber < unmatchedUnits[j].UnitNumber
	})
	unmatchedJSON, err := json.Marshal(unmatchedUnits)
	if err != nil {
		return TollImportResult{}, err
	}

	postingDateStart, postingDateEnd := rows[0].PostingDate, rows[0].PostingDate
	for _, row := range rows[1:] {
		if row.PostingDate.Before(postingDateStart) {
			postingDateStart = row.PostingDate
		}
		if row.PostingDate.After(postingDateEnd) {
			postingDateEnd = row.PostingDate
		}
	}

	var reportID string
	err = tx.QueryRow(ctx, `
		INSERT INTO toll_reports (
			file_name, file_sha256, row_count, imported_count, duplicate_count,
			unmatched_count, unmatched_units, total_amount, imported_amount,
			posting_date_start, posting_date_end
		) VALUES ($1, $2, $3, 0, 0, $4, $5, $6::numeric, 0, $7, $8)
		RETURNING id`,
		fileName, fileSHA256, len(rows), unmatchedCount, unmatchedJSON,
		formatCents(totalAmountCents), postingDateStart, postingDateEnd,
	).Scan(&reportID)
	if err != nil {
		return TollImportResult{}, err
	}

	importedCount := 0
	duplicateCount := 0
	importedAmountCents := int64(0)
	for _, row := range rows {
		truckID, found := truckIDs[row.EquipmentUnit]
		if !found {
			continue
		}
		command, execErr := tx.Exec(ctx, `
			INSERT INTO tolls (
				report_id, truck_id, posting_date, invoice_date, customer_id,
				source, read_type, prepass_tag_id, transponder_or_plate,
				equipment_unit, agency, entry_plaza, entry_date, entry_time,
				exit_plaza, exit_date, exit_time, toll_class, miles, amount,
				row_fingerprint
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
				$13, $14::time, $15, $16, $17::time, $18, $19::numeric,
				$20::numeric, $21
			)
			ON CONFLICT (row_fingerprint) DO NOTHING`,
			reportID, truckID, row.PostingDate, row.InvoiceDate, row.CustomerID,
			row.Source, row.ReadType, row.PrePassTagID, row.TransponderOrPlate,
			row.EquipmentUnit, row.Agency, row.EntryPlaza, row.EntryDate, row.EntryTime,
			row.ExitPlaza, row.ExitDate, row.ExitTime, row.TollClass, row.Miles,
			formatCents(row.AmountCents), row.Fingerprint,
		)
		if execErr != nil {
			return TollImportResult{}, execErr
		}
		if command.RowsAffected() == 0 {
			duplicateCount++
			continue
		}
		importedCount++
		importedAmountCents += row.AmountCents
	}

	_, err = tx.Exec(ctx, `
		UPDATE toll_reports
		SET imported_count = $2, duplicate_count = $3, imported_amount = $4::numeric
		WHERE id = $1`, reportID, importedCount, duplicateCount, formatCents(importedAmountCents))
	if err != nil {
		return TollImportResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TollImportResult{}, err
	}

	return TollImportResult{
		ReportID: reportID, FileName: fileName, RowCount: len(rows),
		ImportedCount: importedCount, DuplicateCount: duplicateCount,
		UnmatchedCount: unmatchedCount, UnmatchedUnits: unmatchedUnits,
		TotalAmount:      float64(totalAmountCents) / 100,
		ImportedAmount:   float64(importedAmountCents) / 100,
		PostingDateStart: postingDateStart.Format("2006-01-02"),
		PostingDateEnd:   postingDateEnd.Format("2006-01-02"),
	}, nil
}

func formatCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}
