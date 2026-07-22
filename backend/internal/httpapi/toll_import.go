package httpapi

import (
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"mserp/internal/repository"
)

var tollCSVHeaders = []string{
	"PostingDate", "InvoiceDate", "CustID", "Source", "ReadType",
	"PPTagID", "ETagID_Plate", "EquipID", "Agency", "Entry_Plaza",
	"Entry_Date", "Entry_Time", "Exit_Plaza", "Exit_Date", "Exit_Time",
	"Toll_Class", "Miles", "Toll_Amount",
}

func parseTollCSV(input io.Reader) ([]repository.TollImportRow, int64, error) {
	reader := csv.NewReader(input)
	reader.FieldsPerRecord = len(tollCSVHeaders)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, 0, errors.New("the CSV file is empty")
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read CSV header: %w", err)
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	for index, expected := range tollCSVHeaders {
		if strings.TrimSpace(header[index]) != expected {
			return nil, 0, fmt.Errorf(
				"unexpected CSV column %d: expected %q, found %q",
				index+1, expected, strings.TrimSpace(header[index]),
			)
		}
	}

	rows := make([]repository.TollImportRow, 0)
	occurrences := make(map[string]int)
	totalAmountCents := int64(0)
	for line := 2; ; line++ {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, 0, fmt.Errorf("read CSV line %d: %w", line, readErr)
		}
		if recordIsBlank(record) {
			continue
		}
		for index := range record {
			record[index] = strings.TrimSpace(record[index])
		}

		row, parseErr := parseTollRecord(record)
		if parseErr != nil {
			return nil, 0, fmt.Errorf("CSV line %d: %w", line, parseErr)
		}
		canonical := strings.Join(record, "\x1f")
		occurrence := occurrences[canonical]
		occurrences[canonical] = occurrence + 1
		fingerprint := sha256.Sum256([]byte(canonical + "\x1e" + strconv.Itoa(occurrence)))
		row.Fingerprint = fmt.Sprintf("%x", fingerprint)
		rows = append(rows, row)
		totalAmountCents += row.AmountCents
	}
	if len(rows) == 0 {
		return nil, 0, errors.New("the CSV file does not contain any toll rows")
	}
	return rows, totalAmountCents, nil
}

func parseTollRecord(record []string) (repository.TollImportRow, error) {
	postingDate, err := requiredDate(record[0], "PostingDate", "01/02/06")
	if err != nil {
		return repository.TollImportRow{}, err
	}
	invoiceDate, err := requiredDate(record[1], "InvoiceDate", "2006-01-02")
	if err != nil {
		return repository.TollImportRow{}, err
	}
	entryDate, err := optionalDate(record[10], "Entry_Date", "01/02/06")
	if err != nil {
		return repository.TollImportRow{}, err
	}
	entryTime, err := optionalClock(record[11], "Entry_Time")
	if err != nil {
		return repository.TollImportRow{}, err
	}
	if (entryDate == nil) != (entryTime == nil) {
		return repository.TollImportRow{}, errors.New("Entry_Date and Entry_Time must either both be present or both be empty")
	}
	exitDate, err := requiredDate(record[13], "Exit_Date", "01/02/06")
	if err != nil {
		return repository.TollImportRow{}, err
	}
	exitTime, err := requiredClock(record[14], "Exit_Time")
	if err != nil {
		return repository.TollImportRow{}, err
	}
	miles, err := optionalDecimal(record[16], "Miles")
	if err != nil {
		return repository.TollImportRow{}, err
	}
	amountCents, err := parseCents(record[17])
	if err != nil {
		return repository.TollImportRow{}, fmt.Errorf("Toll_Amount %w", err)
	}

	required := []struct {
		name  string
		value string
	}{
		{"CustID", record[2]}, {"Source", record[3]}, {"ReadType", record[4]},
		{"ETagID_Plate", record[6]}, {"EquipID", record[7]}, {"Agency", record[8]},
		{"Exit_Plaza", record[12]}, {"Toll_Class", record[15]},
	}
	for _, field := range required {
		if field.value == "" {
			return repository.TollImportRow{}, fmt.Errorf("%s is required", field.name)
		}
	}

	return repository.TollImportRow{
		PostingDate: postingDate, InvoiceDate: invoiceDate,
		CustomerID: record[2], Source: record[3], ReadType: record[4],
		PrePassTagID: optionalCSVString(record[5]), TransponderOrPlate: record[6],
		EquipmentUnit: record[7], Agency: record[8], EntryPlaza: optionalCSVString(record[9]),
		EntryDate: entryDate, EntryTime: entryTime, ExitPlaza: record[12],
		ExitDate: exitDate, ExitTime: exitTime, TollClass: record[15],
		Miles: miles, AmountCents: amountCents,
	}, nil
}

func requiredDate(value, field, layout string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("%s is required", field)
	}
	parsed, err := time.Parse(layout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s has invalid date %q", field, value)
	}
	return parsed, nil
}

func optionalDate(value, field, layout string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := requiredDate(value, field, layout)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func requiredClock(value, field string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if _, err := time.Parse("15:04:05", value); err != nil {
		return "", fmt.Errorf("%s has invalid time %q", field, value)
	}
	return value, nil
}

func optionalClock(value, field string) (*string, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := requiredClock(value, field)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func optionalDecimal(value, field string) (*string, error) {
	if value == "" {
		return nil, nil
	}
	normalized := strings.ReplaceAll(value, ",", "")
	if strings.HasPrefix(normalized, "(") && strings.HasSuffix(normalized, ")") {
		normalized = "-" + strings.TrimSuffix(strings.TrimPrefix(normalized, "("), ")")
	}
	parsed, err := strconv.ParseFloat(normalized, 64)
	if err != nil || parsed != parsed || parsed > 1.7976931348623157e+308 || parsed < -1.7976931348623157e+308 {
		return nil, fmt.Errorf("%s has invalid number %q", field, value)
	}
	return &normalized, nil
}

func parseCents(value string) (int64, error) {
	normalized := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "$", ""), ",", ""))
	negative := false
	if strings.HasPrefix(normalized, "(") && strings.HasSuffix(normalized, ")") {
		negative = true
		normalized = strings.TrimSuffix(strings.TrimPrefix(normalized, "("), ")")
	} else if strings.HasPrefix(normalized, "-") {
		negative = true
		normalized = strings.TrimPrefix(normalized, "-")
	} else {
		normalized = strings.TrimPrefix(normalized, "+")
	}
	parts := strings.Split(normalized, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("has invalid amount %q", value)
	}
	dollars, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("has invalid amount %q", value)
	}
	cents := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 2 {
			return 0, fmt.Errorf("has invalid amount %q", value)
		}
		fraction := parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("has invalid amount %q", value)
		}
	}
	total := dollars*100 + cents
	if negative {
		total = -total
	}
	return total, nil
}

func recordIsBlank(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func optionalCSVString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
