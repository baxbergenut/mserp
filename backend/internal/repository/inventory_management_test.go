package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInventoryManagementDatabase(t *testing.T) {
	withInventoryDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		repo := NewInventoryRepository(pool)
		truck := inventoryID()
		if _, err := pool.Exec(ctx, `INSERT INTO trucks(id,unit_number) VALUES($1,'REPLACE-022')`, truck); err != nil {
			t.Fatal(err)
		}
		save := func(collection string, v InventoryRecord) InventoryRecord {
			t.Helper()
			out, err := repo.Save(ctx, collection, InventorySave{RequestID: inventoryID(), Record: v}, actor)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		location := "019a0000-0000-4000-8000-000000000001"
		old := save("assets", InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "tablet", IMEI: "000000000000101", Model: "Galaxy Tab A7 Lite SM-T227U 32GB Gray", TruckID: truck, Status: "active", Condition: "good", EquipmentState: "verified_installed", Active: true}})
		spare := save("assets", InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "tablet", IMEI: "000000000000102", LocationID: location, Status: "spare", Condition: "good", Active: true}})
		line := save("lines", InventoryRecord{ID: inventoryID(), Data: InventoryFields{WirelessNumber: "9045550101", AssetID: old.ID, Status: "active", Custodian: "SOURCE COMPANY LABEL", Active: true}})
		input := InventoryOperation{RequestID: inventoryID(), Reason: "Replace damaged tablet", EffectiveDate: "2026-01-01", ExpectedLinks: map[string][]string{old.ID: {line.ID}, spare.ID: {}}}
		released := old
		released.Data.TruckID = ""
		released.Data.LocationID = location
		released.Data.EquipmentState = "unknown"
		released.Data.Condition = "damaged"
		assigned := spare
		assigned.Data.LocationID = ""
		assigned.Data.TruckID = truck
		assigned.Data.Status = "active"
		assigned.Data.EquipmentState = "unknown"
		movedLine := line
		movedLine.Data.AssetID = spare.ID
		input.Changes = []InventoryChange{{"assets", released}, {"assets", assigned}, {"lines", movedLine}}
		stale := input
		stale.RequestID = inventoryID()
		stale.Changes = append([]InventoryChange(nil), input.Changes...)
		stale.Changes[2].Record.Version++
		if _, err := repo.ApplyOperation(ctx, stale, actor); !errors.Is(err, ErrInventoryConflict) {
			t.Fatal("stale replacement accepted", err)
		}
		originalRecords, _ := repo.List(ctx, "assets")
		for _, v := range originalRecords {
			if v.ID == old.ID && v.Data.TruckID != truck {
				t.Fatal("failed operation moved source")
			}
		}
		unseen := input
		unseen.RequestID = inventoryID()
		unseen.ExpectedLinks = nil
		if _, err := repo.ApplyOperation(ctx, unseen, actor); !errors.Is(err, ErrInventoryConflict) {
			t.Fatal("unreviewed dependent link accepted", err)
		}
		result, err := repo.ApplyOperation(ctx, input, actor)
		if err != nil {
			t.Fatal(err)
		}
		again, err := repo.ApplyOperation(ctx, input, actor)
		if err != nil || len(again) != len(result) {
			t.Fatal("replay failed", err)
		}
		input.Reason = "different bytes"
		if _, err = repo.ApplyOperation(ctx, input, actor); !errors.Is(err, ErrInventoryConflict) {
			t.Fatal("changed retry accepted", err)
		}
		histories, err := repo.History(ctx, "assets", old.ID)
		if err != nil || len(histories) != 2 {
			t.Fatal("operation not audited exactly once", len(histories), err)
		}
		var before InventoryRecord
		if err = json.Unmarshal(histories[0].Before, &before); err != nil || before.Data.TruckID != truck || histories[0].Reason != "Replace damaged tablet" || histories[0].EffectiveDate != "2026-01-01" {
			t.Fatal("original assignment or reason lost", histories[0], err)
		}
		lines, _ := repo.List(ctx, "lines")
		if len(lines) != 1 || lines[0].Data.AssetID != spare.ID || lines[0].Data.Custodian != "SOURCE COMPANY LABEL" {
			t.Fatal("line source information changed", lines)
		}
		for _, change := range result {
			if change.Record.ID == old.ID {
				old = change.Record
			}
			if change.Record.ID == spare.ID {
				spare = change.Record
			}
		}
		old.Data.Model = "Galaxy Tab A7 Lite"
		old.Data.ModelNumber = "SM-T227U"
		old.Data.StorageGB = "32"
		old.Data.Color = "Gray"
		old.Data.CaseColor = "Blue"
		old = save("assets", old)
		if old.Data.OriginalModel != "Galaxy Tab A7 Lite SM-T227U 32GB Gray" {
			t.Fatal("source description lost", old)
		}
		old.Data.OriginalModel = "spoofed"
		old = save("assets", old)
		if old.Data.OriginalModel == "spoofed" {
			t.Fatal("source description client writable")
		}
		if _, err = pool.Exec(ctx, `UPDATE inventory_assets SET data=data-'color'-'modelNumber'-'storageGb'-'caseColor' WHERE id=$1`, old.ID); err != nil {
			t.Fatal(err)
		}
		var preserved string
		if err = pool.QueryRow(ctx, `SELECT data->>'color' FROM inventory_assets WHERE id=$1`, old.ID).Scan(&preserved); err != nil || preserved != "Gray" {
			t.Fatal("older writer erased structured details", preserved, err)
		}
		old.Data.CaseColor = ""
		old = save("assets", old)
		if err = pool.QueryRow(ctx, `SELECT data->>'caseColor' FROM inventory_assets WHERE id=$1`, old.ID).Scan(&preserved); err != nil || preserved != "" {
			t.Fatal("explicit clear was not retained", preserved, err)
		}
		spare.Data.Status = "sold"
		if _, err = repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: spare}, actor); err == nil {
			t.Fatal("sold device retained truck/line")
		}
		if _, err = pool.Exec(ctx, `UPDATE inventory_assets SET data=jsonb_set(data,'{status}','"sold"') WHERE id=$1`, spare.ID); err == nil {
			t.Fatal("database allowed sold device with relationships")
		}
		custodian := save("custodians", InventoryRecord{ID: inventoryID(), Data: InventoryFields{Name: "Fleet custodian", Active: true}})
		old.Data.LocationID = ""
		old.Data.CustodianID = custodian.ID
		old = save("assets", old)
		custodian.Data.Active = false
		if _, err = repo.Save(ctx, "custodians", InventorySave{RequestID: inventoryID(), Record: custodian}, actor); err == nil {
			t.Fatal("custodian with equipment archived")
		}
		directory, err := repo.Directory(ctx, InventoryDirectoryQuery{Collection: "assets", DeviceType: "tablet", Custody: "person", Search: "Gray", CanTrucks: true, PageSize: 25})
		if err != nil || directory.Total != 1 || directory.Records[0].ID != old.ID {
			t.Fatal("directory filters incorrect", directory, err)
		}
		if directory.Context["assets"][0].Data.OriginalModel != "" {
			t.Fatal("reference context includes source descriptions")
		}
	})
}

