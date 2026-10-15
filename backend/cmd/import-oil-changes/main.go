// Imports an explicitly reviewed Fleet board Oil change snapshot. Dry run is default.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/repository"
)

const sourceURL = "https://docs.google.com/spreadsheets/d/1RcRmMoM4Jyj1OTAMqDFxawHDiPG8oRVEPzbVOZHmJDk/edit#gid=1621284305"

type snapshot struct {
	Range     string            `json:"range"`
	Values    [][]string        `json:"values"`
	TruckVINs map[string]string `json:"truckVins"`
}
type oilRow struct {
	Row                                   int
	Unit, Date, Oil, Information, Current string
	Miles                                 int
	ID                                    string
	VIN                                   string
}
type result struct {
	Row      int    `json:"row"`
	Unit     string `json:"unit"`
	Action   string `json:"action"`
	Reason   string `json:"reason,omitempty"`
	RecordID string `json:"recordId,omitempty"`
}

func parseSource(s snapshot, today string) ([]oilRow, error) {
	if len(s.Values) < 2 || len(s.Values[0]) < 7 || strings.TrimSpace(s.Values[0][1]) != "Last Change Date" || strings.TrimSpace(s.Values[0][2]) != "Last Change ODO" {
		return nil, errors.New("expected the Oil change A:G export with its original headers")
	}
	rows := []oilRow{}
	seen := map[string]bool{}
	for i, cells := range s.Values[1:] {
		get := func(n int) string {
			if n < len(cells) {
				return strings.TrimSpace(cells[n])
			}
			return ""
		}
		unit := get(0)
		if unit == "" {
			continue
		}
		date, err := time.Parse("1/2/2006", get(1))
		if err != nil {
			date, err = time.Parse("1/2/06", get(1))
		}
		if err != nil || date.Format(time.DateOnly) > today {
			return nil, fmt.Errorf("row %d: invalid or future oil change date", i+2)
		}
		raw := strings.ReplaceAll(get(2), ",", "")
		miles, err := strconv.Atoi(raw)
		if err != nil || miles < 0 || miles > 2000000000 {
			return nil, fmt.Errorf("row %d: invalid last-change odometer", i+2)
		}
		key := strings.ToLower(unit) + "|" + date.Format(time.DateOnly) + "|" + strconv.Itoa(miles)
		if seen[key] {
			return nil, fmt.Errorf("row %d: repeated oil change in source", i+2)
		}
		seen[key] = true
		sum := sha256.Sum256([]byte(sourceURL + "|" + key))
		sum[6] = sum[6]&0x0f | 0x50
		sum[8] = sum[8]&0x3f | 0x80
		h := hex.EncodeToString(sum[:16])
		id := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
		rows = append(rows, oilRow{Row: i + 2, Unit: unit, Date: date.Format(time.DateOnly), Miles: miles, Oil: get(5), Information: get(6), Current: get(3), ID: id, VIN: s.TruckVINs[unit]})
	}
	return rows, nil
}

