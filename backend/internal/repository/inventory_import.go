package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Snapshot is a lossless, private export of displayed sheet values. Original
// strings, including whitespace/leading zeros, remain in Snapshot after review.
type InventorySheet struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
}
type InventorySnapshot struct {
	SpreadsheetID string           `json:"spreadsheetId"`
	CapturedAt    string           `json:"capturedAt"`
	Sheets        []InventorySheet `json:"sheets"`
}
type InventoryImportRow struct {
	Key        string          `json:"key"`
	Sources    []string        `json:"sources"`
	Collection string          `json:"collection"`
	Record     InventoryRecord `json:"record"`
	Issues     []string        `json:"issues"`
	Decision   string          `json:"decision"`
	Reason     string          `json:"reason"`
	Reviewed   bool            `json:"reviewed"`
}
type InventoryImport struct {
	ID            string               `json:"id"`
	Version       int64                `json:"version"`
	Status        string               `json:"status"`
	SourceHash    string               `json:"sourceHash"`
	SpreadsheetID string               `json:"spreadsheetId"`
	CapturedAt    string               `json:"capturedAt"`
	Rows          []InventoryImportRow `json:"rows"`
	CreatedAt     time.Time            `json:"createdAt"`
}
type InventoryImportReview struct {
	Version int64                `json:"version"`
	Rows    []InventoryImportRow `json:"rows"`
}

func inventoryCell(row []string, index int) string {
	if index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}
func inventorySourceDate(s string) string {
	for _, layout := range []string{"2006-01-02", "1/2/2006", "01/02/2006", "Jan 02, 2006", "Jan 2, 2006"} {
		if d, e := time.Parse(layout, strings.TrimSpace(s)); e == nil {
			return d.Format(time.DateOnly)
		}
	}
	return ""
}

var inventoryDigits = regexp.MustCompile(`[^0-9]`)

func inventorySourcePhone(s string) string {
	digits := inventoryDigits.ReplaceAllString(s, "")
	if len(digits) == 11 && digits[0] == '1' {
		digits = digits[1:]
	}
	return digits
}