func TestInventoryGatewayReplacementDatabase(t *testing.T) {
	withInventoryDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		repo := NewInventoryRepository(pool)
		truck := inventoryID()
		pool.Exec(ctx, `INSERT INTO trucks(id,unit_number) VALUES($1,'GATEWAY-REPLACE')`, truck)
		save := func(v InventoryRecord) InventoryRecord {
			t.Helper()
			v, err := repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: v}, actor)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		a := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "samsara_gateway", Serial: "REPLACE-A", TruckID: truck, Condition: "good", Status: "active", Active: true}})
		b := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "samsara_gateway", Serial: "REPLACE-B", LocationID: "019a0000-0000-4000-8000-000000000001", Condition: "good", Status: "spare", Active: true}})
		camera := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "dashcam", Brand: "Samsara", Serial: "REPLACE-CAMERA", GatewayID: a.ID, EquipmentState: "verified_installed", Active: true}})
		oldLocation := b.Data.LocationID
		a.Data.TruckID = ""
		a.Data.LocationID = oldLocation
		b.Data.LocationID = ""
		b.Data.TruckID = truck
		camera.Data.GatewayID = b.ID
		camera.Data.EquipmentState = "unknown"
		input := InventoryOperation{RequestID: inventoryID(), Reason: "Gateway replacement", EffectiveDate: "2026-01-01", ExpectedLinks: map[string][]string{a.ID: {camera.ID}}, Changes: []InventoryChange{{"assets", camera}, {"assets", b}, {"assets", a}}}
		result, err := repo.ApplyOperation(ctx, input, actor)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range result {
			if c.Record.ID == camera.ID && (c.Record.Data.VerifiedAt != "" || c.Record.Data.GatewayID != b.ID) {
				t.Fatal("camera verification carried across replacement")
			}
		}
		events, _ := repo.History(ctx, "assets", camera.ID)
		var before InventoryRecord
		json.Unmarshal(events[0].Before, &before)
		if before.Data.GatewayID != a.ID || before.Data.VerifiedAt == "" {
			t.Fatal("camera before snapshot lost")
		}
		if _, err = pool.Exec(ctx, `UPDATE inventory_assets SET data=jsonb_set(data,'{status}','"sold"') WHERE id=$1`, a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE inventory_assets SET data=jsonb_set(data,'{gatewayId}',to_jsonb($2::text)) WHERE id=$1`, camera.ID, a.ID); err == nil {
			t.Fatal("database allowed a camera to link an unavailable parent")
		}
	})
}

func TestInventoryDeviceDetailValidation(t *testing.T) {
	d := InventoryFields{DeviceType: "tablet", IMEI: "000000000000001", Active: true}
	for _, value := range []string{"0", "-1", "1.5", "65537", "01", "32GB"} {
		d.StorageGB = value
		if validateInventoryDetails(d) == nil {
			t.Fatal("invalid capacity", value)
		}
	}
	d.StorageGB = "32"
	d.Color = "Graphite"
	d.CaseColor = "Blue"
	if err := validateInventoryDetails(d); err != nil {
		t.Fatal(err)
	}
	d.Color = " Gray "
	if validateInventoryDetails(d) == nil {
		t.Fatal("untrimmed color accepted")
	}
}

func TestInventoryDirectoryPagingDatabase(t *testing.T) {
	withInventoryDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		repo := NewInventoryRepository(pool)
		for i := 1; i <= 31; i++ {
			v := InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "phone", AssetLabel: "PHONE-" + strconv.Itoa(i), Model: "Phone " + strconv.Itoa(i), Notes: "Private description", Active: true, Color: "Graphite", Status: "spare", Condition: "good", LocationID: "019a0000-0000-4000-8000-000000000001"}}
			if i == 31 {
				v.Data.Active = false
			}
			if _, err := repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: v}, actor); err != nil {
				t.Fatal(err)
			}
		}
		q := InventoryDirectoryQuery{Collection: "assets", DeviceType: "phone", Search: "Graphite", Custody: "available", Condition: "good", Lifecycle: "spare", PageSize: 25, Page: 2, Sort: "Make / Model"}
		result, err := repo.Directory(ctx, q)
		if err != nil || result.Total != 30 || len(result.Records) != 5 || result.Records[0].Data.Model != "Phone 26" {
			t.Fatal("paging/natural sorting", result, err)
		}
		q.Active = "record:archived"
		q.Custody = ""
		result, err = repo.Directory(ctx, q)
		if err != nil || result.Total != 1 || result.Page != 1 || result.Records[0].Data.Model != "Phone 31" {
			t.Fatal("independent archived/condition filter", result, err)
		}
		for _, v := range result.Context["assets"] {
			if v.Data.Notes != "" {
				t.Fatal("directory context contains full descriptions")
			}
		}
	})
}
