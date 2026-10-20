package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type InventoryDirectoryQuery struct {
	Collection, DeviceType, TruckID, Search, Active, Condition, Lifecycle, Presence, Custody, Location, Brand, Sort, Direction string
	Page, PageSize                                                                                                             int
	CanTrucks                                                                                                                  bool
}
type InventoryDirectory struct {
	Records  []InventoryRecord            `json:"records"`
	Context  map[string][]InventoryRecord `json:"context"`
	Total    int                          `json:"total"`
	Page     int                          `json:"page"`
	PageSize int                          `json:"pageSize"`
}

// A consistent directory snapshot. Large source descriptions and observations
// are fetched only for selected records or an explicitly opened review/editor.
func (r *InventoryRepository) Directory(ctx context.Context, q InventoryDirectoryQuery) (InventoryDirectory, error) {
	result := InventoryDirectory{Records: []InventoryRecord{}, Context: map[string][]InventoryRecord{}}
	if q.Collection != "assets" && q.Collection != "lines" && q.Collection != "locations" && q.Collection != "custodians" {
		return result, inventoryInvalid("Unknown inventory directory")
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize != 25 && q.PageSize != 50 && q.PageSize != 100 {
		q.PageSize = 25
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	all := map[string][]InventoryRecord{}
	for _, collection := range []string{"assets", "lines", "locations", "custodians"} {
		rows, e := tx.Query(ctx, `SELECT id::text,version,data,updated_at FROM `+inventoryTables[collection]+` ORDER BY id`)
		if e != nil {
			return result, e
		}
		all[collection] = []InventoryRecord{}
		for rows.Next() {
			var v InventoryRecord
			if e = rows.Scan(&v.ID, &v.Version, &v.Data, &v.UpdatedAt); e != nil {
				rows.Close()
				return result, e
			}
			all[collection] = append(all[collection], v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
	}
	trucks := map[string]string{}
	if q.CanTrucks {
		rows, e := tx.Query(ctx, `SELECT id::text,unit_number FROM trucks`)
		if e != nil {
			return result, e
		}
		for rows.Next() {
			var id, unit string
			if e = rows.Scan(&id, &unit); e != nil {
				rows.Close()
				return result, e
			}
			trucks[id] = unit
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	assets := map[string]InventoryRecord{}
	lines := map[string]InventoryRecord{}
	names := map[string]string{}
	for _, a := range all["assets"] {
		assets[a.ID] = a
	}
	for _, l := range all["lines"] {
		if l.Data.Active && l.Data.AssetID != "" {
			lines[l.Data.AssetID] = l
		}
	}
	for _, c := range []string{"locations", "custodians"} {
		for _, v := range all[c] {
			names[v.ID] = v.Data.Name
		}
	}
	holder := func(v InventoryRecord) InventoryFields {
		if v.Data.GatewayID != "" {
			return assets[v.Data.GatewayID].Data
		}
		return v.Data
	}
	custody := func(v InventoryRecord) string {
		d := holder(v)
		if d.TruckID != "" {
			return "truck"
		}
		if d.LocationID != "" {
			return "storage"
		}
		if d.CustodianID != "" || d.Custodian != "" {
			return "person"
		}
		return "unknown"
	}
	custodyLabel := func(v InventoryRecord) string {
		d := holder(v)
		if d.TruckID != "" {
			if trucks[d.TruckID] != "" {
				return trucks[d.TruckID]
			}
			return "Assigned Truck"
		}
		if d.LocationID != "" {
			return names[d.LocationID]
		}
		if d.CustodianID != "" {
			return names[d.CustodianID]
		}
		if d.Custodian != "" {
			return d.Custodian
		}
		return "Unknown"
	}
	presence := func(v InventoryRecord) string {
		if v.Data.EquipmentState == "missing" {
			return "missing"
		}
		if holder(v).TruckID != "" {
			return inventoryDefault(v.Data.EquipmentState, "unknown")
		}
		if custody(v) != "unknown" {
			return "not_installed"
		}
		return "unassigned"
	}
	value := func(v InventoryRecord) string {
		d := v.Data
		switch q.Sort {
		case "Truck / Custody":
			return custodyLabel(v)
		case "Condition":
			return inventoryDefault(d.Condition, "unknown")
		case "Installation":
			return presence(v)
		case "IMEI", "IMEI / Serial":
			return inventoryDefault(d.IMEI, d.Serial)
		case "Gateway Serial":
			return d.Serial
		case "Linked Gateway":
			return assets[d.GatewayID].Data.Serial
		case "Linked Dashcam":
			for _, a := range all["assets"] {
				if a.Data.Active && a.Data.GatewayID == v.ID {
					return inventoryDefault(a.Data.IMEI, a.Data.Serial)
				}
			}
		case "Brand":
			return d.Brand
		case "Model":
			return d.Model
		case "Wireless Number":
			return d.WirelessNumber
		case "Device IMEI":
			return inventoryDefault(assets[d.AssetID].Data.IMEI, assets[d.AssetID].Data.Serial)
		case "Tablet Number", "Phone Number":
			return lines[v.ID].Data.WirelessNumber
		case "Carrier User":
			return d.Custodian
		case "Carrier Status":
			return d.Status
		case "Plan":
			return d.Plan
		case "Contract End":
			return d.ContractEnd
		case "Location", "Custodian":
			return d.Name
		case "Notes":
			return d.Notes
		}
		return strings.Join([]string{d.Make, d.Brand, d.Model, d.ModelNumber, d.StorageGB, d.Color}, " ")
	}
	matched := []InventoryRecord{}
	for _, v := range all[q.Collection] {
		d := v.Data
		h := holder(v)
		if q.Collection == "assets" {
			if q.DeviceType == "equipment" {
				if h.TruckID == "" || d.DeviceType == "other" || d.DeviceType == "pt30" {
					continue
				}
			} else if d.DeviceType != q.DeviceType {
				continue
			}
		}
		if q.TruckID != "" && h.TruckID != q.TruckID {
			continue
		}
		if q.Active == "record:archived" {
			if d.Active {
				continue
			}
		} else if q.Active != "all" && !d.Active {
			continue
		}
		if q.Condition != "" && inventoryDefault(d.Condition, "unknown") != q.Condition || q.Lifecycle != "" && inventoryDefault(d.Status, "unknown") != q.Lifecycle || q.Presence != "" && presence(v) != q.Presence || q.Brand != "" && d.Brand != q.Brand || q.Location != "" && h.LocationID != q.Location {
			continue
		}
		if q.Custody == "available" {
			if inventoryUnavailable(d) || d.Condition != "good" || (d.Status != "active" && d.Status != "spare") || d.GatewayID != "" || (custody(v) != "storage" && custody(v) != "person") {
				continue
			}
		} else if q.Custody != "" && custody(v) != q.Custody {
			continue
		}
		if q.Search != "" {
			raw, _ := json.Marshal(d)
			haystack := strings.ToLower(string(raw) + " " + custodyLabel(v) + " " + lines[v.ID].Data.WirelessNumber + " " + assets[d.GatewayID].Data.Serial + " " + assets[d.AssetID].Data.IMEI)
			digits := strings.NewReplacer(" ", "", "+1", "", "(", "", ")", "", "-", "").Replace(q.Search)
			phoneMatch := len(digits) >= 7 && (strings.Contains(lines[v.ID].Data.WirelessNumber, digits) || strings.Contains(d.WirelessNumber, digits))
			if !strings.Contains(haystack, strings.ToLower(q.Search)) && !phoneMatch {
				continue
			}
		}
		matched = append(matched, v)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		a, b := strings.ToLower(value(matched[i])), strings.ToLower(value(matched[j]))
		if a == b {
			a, b = matched[i].ID, matched[j].ID
		}
		if q.Direction == "desc" {
			return inventoryNaturalLess(b, a)
		}
		return inventoryNaturalLess(a, b)
	})
	result.Total = len(matched)
	result.PageSize = q.PageSize
	maxPage := (result.Total + q.PageSize - 1) / q.PageSize
	if maxPage < 1 {
		maxPage = 1
	}
	if q.Page > maxPage {
		q.Page = maxPage
	}
	result.Page = q.Page
	start := (q.Page - 1) * q.PageSize
	end := start + q.PageSize
	if end > len(matched) {
		end = len(matched)
	}
	result.Records = matched[start:end]
	for collection, records := range all {
		result.Context[collection] = make([]InventoryRecord, 0, len(records))
		for _, v := range records {
			v.Data.Notes = ""
			v.Data.Source = ""
			v.Data.OriginalModel = ""
			result.Context[collection] = append(result.Context[collection], v)
		}
	}
	return result, nil
}
func inventoryDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// Compare digit runs without converting identifiers to numeric storage values.
func inventoryNaturalLess(a, b string) bool {
	for len(a) > 0 && len(b) > 0 {
		if a[0] >= '0' && a[0] <= '9' && b[0] >= '0' && b[0] <= '9' {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			x, y := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if len(x) != len(y) {
				return len(x) < len(y)
			}
			if x != y {
				return x < y
			}
			a, b = a[i:], b[j:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}