func (r *InventoryRepository) PreviewImport(ctx context.Context, snapshot InventorySnapshot, actor InventoryActor) (InventoryImport, error) {
	var result InventoryImport
	captured, err := time.Parse(time.RFC3339, snapshot.CapturedAt)
	if err != nil || captured.After(time.Now().Add(time.Minute*5)) || snapshot.SpreadsheetID == "" || len(snapshot.Sheets) == 0 || len(snapshot.Sheets) > 50 {
		return result, inventoryInvalid("Snapshot requires a source spreadsheet ID, capture timestamp, and sheets")
	}
	bytes, _ := json.Marshal(snapshot)
	if len(bytes) > 10<<20 {
		return result, inventoryInvalid("Snapshot exceeds 10 MB")
	}
	hash := inventoryHash(snapshot)
	if existing, e := r.getImportByHash(ctx, hash); e == nil {
		return existing, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return result, e
	}
	result = InventoryImport{ID: inventoryID(), Version: 1, Status: "preview", SourceHash: hash, SpreadsheetID: snapshot.SpreadsheetID, CapturedAt: snapshot.CapturedAt, Rows: []InventoryImportRow{}, CreatedAt: time.Now().UTC()}
	trucks := map[string][]string{}
	vins := map[string][]string{}
	rows, err := r.pool.Query(ctx, `SELECT id::text,unit_number,coalesce(vin,'') FROM trucks`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var id, unit, vin string
		if err = rows.Scan(&id, &unit, &vin); err != nil {
			rows.Close()
			return result, err
		}
		trucks[unit] = append(trucks[unit], id)
		if vin != "" {
			vins[strings.ToUpper(vin)] = append(vins[strings.ToUpper(vin)], id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	resolve := func(unit, vin string) (string, []string) {
		if vin != "" {
			if match := vins[strings.ToUpper(vin)]; len(match) == 1 {
				return match[0], nil
			}
		}
		if match := trucks[unit]; len(match) == 1 {
			return match[0], nil
		}
		if unit == "" && vin == "" {
			return "", nil
		}
		return "", []string{"Truck requires an explicit reviewed mapping: " + unit + " / " + vin}
	}
	existing := map[string]InventoryRecord{}
	for _, collection := range []string{"assets", "lines", "equipment", "trailers", "transponder-notes"} {
		records, e := r.List(ctx, collection)
		if e != nil {
			return result, e
		}
		for _, v := range records {
			existing[inventoryImportKey(collection, v.Data)] = v
			if collection == "assets" {
				if v.Data.IMEI != "" {
					existing[collection+":imei:"+v.Data.IMEI] = v
				}
				if v.Data.Serial != "" {
					existing[collection+":serial:"+strings.ToLower(v.Data.Serial)] = v
				}
				if v.Data.AssetLabel != "" {
					existing[collection+":label:"+strings.ToLower(v.Data.AssetLabel)] = v
				}
			}
		}
	}
	indexes := map[string]int{}
	add := func(collection string, d InventoryFields, source string, issues []string) string {
		key := inventoryImportKey(collection, d)
		if collection == "equipment" && d.TruckID == "" {
			key += ":" + source
		}
		if key == collection+":" || key == "" {
			key = collection + ":" + source
		}
		if index, ok := indexes[key]; ok {
			item := &result.Rows[index]
			for _, priorSource := range item.Sources {
				if strings.Split(priorSource, "!")[0] == strings.Split(source, "!")[0] && priorSource != source {
					item.Issues = append(item.Issues, "Duplicate identifier within one source tab; verify that these rows describe the same physical device or line")
					break
				}
			}
			item.Sources = append(item.Sources, source)
			item.Issues = append(item.Issues, issues...)
			// Merge complementary facts only. Conflicting values remain visibly blocked
			// until a reviewer explicitly resolves the row.
			a, _ := json.Marshal(item.Record.Data)
			b, _ := json.Marshal(d)
			var old, new map[string]any
			_ = json.Unmarshal(a, &old)
			_ = json.Unmarshal(b, &new)
			for field, value := range new {
				if field == "active" || field == "source" {
					continue
				}
				prior, present := old[field]
				if !present || prior == "" {
					old[field] = value
				} else if prior != value {
					item.Issues = append(item.Issues, "Conflicting "+field+" between sources; select the reviewed value")
				}
			}
			merged, _ := json.Marshal(old)
			_ = json.Unmarshal(merged, &item.Record.Data)
			item.Record.Data.Source = strings.Join(item.Sources, ", ")
			return item.Record.ID
		}
		v := InventoryRecord{ID: inventoryID(), Data: d}
		if current, ok := existing[key]; ok {
			v.ID = current.ID
			v.Version = current.Version
			issues = append(issues, "Existing MSERP record: review source values before merging")
		}
		d.Source = source
		v.Data = d
		indexes[key] = len(result.Rows)
		result.Rows = append(result.Rows, InventoryImportRow{Key: key, Sources: []string{source}, Collection: collection, Record: v, Issues: issues, Decision: "include"})
		return v.ID
	}
	archive := func(source, reason string) {
		result.Rows = append(result.Rows, InventoryImportRow{Key: "archive:" + source, Sources: []string{source}, Issues: []string{}, Decision: "archive", Reason: reason, Reviewed: true})
	}
	// Latest dated carrier export first. Older snapshots supplement empty fields,
	// but disagreements remain review issues instead of silently overriding values.
	sheets := append([]InventorySheet(nil), snapshot.Sheets...)
	slices.SortStableFunc(sheets, func(a, b InventorySheet) int {
		rank := func(s string) int {
			switch s {
			case "Updated ATT inventory":
				return 0
			case "ATT Inventory":
				return 1
			case "Truck list":
				return 2
			case "Samsara Inventory":
				return 3
			default:
				return 4
			}
		}
		return rank(a.Name) - rank(b.Name)
	})
	seenSheets := map[string]bool{}
	for _, sheet := range sheets {
		if seenSheets[sheet.Name] {
			return result, inventoryInvalid("Snapshot contains duplicate sheet names")
		}
		seenSheets[sheet.Name] = true
		for index, row := range sheet.Rows {
			if index == 0 {
				continue
			}
			nonempty := false
			for _, value := range row {
				if strings.TrimSpace(value) != "" {
					nonempty = true
					break
				}
			}
			if !nonempty {
				continue
			}
			source := fmt.Sprintf("%s!%d", sheet.Name, index+1)
			switch sheet.Name {
			case "Updated ATT inventory", "ATT Inventory":
				newer := sheet.Name == "Updated ATT inventory"
				phoneCol, imeiCol, makeCol, modelCol, statusCol, typeCol, dateCol, activationCol := 5, 14, 15, 16, 7, 13, 0, 33
				if newer {
					phoneCol, imeiCol, makeCol, modelCol, statusCol, typeCol, dateCol, activationCol = 3, 8, 9, 10, 5, 7, 0, 15
				}
				imei := inventoryCell(row, imeiCol)
				if imei == "" && inventoryCell(row, phoneCol) == "" {
					archive(source, "Blank carrier placeholder retained in snapshot")
					continue
				}
				deviceType := "other"
				if strings.EqualFold(inventoryCell(row, typeCol), "Tablet") {
					deviceType = "tablet"
				}
				assetID := ""
				issues := []string{}
				if imei != "" {
					assetID = add("assets", InventoryFields{DeviceType: deviceType, IMEI: imei, Make: inventoryCell(row, makeCol), Model: inventoryCell(row, modelCol), Condition: "unknown", EquipmentState: "unknown", Active: true}, source, nil)
				}
				phone := inventorySourcePhone(inventoryCell(row, phoneCol))
				d := InventoryFields{WirelessNumber: phone, Carrier: "AT&T", AssetID: assetID, Status: inventoryCell(row, statusCol), ActivationDate: inventorySourceDate(inventoryCell(row, activationCol)), SourceUpdated: inventorySourceDate(inventoryCell(row, dateCol)), Active: true}
				if newer {
					d.ContractStart = inventorySourceDate(inventoryCell(row, 17))
					d.ContractEnd = inventorySourceDate(inventoryCell(row, 18))
					d.Custodian = inventoryCell(row, 4)
				} else {
					d.ContractStart = inventorySourceDate(inventoryCell(row, 37))
					d.ContractEnd = inventorySourceDate(inventoryCell(row, 38))
					d.ICCID = inventoryCell(row, 26)
					d.Plan = inventoryCell(row, 27)
					d.Custodian = inventoryCell(row, 6)
					if d.ICCID != "" {
						issues = append(issues, "Legacy sheet ICCID may have lost numeric precision; verify against carrier before including")
					}
				}
				add("lines", d, source, issues)
			case "Truck list":
				unit, vin := inventoryCell(row, 1), inventoryCell(row, 4)
				truck, issues := resolve(unit, vin)
				if serial := inventoryCell(row, 11); serial != "" {
					add("assets", InventoryFields{DeviceType: "samsara_gateway", Serial: serial, TruckID: truck, Condition: "unknown", EquipmentState: "reported_present", Active: true}, source, issues)
				}
				if imei := inventoryCell(row, 12); imei != "" {
					assetID := add("assets", InventoryFields{DeviceType: "tablet", IMEI: imei, TruckID: truck, Condition: "unknown", EquipmentState: "reported_present", Active: true}, source, issues)
					if phone := inventorySourcePhone(inventoryCell(row, 13)); phone != "" {
						add("lines", InventoryFields{WirelessNumber: phone, AssetID: assetID, Carrier: "AT&T", Active: true}, source, nil)
					}
				}
				archive(source+":profile", "Truck identity, ownership, status and compliance fields retained in snapshot; existing MSERP records remain authoritative")
			case "Samsara Inventory":
				truck, issues := resolve(inventoryCell(row, 2), "") // # prefixes are deliberately not stripped.
				gateway := ""
				if serial := inventoryCell(row, 1); serial != "" {
					gateway = add("assets", InventoryFields{DeviceType: "samsara_gateway", Serial: serial, TruckID: truck, Condition: "unknown", EquipmentState: "reported_present", Active: true}, source, issues)
				}
				if serial := inventoryCell(row, 0); serial != "" {
					add("assets", InventoryFields{DeviceType: "samsara_camera", Serial: serial, GatewayID: gateway, Status: inventoryCell(row, 3), Condition: "unknown", EquipmentState: "reported_present", Active: true}, source, nil)
				}
				if len(row) > 7 {
					archive(source+":orders", "Order/license side table retained as procurement references; not physical stock")
				}
			case "TT DashCam Inventory":
				truck, issues := resolve(inventoryCell(row, 2), "")
				ownership := strings.ToLower(inventoryCell(row, 4))
				if !slices.Contains([]string{"owned", "rented", "contracted"}, ownership) {
					ownership = "unknown"
				}
				add("assets", InventoryFields{DeviceType: "tt_camera", IMEI: inventoryCell(row, 0), TruckID: truck, Ownership: ownership, Condition: "unknown", EquipmentState: "reported_present", Status: inventoryCell(row, 1), Notes: inventoryCell(row, 6), Active: true}, source, issues)
			case "Trailer  list":
				add("trailers", InventoryFields{UnitNumber: inventoryCell(row, 0), RegistrationURL: inventoryCell(row, 1), VIN: strings.ToUpper(inventoryCell(row, 2)), Plate: inventoryCell(row, 3), Year: inventoryCell(row, 4), Make: inventoryCell(row, 5), Usage: inventoryCell(row, 6), StorageLocation: inventoryCell(row, 7), Status: "stored_unused", Condition: "unknown", Notes: "Source availability: " + inventoryCell(row, 8) + ". Requires physical condition review.", Active: true}, source, nil)
			case "Inventory":
				if inventoryCell(row, 0) == "" {
					archive(source, "Blank-unit checklist placeholder; no equipment inferred")
					continue
				}
				truck, issues := resolve(inventoryCell(row, 0), "")
				for _, item := range []struct {
					column int
					name   string
				}{{1, "MC Sticker"}, {2, "IFTA Sticker"}, {3, "PrePass"}, {4, "Samsara DashCam"}, {5, "TT DashCam"}, {6, "PT-30/Protrux"}, {8, "Tablet"}, {12, "Chains"}, {13, "Extinguisher"}} {
					value := strings.ToUpper(inventoryCell(row, item.column))
					state := "unknown"
					if value == "TRUE" {
						state = "reported_present"
					}
					d := InventoryFields{Name: item.name, TruckID: truck, EquipmentState: state, Active: true, Notes: "Legacy checkbox: " + value + ". False/blank is not a verified absence; no quantity or issue date inferred."}
					add("equipment", d, source+fmt.Sprintf(":column%d", item.column+1), issues)
				}
				archive(source+":documents", "Document, permit, readiness and compliance checks retained in archive only")
			case "IFTA 26":
				if inventoryCell(row, 0) == "" && inventoryCell(row, 1) == "" {
					archive(source, "Blank-unit IFTA placeholder; no issuance inferred")
					continue
				}
				truck, issues := resolve(inventoryCell(row, 0), inventoryCell(row, 1))
				add("equipment", InventoryFields{Name: "IFTA Sticker", BatchYear: "2026", TruckID: truck, EquipmentState: "unknown", Sent: strings.EqualFold(inventoryCell(row, 2), "TRUE"), Attached: strings.EqualFold(inventoryCell(row, 3), "TRUE"), Notes: "Imported 2026 sent/attached observations; dates and quantities unknown.", Active: true}, source, issues)
			case "Transponder ":
				archive(source, "Historical PrePass tags, status and lost/stolen/return notes require exact provider account/environment review; do not import as a second roster")
				for _, column := range []int{2, 4} {
					if serial := inventoryCell(row, column); serial != "" {
						add("transponder-notes", InventoryFields{Serial: serial, Notes: "Source unit: " + inventoryCell(row, 0) + "; status: " + inventoryCell(row, 3) + "; " + inventoryCell(row, 5), Active: true}, source, []string{"Select the verified PrePass environment/account before importing historical notes"})
					}
				}
			case "Oil change":
				archive(source, "Compare with MSERP preventative maintenance; apply reviewed missing baselines through maintenance, without expense creation")
			default:
				archive(source, "Historical/report data retained in original snapshot; excluded from operational migration")
			}
		}
	}
	// Surface duplicate assignments across different identifiers before commit.
	assignments := map[string]int{}
	for index := range result.Rows {
		item := &result.Rows[index]
		if item.Collection == "" {
			continue
		}
		if e := validateInventory(item.Collection, item.Record); e != nil {
			item.Issues = append(item.Issues, e.Error())
		}
		d := item.Record.Data
		assignment := ""
		if item.Collection == "assets" && d.TruckID != "" && (d.DeviceType == "tablet" || d.DeviceType == "samsara_gateway") {
			assignment = d.DeviceType + ":" + d.TruckID
		}
		if item.Collection == "lines" && d.AssetID != "" {
			assignment = "line:" + d.AssetID
		}
		if assignment != "" {
			if previous, ok := assignments[assignment]; ok {
				item.Issues = append(item.Issues, "Conflicting assignment; review all source identities")
				result.Rows[previous].Issues = append(result.Rows[previous].Issues, "Conflicting assignment; review all source identities")
			} else {
				assignments[assignment] = index
			}
		}
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO inventory_imports(id,snapshot,plan,status,source_hash,created_by) VALUES($1,$2,$3,'preview',$4,$5) ON CONFLICT(source_hash) DO NOTHING`, result.ID, snapshot, result, hash, actor.ID)
	if err != nil {
		return result, err
	}
	return r.getImportByHash(ctx, hash)
}
func inventoryImportKey(collection string, d InventoryFields) string {
	switch collection {
	case "assets":
		if d.IMEI != "" {
			return collection + ":imei:" + d.IMEI
		}
		if d.Serial != "" {
			return collection + ":serial:" + strings.ToLower(d.Serial)
		}
		return collection + ":label:" + strings.ToLower(d.AssetLabel)
	case "lines":
		return collection + ":" + d.WirelessNumber
	case "equipment":
		return collection + ":" + d.TruckID + ":" + d.Name + ":" + d.BatchYear
	case "trailers":
		return collection + ":" + d.UnitNumber
	case "transponder-notes":
		return collection + ":" + d.ProviderEnvironment + ":" + d.ProviderAccount + ":" + d.Serial
	}
	return ""
}
func (r *InventoryRepository) getImportByHash(ctx context.Context, hash string) (InventoryImport, error) {
	var result InventoryImport
	err := r.pool.QueryRow(ctx, `SELECT plan FROM inventory_imports WHERE source_hash=$1`, hash).Scan(&result)
	return result, err
}
func (r *InventoryRepository) ListImports(ctx context.Context) ([]InventoryImport, error) {
	result := []InventoryImport{}
	rows, err := r.pool.Query(ctx, `SELECT plan FROM inventory_imports ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v InventoryImport
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (r *InventoryRepository) ReviewImport(ctx context.Context, id string, input InventoryImportReview) (InventoryImport, error) {
	var result InventoryImport
	if !inventoryUUID.MatchString(id) {
		return result, inventoryInvalid("Invalid import identity")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT plan FROM inventory_imports WHERE id=$1 FOR UPDATE`, id).Scan(&result)
	if err != nil {
		return result, err
	}
	if result.Status != "preview" || result.Version != input.Version {
		return result, ErrInventoryConflict
	}
	if len(input.Rows) != len(result.Rows) {
		return result, inventoryInvalid("Every source outcome must remain in the review")
	}
	for index, row := range input.Rows {
		old := result.Rows[index]
		if row.Key != old.Key || row.Collection != old.Collection || row.Record.ID != old.Record.ID || row.Record.Version != old.Record.Version {
			return result, inventoryInvalid("Source/target identities cannot change during review; edit fields or explicitly exclude the row")
		}
		if !slices.Contains([]string{"include", "exclude", "archive"}, row.Decision) {
			return result, inventoryInvalid("Choose include, exclude, or archive")
		}
		if old.Collection == "" && row.Decision == "include" {
			return result, inventoryInvalid("Archived source rows cannot create operational records")
		}
		if row.Decision != "include" && strings.TrimSpace(row.Reason) == "" {
			return result, inventoryInvalid("Excluded/archived rows require a reason")
		}
		row.Sources = old.Sources
		row.Issues = old.Issues
		row.Record.Data.Source = strings.Join(old.Sources, ", ")
		result.Rows[index] = row
	}
	result.Version++
	_, err = tx.Exec(ctx, `UPDATE inventory_imports SET version=$2,plan=$3 WHERE id=$1`, id, result.Version, result)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (r *InventoryRepository) CommitImport(ctx context.Context, id string, version int64, actor InventoryActor) (InventoryImport, error) {
	var result InventoryImport
	if !inventoryUUID.MatchString(id) {
		return result, inventoryInvalid("Invalid import identity")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = inventoryLock(ctx, tx); err != nil {
		return result, err
	}
	err = tx.QueryRow(ctx, `SELECT plan FROM inventory_imports WHERE id=$1 FOR UPDATE`, id).Scan(&result)
	if err != nil {
		return result, err
	}
	if result.Status == "committed" {
		return result, nil
	}
	if result.Version != version {
		return result, ErrInventoryConflict
	}
	for _, row := range result.Rows {
		if !row.Reviewed {
			return result, inventoryInvalid("Review every operational row before committing")
		}
		if len(row.Issues) > 0 && strings.TrimSpace(row.Reason) == "" {
			return result, inventoryInvalid("Every flagged row needs an explicit resolution or exclusion reason")
		}
	}
	// Parents before children, independent of the workbook's tab order.
	ordered := append([]InventoryImportRow(nil), result.Rows...)
	rank := func(row InventoryImportRow) int {
		switch row.Collection {
		case "assets":
			if row.Record.Data.GatewayID != "" {
				return 2
			}
			return 1
		case "lines":
			return 3
		default:
			return 4
		}
	}
	slices.SortStableFunc(ordered, func(a, b InventoryImportRow) int { return rank(a) - rank(b) })
	for _, row := range ordered {
		if row.Decision != "include" {
			continue
		}
		requestID := inventoryID()
		_, err = inventorySaveTx(ctx, tx, row.Collection, row.Record, actor, requestID, inventoryHash(row), true)
		if err != nil {
			return result, fmt.Errorf("%s: %w", strings.Join(row.Sources, ", "), err)
		}
	}
	result.Status = "committed"
	result.Version++
	_, err = tx.Exec(ctx, `UPDATE inventory_imports SET status='committed',version=$2,plan=$3,committed_at=now() WHERE id=$1`, id, result.Version, result)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
