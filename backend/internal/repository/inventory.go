package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInventoryConflict = errors.New("Inventory changed or this request ID was already used; reload and review the current record")

type InventoryValidationError struct{ Message string }

func (e *InventoryValidationError) Error() string { return e.Message }
func inventoryInvalid(message string) error       { return &InventoryValidationError{message} }

// Descriptive fields are versioned together; relationships also have database FKs.
// All identifiers remain text, including values consisting entirely of digits.
type InventoryFields struct {
	Name                string `json:"name,omitempty"`
	DeviceType          string `json:"deviceType,omitempty"`
	IMEI                string `json:"imei,omitempty"`
	Serial              string `json:"serial,omitempty"`
	AssetLabel          string `json:"assetLabel,omitempty"`
	Make                string `json:"make,omitempty"`
	Model               string `json:"model,omitempty"`
	Condition           string `json:"condition,omitempty"`
	Status              string `json:"status,omitempty"`
	Ownership           string `json:"ownership,omitempty"`
	TruckID             string `json:"truckId,omitempty"`
	LocationID          string `json:"locationId,omitempty"`
	Custodian           string `json:"custodian,omitempty"`
	GatewayID           string `json:"gatewayId,omitempty"`
	AssetID             string `json:"assetId,omitempty"`
	WirelessNumber      string `json:"wirelessNumber,omitempty"`
	Carrier             string `json:"carrier,omitempty"`
	ICCID               string `json:"iccid,omitempty"`
	Plan                string `json:"plan,omitempty"`
	ActivationDate      string `json:"activationDate,omitempty"`
	ContractStart       string `json:"contractStart,omitempty"`
	ContractEnd         string `json:"contractEnd,omitempty"`
	SourceUpdated       string `json:"sourceUpdated,omitempty"`
	Unit                string `json:"unit,omitempty"`
	Reusable            bool   `json:"reusable,omitempty"`
	BatchYear           string `json:"batchYear,omitempty"`
	SupplyID            string `json:"supplyId,omitempty"`
	EquipmentState      string `json:"equipmentState,omitempty"`
	Sent                bool   `json:"sent,omitempty"`
	Attached            bool   `json:"attached,omitempty"`
	SentDate            string `json:"sentDate,omitempty"`
	AttachedDate        string `json:"attachedDate,omitempty"`
	VerifiedAt          string `json:"verifiedAt,omitempty"`
	VerifiedBy          string `json:"verifiedBy,omitempty"`
	UnitNumber          string `json:"unitNumber,omitempty"`
	VIN                 string `json:"vin,omitempty"`
	Year                string `json:"year,omitempty"`
	Plate               string `json:"plate,omitempty"`
	PlateState          string `json:"plateState,omitempty"`
	StorageLocation     string `json:"storageLocation,omitempty"`
	Usage               string `json:"usage,omitempty"`
	RegistrationURL     string `json:"registrationUrl,omitempty"`
	ProviderEnvironment string `json:"providerEnvironment,omitempty"`
	ProviderAccount     string `json:"providerAccount,omitempty"`
	Notes               string `json:"notes,omitempty"`
	Source              string `json:"source,omitempty"`
	Active              bool   `json:"active"`
}
type InventoryRecord struct {
	ID        string          `json:"id"`
	Version   int64           `json:"version"`
	Data      InventoryFields `json:"data"`
	UpdatedAt time.Time       `json:"updatedAt"`
	UpdatedBy string          `json:"updatedBy"`
}
type InventorySave struct {
	RequestID string          `json:"requestId"`
	Record    InventoryRecord `json:"record"`
}
type InventoryActor struct{ ID, Name string }
type InventoryEvent struct {
	ID         string          `json:"id"`
	Collection string          `json:"collection"`
	RecordID   string          `json:"recordId"`
	Action     string          `json:"action"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
	Actor      string          `json:"actor"`
	CreatedAt  time.Time       `json:"createdAt"`
}
type InventoryRepository struct{ pool *pgxpool.Pool }

func NewInventoryRepository(pool *pgxpool.Pool) *InventoryRepository {
	return &InventoryRepository{pool}
}

var inventoryTables = map[string]string{
	"assets": "inventory_assets", "lines": "inventory_lines", "locations": "inventory_locations",
	"supplies": "inventory_supplies", "equipment": "inventory_equipment", "trailers": "fleet_trailers", "transponder-notes": "inventory_transponder_notes",
}
var inventoryUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var inventoryIMEI = regexp.MustCompile(`^[0-9]{15}$`)
var inventoryPhone = regexp.MustCompile(`^[0-9]{10}$`)
var inventoryVIN = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`)

