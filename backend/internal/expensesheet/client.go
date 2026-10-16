// Package expensesheet implements the temporary, insert-only Google Sheets backup.
package expensesheet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiURL = "https://sheets.googleapis.com/v4/spreadsheets/"
const metadataKey = "mserp_expense_export_v1"

var extraHeaders = []string{"MSERP Entry ID", "MSERP Created At", "Work Order ID", "Service Date", "Odometer (mi)", "Vendor", "Service Location", "Service Details", "Oil Grade", "Work Order Number", "Work Order Status", "Charged Driver ID", "Owner ID", "MSERP Paid", "MSERP Category"}

type Client struct {
	HTTP    *http.Client
	baseURL string
}
type Tab struct {
	ID                      int
	Title                   string
	Columns, Rows, IDColumn int
	Compact                 bool
}
type spreadsheet struct {
	Sheets []struct {
		Properties struct {
			SheetID        int `json:"sheetId"`
			Title          string
			GridProperties struct {
				RowCount    int
				ColumnCount int
			}
		}
	}
}
type valueRange struct{ Values [][]any }
type Entry struct {
	ID, Spreadsheet string
	SheetID         int
	Payload         json.RawMessage
}

func (c *Client) request(ctx context.Context, method, path string, body, result any) error {
	var data io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		data = bytes.NewReader(b)
	}
	base := c.baseURL
	if base == "" {
		base = apiURL
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, data)
	if err != nil {
		return errors.New("invalid Sheets request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("Sheets request failed; retry will check the atomic receipt")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Sheets API returned HTTP %d; no response body logged", resp.StatusCode)
	}
	if result != nil {
		d := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
		d.UseNumber()
		if err = d.Decode(result); err != nil {
			return errors.New("invalid or oversized Sheets response")
		}
	}
	return nil
}
func column(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}
func quoted(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func (c *Client) values(ctx context.Context, spreadsheetID, title, area string) ([][]any, error) {
	var r valueRange
	err := c.request(ctx, http.MethodGet, url.PathEscape(spreadsheetID)+"/values/"+url.PathEscape(quoted(title)+"!"+area)+"?valueRenderOption=UNFORMATTED_VALUE", nil, &r)
	return r.Values, err
}
func text(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func at(rows [][]any, row, col int) string {
	if row >= len(rows) || col >= len(rows[row]) {
		return ""
	}
	return text(rows[row][col])
}
func baseHeaders(hr bool) []string {
	if hr {
		return []string{"Company", "Week", "Date", "Unit #", "Driver", "Amount", "Payment Type", "Expense Type", "Description", "Who will cover", "Paid By", "Manager Verification", "Accounting Verification"}
	}
	return []string{"", "Category", "Week", "Date", "Unit #", "Driver", "Amount", "Payment Type", "Expense Type", "Reference #", "Description", "Who will cover", "Paid By", "Manager Verification", "Accounting Verification"}
}

// Prepare verifies pinned sheet IDs and original headers. Initialization only appends
// dedicated columns; it never rewrites an existing source field or record.
func (c *Client) Prepare(ctx context.Context, spreadsheetID string, maintenanceID, hrID, safetyID int, initialize, apply bool) (map[int]Tab, error) {
	var doc spreadsheet
	if err := c.request(ctx, http.MethodGet, url.PathEscape(spreadsheetID)+"?fields=sheets(properties)", nil, &doc); err != nil {
		return nil, err
	}
	tabs := map[int]Tab{}
	requests := []any{}
	for _, sheet := range doc.Sheets {
		prop := sheet.Properties
		if prop.SheetID != maintenanceID && prop.SheetID != hrID && prop.SheetID != safetyID {
			continue
		}
		t := Tab{ID: prop.SheetID, Title: prop.Title, Columns: prop.GridProperties.ColumnCount, Rows: prop.GridProperties.RowCount, Compact: prop.SheetID != maintenanceID, IDColumn: 19}
		if t.Compact {
			t.IDColumn = 27
		}
		width := t.IDColumn + len(extraHeaders)
		readWidth := min(t.Columns, width)
		rows, err := c.values(ctx, spreadsheetID, t.Title, "A1:"+column(readWidth)+"1")
		if err != nil {
			return nil, err
		}
		for i, h := range baseHeaders(t.Compact) {
			if i == 0 && !t.Compact {
				continue
			}
			if at(rows, 0, i) != h {
				return nil, fmt.Errorf("sheet %d original header %s changed", t.ID, column(i+1))
			}
		}
		ready := true
		for i, h := range extraHeaders {
			v := at(rows, 0, t.IDColumn+i)
			if v != h {
				ready = false
			}
			if v != "" && v != h {
				return nil, fmt.Errorf("sheet %d export columns already contain other headers", t.ID)
			}
		}
		if !ready && !initialize {
			return nil, errors.New("export columns not initialized; run with -initialize -apply first")
		}
		if !ready {
			if t.Columns > t.IDColumn { // Existing blank headers are not proof the whole columns are unused.
				existing, err := c.values(ctx, spreadsheetID, t.Title, column(t.IDColumn+1)+"1:"+column(min(t.Columns, width))+strconv.Itoa(t.Rows))
				if err != nil {
					return nil, err
				}
				for _, row := range existing {
					for _, v := range row {
						if text(v) != "" {
							return nil, errors.New("reserved export columns contain data; initialization stopped")
						}
					}
				}
			}
			if t.Columns < width {
				requests = append(requests, map[string]any{"appendDimension": map[string]any{"sheetId": t.ID, "dimension": "COLUMNS", "length": width - t.Columns}})
			}
			cells := []any{}
			for _, h := range extraHeaders {
				cells = append(cells, stringCell(h))
			}
			requests = append(requests, map[string]any{"updateCells": map[string]any{"range": grid(t.ID, 0, 1, t.IDColumn, width), "rows": []any{map[string]any{"values": cells}}, "fields": "userEnteredValue"}})
		}
		tabs[t.ID] = t
	}
	if len(tabs) != 3 {
		return nil, errors.New("all three configured expense sheet IDs must exist")
	}
	if initialize && apply && len(requests) > 0 {
		if err := c.request(ctx, http.MethodPost, url.PathEscape(spreadsheetID)+":batchUpdate", map[string]any{"requests": requests}, nil); err != nil {
			return nil, err
		}
	}
	return tabs, nil
}
func grid(id, start, end, left, right int) map[string]any {
	return map[string]any{"sheetId": id, "startRowIndex": start, "endRowIndex": end, "startColumnIndex": left, "endColumnIndex": right}
}
func stringCell(v string) map[string]any {
	return map[string]any{"userEnteredValue": map[string]any{"stringValue": v}}
}
func numberCell(v string) map[string]any {
	if v == "" {
		return map[string]any{}
	}
	return map[string]any{"userEnteredValue": map[string]any{"numberValue": json.Number(v)}}
}
func boolCell(v bool) map[string]any {
	return map[string]any{"userEnteredValue": map[string]any{"boolValue": v}}
}
func metadataID(id string) int {
	h := sha256.Sum256([]byte(metadataKey + ":" + id))
	return int(binary.BigEndian.Uint32(h[:4])%0x7fffffff) + 1
}
func (c *Client) received(ctx context.Context, e Entry) (bool, error) {
	var response struct {
		Matched []struct {
			Metadata struct {
				MetadataID                 int
				MetadataKey, MetadataValue string
				Location                   struct{ DimensionRange struct{ SheetID int } }
			} `json:"developerMetadata"`
		} `json:"matchedDeveloperMetadata"`
	}
	body := map[string]any{"dataFilters": []any{map[string]any{"developerMetadataLookup": map[string]any{"metadataId": metadataID(e.ID)}}}}
	if err := c.request(ctx, http.MethodPost, url.PathEscape(e.Spreadsheet)+"/developerMetadata:search", body, &response); err != nil {
		return false, err
	}
	if len(response.Matched) == 0 {
		return false, nil
	}
	m := response.Matched[0].Metadata
	if len(response.Matched) != 1 || m.MetadataKey != metadataKey || m.MetadataValue != e.ID || m.Location.DimensionRange.SheetID != e.SheetID {
		return false, errors.New("Sheets receipt ID collision or different destination; operator review required")
	}
	return true, nil
}

// Deliver puts the row and a unique, deterministic developer-metadata ID in ONE
// atomic Google request. Repeating an uncertain request cannot insert a second row:
// Google's duplicate metadata-ID rejection rolls back the entire repeated batch.
func (c *Client) Deliver(ctx context.Context, e Entry, t Tab) error {
	found, err := c.received(ctx, e)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	data, err := c.values(ctx, e.Spreadsheet, t.Title, "A1:"+column(len(baseHeaders(t.Compact)))+strconv.Itoa(t.Rows))
	if err != nil {
		return err
	}
	last := 0
	indices := []int{3, 4, 5, 6, 8, 10}
	if t.Compact {
		indices = []int{2, 3, 4, 5, 7, 8}
	}
	for row := 1; row < len(data); row++ {
		for _, col := range indices {
			if at(data, row, col) != "" {
				last = row
				break
			}
		}
	}
	row := last + 1 // Insert before unused/preformatted rows, preserving any other columns.
	cells, err := entryCells(e, t)
	if err != nil {
		return err
	}
	requests := []any{map[string]any{"insertDimension": map[string]any{"range": map[string]any{"sheetId": t.ID, "dimension": "ROWS", "startIndex": row, "endIndex": row + 1}, "inheritFromBefore": true}}}
	if row > 1 {
		requests = append(requests, map[string]any{"copyPaste": map[string]any{"source": grid(t.ID, row-1, row, 0, len(baseHeaders(t.Compact))), "destination": grid(t.ID, row, row+1, 0, len(baseHeaders(t.Compact))), "pasteType": "PASTE_FORMAT"}})
	}
	// New custom names/payers may not exist in the old sheet's dropdown catalog.
	// Use literal values and preserve checkbox controls without changing old rows.
	requests = append(requests, map[string]any{"setDataValidation": map[string]any{"range": grid(t.ID, row, row+1, 0, len(cells))}})
	check := 13
	if t.Compact {
		check = 11
	}
	requests = append(requests, map[string]any{"setDataValidation": map[string]any{"range": grid(t.ID, row, row+1, check, check+2), "rule": map[string]any{"condition": map[string]any{"type": "BOOLEAN"}, "strict": true}}})
	requests = append(requests, map[string]any{"updateCells": map[string]any{"range": grid(t.ID, row, row+1, 0, len(cells)), "rows": []any{map[string]any{"values": cells}}, "fields": "userEnteredValue"}}, map[string]any{"createDeveloperMetadata": map[string]any{"developerMetadata": map[string]any{"metadataId": metadataID(e.ID), "metadataKey": metadataKey, "metadataValue": e.ID, "visibility": "DOCUMENT", "location": map[string]any{"dimensionRange": map[string]any{"sheetId": t.ID, "dimension": "ROWS", "startIndex": row, "endIndex": row + 1}}}}})
	if err = c.request(ctx, http.MethodPost, url.PathEscape(e.Spreadsheet)+":batchUpdate", map[string]any{"requests": requests}, nil); err != nil {
		return err
	}
	found, err = c.received(ctx, e)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("Sheets write returned without its receipt")
	}
	return nil
}
func entryCells(e Entry, t Tab) ([]any, error) {
	var snapshot struct {
		Expense     map[string]any
		Maintenance map[string]any
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.UseNumber()
	if err := dec.Decode(&snapshot); err != nil {
		return nil, err
	}
	v := snapshot.Expense
	m := snapshot.Maintenance
	get := func(k string) string { return text(v[k]) }
	mg := func(k string) string { return text(m[k]) }
	if get("id") != e.ID {
		return nil, errors.New("outbox expense identity mismatch")
	}
	date, err := time.Parse(time.DateOnly, get("expense_date"))
	if err != nil {
		return nil, errors.New("expense date required for sheet backup")
	}
	// The week column follows MSERP's Monday-Sunday reporting period.
	monday := date.AddDate(0, 0, -(int(date.Weekday())+6)%7)
	week := monday.Format("01/02") + "-" + monday.AddDate(0, 0, 6).Format("01/02")
	description := get("description")
	if mg("notes") != "" && mg("notes") != description {
		description += "\nWork order notes: " + mg("notes")
	}
	var cells []any
	if t.Compact {
		cells = []any{stringCell(""), stringCell(week), stringCell(date.Format("01/02/2006")), stringCell(get("unit_number")), stringCell(get("driver_name")), numberCell(get("amount")), stringCell(get("payment_type")), stringCell(get("expense_type")), stringCell(description), stringCell(get("covered_by")), stringCell(get("paid_by")), boolCell(v["manager_verified"] == true), boolCell(v["accounting_verified"] == true)}
	} else {
		category := "Other"
		if m != nil {
			category = "Maintenance"
		}
		cells = []any{stringCell(""), stringCell(category), stringCell(week), stringCell(date.Format("01/02/2006")), stringCell(get("unit_number")), stringCell(get("driver_name")), numberCell(get("amount")), stringCell(get("payment_type")), stringCell(get("expense_type")), stringCell(get("reference_number")), stringCell(description), stringCell(get("covered_by")), stringCell(get("paid_by")), boolCell(v["manager_verified"] == true), boolCell(v["accounting_verified"] == true)}
	}
	for len(cells) < t.IDColumn {
		cells = append(cells, map[string]any{})
	}
	services := ""
	if m["services"] != nil {
		b, _ := json.Marshal(m["services"])
		services = string(b)
	}
	for _, s := range []string{e.ID, get("created_at"), mg("id"), mg("service_date"), mg("odometer"), mg("vendor_name"), mg("location"), services, mg("oil_type"), mg("work_order"), mg("status"), get("charge_driver_id"), get("owner_id")} {
		cells = append(cells, stringCell(s))
	}
	cells = append(cells, boolCell(v["driver_settled"] == true))
	cells = append(cells, stringCell(get("category")))
	for _, cell := range cells {
		b, _ := json.Marshal(cell)
		if len(b) > 48000 {
			return nil, errors.New("expense text exceeds a Google Sheets cell limit; operator review required")
		}
	}
	return cells, nil
}