func run(ctx context.Context, pool *pgxpool.Pool, rows []oilRow, apply bool) ([]result, error) {
	repo := repository.NewMaintenanceRepository(pool)
	out := []result{}
	for _, row := range rows {
		r := result{Row: row.Row, Unit: row.Unit, RecordID: row.ID}
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM maintenance_records WHERE id=$1)", row.ID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			r.Action = "skipped"
			r.Reason = "source event already imported"
			out = append(out, r)
			continue
		}
		var trucks []string
		if err := pool.QueryRow(ctx, "SELECT coalesce(array_agg(id::text),'{}') FROM trucks WHERE lower(btrim(unit_number))=lower($1)", row.Unit).Scan(&trucks); err != nil {
			return nil, err
		}
		if len(trucks) != 1 {
			r.Action = "needs_review"
			r.Reason = "requires one exact truck number match, including leading zeros"
			date, _ := time.Parse(time.DateOnly, row.Date)
			if apply {
				if _, err := repo.Save(ctx, repository.MaintenanceInput{ID: row.ID, SourceUnitNumber: row.Unit, ServiceDate: &date, Odometer: &row.Miles, Location: "unknown", Status: "needs_review", Notes: sourceNotes(row) + "\nImport review: " + r.Reason, WorkOrder: fmt.Sprintf("fleet-board:oil-change:row-%d", row.Row), Actor: "Fleet board import", Services: []repository.MaintenanceWork{{TypeCode: "oil", Name: "Oil change", TrackingCode: "oil"}}}); err != nil {
					return nil, fmt.Errorf("source row %d: %w", row.Row, err)
				}
			}
			out = append(out, r)
			continue
		}
		truck := trucks[0]
		var prior []string
		if err := pool.QueryRow(ctx, `SELECT coalesce(array_agg(id::text),'{}') FROM maintenance_records m WHERE truck_id=$1 AND service_date=$2::date AND status='completed' AND EXISTS(SELECT 1 FROM jsonb_array_elements(services) s WHERE s->>'trackingCode'='oil')`, truck, row.Date).Scan(&prior); err != nil {
			return nil, err
		}
		if len(prior) > 0 {
			r.Action = "skipped"
			r.RecordID = strings.Join(prior, ",")
			r.Reason = "confirmed oil change already exists on this service date"
			out = append(out, r)
			continue
		}
		notes := sourceNotes(row)
		status := "completed"
		readings, err := repo.Readings(ctx, truck)
		if err != nil {
			return nil, err
		}
		for _, reading := range readings {
			if reading.Date < row.Date && reading.Mileage > row.Miles || reading.Date > row.Date && reading.Mileage < row.Miles || reading.Date == row.Date && reading.Mileage != row.Miles {
				status = "needs_review"
				r.Reason = "source odometer conflicts with dated history"
				break
			}
		}
		// An undated invoice with a matching truck/date is a possible duplicate, not proof of a completed service.
		var possible bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM maintenance_records m JOIN expenses e ON e.id=m.expense_id WHERE m.truck_id=$1 AND m.service_date IS NULL AND e.expense_date=$2::date AND m.status='needs_review' AND (e.expense_type ILIKE '%oil%' OR EXISTS(SELECT 1 FROM jsonb_array_elements(m.services) s WHERE s->>'name' ILIKE '%oil%')))`, truck, row.Date).Scan(&possible); err != nil {
			return nil, err
		}
		if possible {
			status = "needs_review"
			r.Reason = "possible matching invoice requires review"
		}
		date, _ := time.Parse(time.DateOnly, row.Date)
		input := repository.MaintenanceInput{ID: row.ID, TruckID: &truck, ServiceDate: &date, Odometer: &row.Miles, Location: "unknown", Status: status, WorkOrder: fmt.Sprintf("fleet-board:oil-change:row-%d", row.Row), Notes: notes, Actor: "Fleet board import", Services: []repository.MaintenanceWork{{TypeCode: "oil", Name: "Oil change", TrackingCode: "oil"}}}
		r.Action = status
		if apply {
			if _, err := repo.Save(ctx, input); err != nil {
				return nil, fmt.Errorf("source row %d: %w", row.Row, err)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func sourceNotes(row oilRow) string {
	return fmt.Sprintf("Imported from Fleet board → Oil change, source row %d.\nSource: %s\nSource truck: %s\nTruck list VIN: %s\nLast Change Date: %s\nLast Change ODO: %d miles\nType of oil: %s\nSource Current ODO (undated; not a dated reading): %s\nSource information: %s\nInvoice amount, vendor and historical driver were not provided; no charge was created.", row.Row, sourceURL, row.Unit, row.VIN, row.Date, row.Miles, row.Oil, row.Current, row.Information)
}

func main() {
	file := flag.String("source", "", "Original bounded Google Sheets range JSON (A:G, headers included)")
	apply := flag.Bool("apply", false, "Write the reviewed import; otherwise report only")
	flag.Parse()
	fail := func(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	raw, err := os.ReadFile(*file)
	if err != nil {
		fail(err)
	}
	var s snapshot
	if err = json.Unmarshal(raw, &s); err != nil {
		fail(err)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		fail(err)
	}
	rows, err := parseSource(s, time.Now().In(ny).Format(time.DateOnly))
	if err != nil {
		fail(err)
	}
	if os.Getenv("DATABASE_URL") == "" {
		fail(errors.New("DATABASE_URL is required"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	report, err := run(ctx, pool, rows, *apply)
	if err != nil {
		fail(err)
	}
	summary := map[string]int{}
	for _, r := range report {
		summary[r.Action]++
	}
	if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"applied": *apply, "source": sourceURL, "summary": summary, "rows": report}); err != nil {
		fail(err)
	}
}