func inventoryID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func inventoryHash(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// A single, short-lived transaction lock serializes cross-table inventory
// invariants, including line/device swaps and source import commits.
func inventoryLock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(119,1)`)
	return err
}

func (r *InventoryRepository) List(ctx context.Context, collection string) ([]InventoryRecord, error) {
	table, ok := inventoryTables[collection]
	if !ok {
		return nil, inventoryInvalid("Unknown inventory category")
	}
	rows, err := r.pool.Query(ctx, `SELECT r.id::text,r.version,r.data,r.updated_at,coalesce(u.username,'') FROM `+table+` r LEFT JOIN app_users u ON u.id=r.updated_by ORDER BY coalesce(r.data->>'unitNumber',r.data->>'name',r.data->>'imei',r.data->>'serial',''),r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []InventoryRecord{}
	for rows.Next() {
		var v InventoryRecord
		if err = rows.Scan(&v.ID, &v.Version, &v.Data, &v.UpdatedAt, &v.UpdatedBy); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (r *InventoryRepository) History(ctx context.Context, collection, id string) ([]InventoryEvent, error) {
	result := []InventoryEvent{}
	rows, err := r.pool.Query(ctx, `SELECT id::text,collection,record_id::text,action,before_data,after_data,actor_name,created_at FROM inventory_events WHERE ($1='' OR collection=$1) AND ($2='' OR record_id::text=$2) ORDER BY created_at DESC,id DESC LIMIT 500`, collection, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e InventoryEvent
		if err = rows.Scan(&e.ID, &e.Collection, &e.RecordID, &e.Action, &e.Before, &e.After, &e.Actor, &e.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func validateInventory(collection string, v InventoryRecord) error {
	if _, ok := inventoryTables[collection]; !ok {
		return inventoryInvalid("Unknown inventory category")
	}
	if !inventoryUUID.MatchString(v.ID) || v.Version < 0 {
		return inventoryInvalid("Invalid record identity or version")
	}
	b, _ := json.Marshal(v.Data)
	if len(b) > 16000 {
		return inventoryInvalid("Inventory record is too large")
	}
	d := v.Data
	for _, s := range []string{d.TruckID, d.LocationID, d.GatewayID, d.AssetID, d.SupplyID} {
		if s != "" && !inventoryUUID.MatchString(s) {
			return inventoryInvalid("Select a valid linked record")
		}
	}
	if len(d.Notes) > 5000 {
		return inventoryInvalid("Notes must be 5000 characters or fewer")
	}
	for _, s := range []string{d.Name, d.Serial, d.Custodian, d.AssetLabel, d.StorageLocation, d.Usage, d.Make, d.Model, d.Plan, d.ProviderAccount} {
		if len(s) > 250 || s != strings.TrimSpace(s) {
			return inventoryInvalid("Text fields must be trimmed and 250 characters or fewer")
		}
	}
	for _, s := range []string{d.ActivationDate, d.ContractStart, d.ContractEnd, d.SourceUpdated, d.SentDate, d.AttachedDate} {
		if s != "" {
			if _, err := time.Parse(time.DateOnly, s); err != nil {
				return inventoryInvalid("Dates must use YYYY-MM-DD")
			}
		}
	}
	if d.BatchYear != "" {
		y, err := strconv.Atoi(d.BatchYear)
		if err != nil || y < 2000 || y > 2200 {
			return inventoryInvalid("Enter a valid sticker batch year")
		}
	}
	if d.EquipmentState != "" && !slices.Contains([]string{"unknown", "reported_present", "verified_installed", "missing"}, d.EquipmentState) {
		return inventoryInvalid("Unknown equipment state")
	}
	if d.Condition != "" && !slices.Contains([]string{"unknown", "good", "damaged", "repair", "lost", "retired"}, d.Condition) {
		return inventoryInvalid("Unknown condition")
	}
	if collection == "assets" {
		if !slices.Contains([]string{"tablet", "samsara_gateway", "samsara_camera", "tt_camera", "pt30", "other"}, d.DeviceType) {
			return inventoryInvalid("Choose a device type")
		}
		if d.IMEI != "" && !inventoryIMEI.MatchString(d.IMEI) {
			return inventoryInvalid("IMEI must contain exactly 15 digits; review the source instead of guessing")
		}
		if d.IMEI == "" && d.Serial == "" && d.AssetLabel == "" {
			return inventoryInvalid("Record an IMEI, serial, or internal asset label")
		}
		if d.Ownership != "" && !slices.Contains([]string{"owned", "rented", "contracted", "unknown"}, d.Ownership) {
			return inventoryInvalid("Unknown ownership type")
		}
		holders := 0
		for _, s := range []string{d.TruckID, d.LocationID, d.Custodian, d.GatewayID} {
			if s != "" {
				holders++
			}
		}
		if holders > 1 {
			return inventoryInvalid("Choose one truck, custodian, location, or linked gateway")
		}
		if d.GatewayID != "" && d.DeviceType != "samsara_camera" {
			return inventoryInvalid("Only a Samsara camera can link to a gateway")
		}
		if (d.Condition == "retired" || d.Condition == "lost" || !d.Active) && (d.TruckID != "" || d.GatewayID != "") {
			return inventoryInvalid("Release truck/gateway assignments before losing or archiving a device")
		}
	}
	if collection == "lines" {
		if !inventoryPhone.MatchString(d.WirelessNumber) {
			return inventoryInvalid("Wireless number must contain ten digits")
		}
		if d.ICCID != "" {
			if len(d.ICCID) < 18 || len(d.ICCID) > 22 || strings.Trim(d.ICCID, "0123456789") != "" {
				return inventoryInvalid("ICCID must be an 18–22 digit string")
			}
		}
		if d.ContractStart != "" && d.ContractEnd != "" && d.ContractStart > d.ContractEnd {
			return inventoryInvalid("Contract end precedes its start")
		}
	}
	if collection == "supplies" || collection == "locations" || collection == "equipment" {
		if d.Name == "" {
			return inventoryInvalid("A name is required")
		}
	}
	if collection == "supplies" {
		if !slices.Contains([]string{"units", "sets", "pairs"}, d.Unit) {
			return inventoryInvalid("Select units, sets, or pairs")
		}
		if d.Name == "IFTA Sticker" && (d.BatchYear == "" || d.Unit != "pairs" || d.Reusable) {
			return inventoryInvalid("IFTA stickers require an annual batch, pairs, and consumable tracking")
		}
	}
	if collection == "equipment" {
		if d.TruckID == "" {
			return inventoryInvalid("Select a truck")
		}
		if d.EquipmentState == "" {
			return inventoryInvalid("Select an equipment state")
		}
		if d.AttachedDate != "" && !d.Attached || d.SentDate != "" && !d.Sent {
			return inventoryInvalid("A recorded date requires the corresponding sent/attached status")
		}
		if d.SentDate != "" && d.AttachedDate != "" && d.SentDate > d.AttachedDate {
			return inventoryInvalid("Attached date precedes sent date")
		}
	}
	if collection == "trailers" {
		if d.UnitNumber == "" || d.UnitNumber != strings.TrimSpace(d.UnitNumber) || len(d.UnitNumber) > 50 {
			return inventoryInvalid("Enter a trailer unit number")
		}
		if d.VIN != "" && !inventoryVIN.MatchString(d.VIN) {
			return inventoryInvalid("Trailer VIN must be 17 uppercase VIN characters")
		}
		if d.Year != "" {
			y, err := strconv.Atoi(d.Year)
			if err != nil || y < 1900 || y > 2200 {
				return inventoryInvalid("Enter a valid trailer year")
			}
		}
		if d.RegistrationURL != "" {
			u, err := url.Parse(d.RegistrationURL)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
				return inventoryInvalid("Registration link must be an HTTPS URL")
			}
		}
		if d.TruckID != "" || d.GatewayID != "" {
			return inventoryInvalid("Stored trailers do not have dispatch assignments")
		}
	}
	if collection == "transponder-notes" && (d.Serial == "" || d.ProviderAccount == "" || d.ProviderEnvironment == "") {
		return inventoryInvalid("Record the provider environment, account, and exact transponder tag")
	}
	return nil
}

func inventoryReadTx(ctx context.Context, tx pgx.Tx, collection, id string) (InventoryRecord, error) {
	table, ok := inventoryTables[collection]
	if !ok {
		return InventoryRecord{}, inventoryInvalid("Unknown category")
	}
	var v InventoryRecord
	err := tx.QueryRow(ctx, `SELECT id::text,version,data,updated_at FROM `+table+` WHERE id=$1`, id).Scan(&v.ID, &v.Version, &v.Data, &v.UpdatedAt)
	return v, err
}
func (r *InventoryRepository) Save(ctx context.Context, collection string, input InventorySave, actor InventoryActor) (InventoryRecord, error) {
	if !inventoryUUID.MatchString(input.RequestID) {
		return InventoryRecord{}, inventoryInvalid("A unique request ID is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InventoryRecord{}, err
	}
	defer tx.Rollback(ctx)
	if err = inventoryLock(ctx, tx); err != nil {
		return InventoryRecord{}, err
	}
	hash := inventoryHash(struct {
		Collection string
		Input      InventorySave
	}{collection, input})
	var oldHash string
	var saved InventoryRecord
	err = tx.QueryRow(ctx, `SELECT request_hash,after_data FROM inventory_events WHERE id=$1`, input.RequestID).Scan(&oldHash, &saved)
	if err == nil {
		if oldHash != hash {
			return saved, ErrInventoryConflict
		}
		return saved, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return saved, err
	}
	saved, err = inventorySaveTx(ctx, tx, collection, input.Record, actor, input.RequestID, hash, false)
	if err != nil {
		return saved, err
	}
	return saved, tx.Commit(ctx)
}

func inventorySaveTx(ctx context.Context, tx pgx.Tx, collection string, v InventoryRecord, actor InventoryActor, requestID, hash string, importing bool) (InventoryRecord, error) {
	if err := validateInventory(collection, v); err != nil {
		return v, err
	}
	old, err := inventoryReadTx(ctx, tx, collection, v.ID)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return v, err
	}
	if (!exists && v.Version != 0) || (exists && old.Version != v.Version) {
		return v, ErrInventoryConflict
	}
	if !importing {
		v.Data.Source = old.Data.Source
	}
	// Verification identity/time are always server-owned. Imported observations
	// are only reported/unknown and never manufacture a physical verification.
	v.Data.VerifiedAt = old.Data.VerifiedAt
	v.Data.VerifiedBy = old.Data.VerifiedBy
	if v.Data.EquipmentState == "verified_installed" {
		if importing {
			return v, inventoryInvalid("Imported observations cannot establish verified installation")
		}
		if old.Data.EquipmentState != "verified_installed" || old.Data.TruckID != v.Data.TruckID || old.Data.GatewayID != v.Data.GatewayID || old.Data.Custodian != v.Data.Custodian || old.Data.LocationID != v.Data.LocationID {
			v.Data.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
			v.Data.VerifiedBy = actor.Name
		}
	} else {
		v.Data.VerifiedAt = ""
		v.Data.VerifiedBy = ""
	}
	if v.Data.GatewayID != "" {
		gateway, e := inventoryReadTx(ctx, tx, "assets", v.Data.GatewayID)
		if e != nil {
			return v, inventoryInvalid("Linked gateway was not found")
		}
		if gateway.ID == v.ID || gateway.Data.DeviceType != "samsara_gateway" || !gateway.Data.Active || gateway.Data.Condition == "lost" || gateway.Data.Condition == "retired" {
			return v, inventoryInvalid("Select an active Samsara gateway")
		}
	}
	if collection == "assets" && exists {
		var linked bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_assets WHERE gateway_id=$1 AND coalesce((data->>'active')::boolean,true)) OR EXISTS(SELECT 1 FROM inventory_lines WHERE asset_id=$1 AND coalesce((data->>'active')::boolean,true))`, v.ID).Scan(&linked)
		if err != nil {
			return v, err
		}
		if linked && (!v.Data.Active || v.Data.Condition == "lost" || v.Data.Condition == "retired" || old.Data.DeviceType != v.Data.DeviceType) {
			return v, inventoryInvalid("Release linked cameras and service lines before archiving, losing, or changing this device type")
		}
	}
	if v.Data.AssetID != "" {
		asset, e := inventoryReadTx(ctx, tx, "assets", v.Data.AssetID)
		if e != nil || !asset.Data.Active || !slices.Contains([]string{"tablet", "other"}, asset.Data.DeviceType) {
			return v, inventoryInvalid("Select an active tablet or phone/other device")
		}
	}
	if v.Data.LocationID != "" {
		location, e := inventoryReadTx(ctx, tx, "locations", v.Data.LocationID)
		if e != nil || !location.Data.Active {
			return v, inventoryInvalid("Select an active storage location")
		}
	}
	if v.Data.SupplyID != "" {
		supply, e := inventoryReadTx(ctx, tx, "supplies", v.Data.SupplyID)
		if e != nil || !supply.Data.Active {
			return v, inventoryInvalid("Select an active supply item")
		}
		if collection == "equipment" && (supply.Data.Name != v.Data.Name || supply.Data.BatchYear != v.Data.BatchYear) {
			return v, inventoryInvalid("Equipment name/year must match the selected supply batch")
		}
	}
	if collection == "locations" && !v.Data.Active && exists {
		var used bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_assets WHERE location_id=$1) OR EXISTS(SELECT 1 FROM inventory_stock_movements WHERE from_holder=$2 OR to_holder=$2)`, v.ID, "location:"+v.ID).Scan(&used)
		if err != nil {
			return v, err
		}
		if used {
			return v, inventoryInvalid("Locations with assets or stock history must remain available")
		}
	}
	if collection == "supplies" && exists && (v.Data.Unit != old.Data.Unit || v.Data.Reusable != old.Data.Reusable || v.Data.BatchYear != old.Data.BatchYear) {
		var used bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_stock_movements WHERE supply_id=$1)`, v.ID).Scan(&used)
		if err != nil {
			return v, err
		}
		if used {
			return v, inventoryInvalid("Create another supply item to change units, reuse rules, or year after stock movements")
		}
	}
	if collection == "supplies" && exists && !v.Data.Active {
		var held bool
		err = tx.QueryRow(ctx, `WITH entries AS (SELECT to_holder holder,quantity FROM inventory_stock_movements WHERE supply_id=$1 UNION ALL SELECT from_holder,-quantity FROM inventory_stock_movements WHERE supply_id=$1) SELECT EXISTS(SELECT 1 FROM entries WHERE holder LIKE 'location:%' OR holder LIKE 'truck:%' OR holder LIKE 'custodian:%' GROUP BY holder HAVING sum(quantity)>0)`, v.ID).Scan(&held)
		if err != nil {
			return v, err
		}
		if held {
			return v, inventoryInvalid("Resolve remaining storage and issued stock before archiving this supply item")
		}
	}
	v.Version++
	v.UpdatedAt = time.Now().UTC()
	v.UpdatedBy = actor.Name
	data, err := json.Marshal(v.Data)
	if err != nil {
		return v, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO `+inventoryTables[collection]+`(id,version,data,updated_by,updated_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET version=excluded.version,data=excluded.data,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, v.ID, v.Version, data, actor.ID, v.UpdatedAt)
	if err != nil {
		return v, err
	}
	action := "edit"
	if !exists {
		action = "create"
	} else if old.Data.Active && !v.Data.Active {
		action = "archive"
	} else if old.Data.TruckID != v.Data.TruckID || old.Data.Custodian != v.Data.Custodian || old.Data.LocationID != v.Data.LocationID || old.Data.GatewayID != v.Data.GatewayID || old.Data.AssetID != v.Data.AssetID {
		action = "transfer"
	}
	var before any
	if exists {
		before = old
	}
	_, err = tx.Exec(ctx, `INSERT INTO inventory_events(id,request_hash,collection,record_id,action,before_data,after_data,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, requestID, hash, collection, v.ID, action, before, v, actor.ID, actor.Name)
	return v, err
}

type InventoryMovement struct {
	ID            string    `json:"id"`
	SupplyID      string    `json:"supplyId"`
	Kind          string    `json:"kind"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	Quantity      int64     `json:"quantity"`
	Reason        string    `json:"reason"`
	PhysicalCount bool      `json:"physicalCount"`
	ReversesID    string    `json:"reversesId,omitempty"`
	Actor         string    `json:"actor"`
	CreatedAt     time.Time `json:"createdAt"`
}
type InventoryBalance struct {
	SupplyID string `json:"supplyId"`
	Holder   string `json:"holder"`
	Quantity int64  `json:"quantity"`
	Verified bool   `json:"verified"`
}

func (r *InventoryRepository) Stock(ctx context.Context) ([]InventoryBalance, error) {
	rows, err := r.pool.Query(ctx, `WITH entries AS (SELECT supply_id,to_holder holder,quantity FROM inventory_stock_movements UNION ALL SELECT supply_id,from_holder,-quantity FROM inventory_stock_movements) SELECT supply_id::text,holder,sum(quantity)::bigint,EXISTS(SELECT 1 FROM inventory_stock_movements m WHERE m.supply_id=e.supply_id AND (m.to_holder=e.holder OR m.from_holder=e.holder) AND m.physical_count) FROM entries e WHERE holder LIKE 'location:%' OR holder LIKE 'truck:%' OR holder LIKE 'custodian:%' GROUP BY supply_id,holder ORDER BY supply_id,holder`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []InventoryBalance{}
	for rows.Next() {
		var v InventoryBalance
		if err = rows.Scan(&v.SupplyID, &v.Holder, &v.Quantity, &v.Verified); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (r *InventoryRepository) Movements(ctx context.Context) ([]InventoryMovement, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text,supply_id::text,kind,from_holder,to_holder,quantity,reason,physical_count,coalesce(reverses_id::text,''),actor_name,created_at FROM inventory_stock_movements ORDER BY created_at DESC,id DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []InventoryMovement{}
	for rows.Next() {
		var v InventoryMovement
		if err = rows.Scan(&v.ID, &v.SupplyID, &v.Kind, &v.From, &v.To, &v.Quantity, &v.Reason, &v.PhysicalCount, &v.ReversesID, &v.Actor, &v.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func inventoryHolder(ctx context.Context, tx pgx.Tx, holder string) error {
	if slices.Contains([]string{"external", "consumed", "lost", "damaged"}, holder) {
		return nil
	}
	kind, id, ok := strings.Cut(holder, ":")
	if !ok {
		return inventoryInvalid("Select a stock holder")
	}
	if kind == "custodian" {
		if len(strings.TrimSpace(id)) == 0 || len(id) > 150 || id != strings.TrimSpace(id) {
			return inventoryInvalid("Enter a custodian name")
		}
		return nil
	}
	if !inventoryUUID.MatchString(id) {
		return inventoryInvalid("Select a valid stock location or truck")
	}
	var exists bool
	switch kind {
	case "location":
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_locations WHERE id=$1 AND (data->>'active')::boolean)`, id).Scan(&exists)
		if err != nil {
			return err
		}
	case "truck":
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trucks WHERE id=$1)`, id).Scan(&exists)
		if err != nil {
			return err
		}
	default:
		return inventoryInvalid("Unknown stock holder")
	}
	if !exists {
		return inventoryInvalid("Stock holder was not found or is archived")
	}
	return nil
}
func inventoryAvailable(ctx context.Context, tx pgx.Tx, supply, holder string) (int64, error) {
	var qty int64
	err := tx.QueryRow(ctx, `SELECT coalesce(sum(CASE WHEN to_holder=$2 THEN quantity ELSE 0 END)-sum(CASE WHEN from_holder=$2 THEN quantity ELSE 0 END),0)::bigint FROM inventory_stock_movements WHERE supply_id=$1`, supply, holder).Scan(&qty)
	return qty, err
}
func (r *InventoryRepository) Move(ctx context.Context, m InventoryMovement, actor InventoryActor) error {
	if !inventoryUUID.MatchString(m.ID) || !inventoryUUID.MatchString(m.SupplyID) || m.Quantity < 0 || m.Quantity == 0 && (!m.PhysicalCount || (m.Kind != "opening" && m.Kind != "adjustment")) || m.Quantity > 1000000 || strings.TrimSpace(m.Reason) == "" || len(m.Reason) > 1000 {
		return inventoryInvalid("Enter an item, whole quantity (up to 1000000), and reason; zero is allowed only for a verified count")
	}
	m.Actor = ""
	m.CreatedAt = time.Time{}
	hash := inventoryHash(m)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = inventoryLock(ctx, tx); err != nil {
		return err
	}
	var prior string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM inventory_stock_movements WHERE id=$1`, m.ID).Scan(&prior)
	if err == nil {
		if prior != hash {
			return ErrInventoryConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	supply, err := inventoryReadTx(ctx, tx, "supplies", m.SupplyID)
	if err != nil {
		return err
	}
	if !supply.Data.Active {
		return inventoryInvalid("Supply item is archived")
	}
	if err = inventoryHolder(ctx, tx, m.From); err != nil {
		return err
	}
	if err = inventoryHolder(ctx, tx, m.To); err != nil {
		return err
	}
	storage := func(s string) bool { return strings.HasPrefix(s, "location:") }
	recipient := func(s string) bool { return strings.HasPrefix(s, "truck:") || strings.HasPrefix(s, "custodian:") }
	real := func(s string) bool { return storage(s) || recipient(s) }
	valid := false
	switch m.Kind {
	case "opening", "receipt":
		valid = m.From == "external" && storage(m.To)
	case "issue":
		valid = storage(m.From) && recipient(m.To)
	case "return":
		valid = recipient(m.From) && storage(m.To) && supply.Data.Reusable
	case "transfer":
		valid = real(m.From) && real(m.To)
	case "use":
		valid = recipient(m.From) && m.To == "consumed" && !supply.Data.Reusable
	case "lost", "damaged":
		valid = real(m.From) && m.To == m.Kind
	case "adjustment":
		valid = (m.From == "external" && storage(m.To)) || (storage(m.From) && m.To == "external")
	case "reversal":
		if !inventoryUUID.MatchString(m.ReversesID) {
			return inventoryInvalid("Select a movement to reverse")
		}
		var old InventoryMovement
		err = tx.QueryRow(ctx, `SELECT supply_id::text,from_holder,to_holder,quantity,kind FROM inventory_stock_movements WHERE id=$1`, m.ReversesID).Scan(&old.SupplyID, &old.From, &old.To, &old.Quantity, &old.Kind)
		if err != nil {
			return err
		}
		valid = old.Kind != "reversal" && m.SupplyID == old.SupplyID && m.From == old.To && m.To == old.From && m.Quantity == old.Quantity
	}
	if !valid || m.From == m.To {
		return inventoryInvalid("Invalid movement path for this operation")
	}
	if m.Kind != "reversal" && m.ReversesID != "" {
		return inventoryInvalid("Only a reversal can reference another movement")
	}
	if m.PhysicalCount && (m.Kind != "opening" && m.Kind != "adjustment") {
		return inventoryInvalid("Physical opening counts belong to opening/adjustment entries")
	}
	if m.Kind == "opening" && !m.PhysicalCount {
		return inventoryInvalid("An opening balance requires a verified physical count")
	}
	if real(m.From) {
		available, e := inventoryAvailable(ctx, tx, m.SupplyID, m.From)
		if e != nil {
			return e
		}
		if available < m.Quantity {
			return inventoryInvalid("Insufficient stock at the selected source")
		}
	}
	if m.Kind == "issue" {
		var verified bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_stock_movements WHERE supply_id=$1 AND (to_holder=$2 OR from_holder=$2) AND physical_count)`, m.SupplyID, m.From).Scan(&verified)
		if err != nil {
			return err
		}
		if !verified {
			return inventoryInvalid("Verify the physical opening count before issuing this stock")
		}
	}
	if m.Kind == "opening" {
		var exists bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_stock_movements WHERE supply_id=$1 AND (from_holder=$2 OR to_holder=$2))`, m.SupplyID, m.To).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			return inventoryInvalid("This stock already has history; use a counted adjustment instead")
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO inventory_stock_movements(id,request_hash,supply_id,kind,from_holder,to_holder,quantity,reason,physical_count,reverses_id,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,'')::uuid,$11,$12)`, m.ID, hash, m.SupplyID, m.Kind, m.From, m.To, m.Quantity, m.Reason, m.PhysicalCount, m.ReversesID, actor.ID, actor.Name)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
