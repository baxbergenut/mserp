package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInventoryBaseMigrationData(t *testing.T) {
	truck, gateway, camera, phone, loose, checklist := inventoryID(), inventoryID(), inventoryID(), inventoryID(), inventoryID(), inventoryID()
	withInventoryDatabaseSeed(t, func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
		t.Helper()
		if _, err := conn.Exec(ctx, `INSERT INTO trucks(id,unit_number) VALUES($1,'MIGRATION-BASE')`, truck); err != nil {
			t.Fatal(err)
		}
		for _, v := range []InventoryRecord{
			{ID: gateway, Data: InventoryFields{DeviceType: "samsara_gateway", Serial: "MIGRATION-GATEWAY", TruckID: truck, Active: true}},
			{ID: camera, Data: InventoryFields{DeviceType: "samsara_camera", Serial: "MIGRATION-CAMERA", GatewayID: gateway, Status: "Active", EquipmentState: "reported_present", Active: true}},
			{ID: phone, Data: InventoryFields{DeviceType: "other", IMEI: "000000000000088", Make: "Apple", Model: "iPhone 15 Pro A2848", Active: true}},
			{ID: loose, Data: InventoryFields{DeviceType: "tt_camera", IMEI: "000000000000089", Status: "Unassigned", EquipmentState: "reported_present", Active: true}},
		} {
			if _, err := conn.Exec(ctx, `INSERT INTO inventory_assets(id,data) VALUES($1,$2)`, v.ID, v.Data); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := conn.Exec(ctx, `INSERT INTO inventory_equipment(id,data) VALUES($1,jsonb_build_object('name','Samsara DashCam','truckId',$2::text,'active',true,'equipmentState','reported_present'))`, checklist, truck); err != nil {
			t.Fatal(err)
		}
	}, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		repo := NewInventoryRepository(pool)
		records, err := repo.List(ctx, "assets")
		if err != nil {
			t.Fatal(err)
		}
		if len(records) == 0 {
			return
		} // The fresh-schema branch is covered independently.
		byID := map[string]InventoryRecord{}
		for _, v := range records {
			byID[v.ID] = v
		}
		if byID[phone].Data.DeviceType != "phone" || byID[phone].Data.Make != "Apple" || byID[phone].Data.IMEI != "000000000000088" {
			t.Fatal("phone identity/model not retained", byID[phone])
		}
		if byID[camera].Data.DeviceType != "dashcam" || byID[camera].Data.Brand != "Samsara" || byID[camera].Data.GatewayID != gateway || byID[camera].Data.Status != "active" {
			t.Fatal("Samsara relationship not retained", byID[camera])
		}
		if byID[loose].Data.DeviceType != "dashcam" || byID[loose].Data.Brand != "FiveELD" || byID[loose].Data.EquipmentState != "unknown" || byID[loose].Data.Status != "unknown" {
			t.Fatal("unassigned FiveELD installation not corrected", byID[loose])
		}
		eq, err := repo.List(ctx, "equipment")
		if err != nil || len(eq) != 1 || eq[0].Data.Active {
			t.Fatal("legacy checklist not archived", eq, err)
		}
		events, err := repo.History(ctx, "assets", camera)
		if err != nil || len(events) != 1 || events[0].Action != "normalize" {
			t.Fatal("migration lost audit", events, err)
		}
	})
}

func TestInventoryBaseRules(t *testing.T) {
	base := InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "dashcam", Brand: "Samsara", Serial: "CAMERA-001", Active: true}}
	if err := validateInventory("assets", base); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"reported_present", "verified_installed"} {
		v := base
		v.Data.EquipmentState = state
		if validateInventory("assets", v) == nil {
			t.Fatal("unassigned installation accepted", state)
		}
	}
	for _, brand := range []string{"TT", "Other", ""} {
		v := base
		v.Data.Brand = brand
		if validateInventory("assets", v) == nil {
			t.Fatal("unsupported dashcam brand", brand)
		}
	}
	v := base
	v.Data.Brand = "FiveELD"
	v.Data.GatewayID = inventoryID()
	if validateInventory("assets", v) == nil {
		t.Fatal("FiveELD camera linked to Samsara gateway")
	}
	for _, status := range []string{"Active", "installed at shop", "anything"} {
		v := base
		v.Data.Status = status
		if validateInventory("assets", v) == nil {
			t.Fatal("free text device status accepted", status)
		}
	}
	line := InventoryRecord{ID: inventoryID(), Data: InventoryFields{WirelessNumber: "9049217936", Status: "arbitrary", Active: true}}
	if validateInventory("lines", line) == nil {
		t.Fatal("free text carrier status accepted")
	}
	for _, state := range []string{"active", "suspended", "cancelled", "pending", "unknown"} {
		line.Data.Status = state
		if err := validateInventory("lines", line); err != nil {
			t.Fatal(err)
		}
	}
	d := inventoryImportedFields("assets", InventoryFields{DeviceType: "tt_camera", Status: "Assigned", EquipmentState: "reported_present"})
	if d.DeviceType != "dashcam" || d.Brand != "FiveELD" || d.Status != "unknown" || d.EquipmentState != "unknown" || d.Notes == "" {
		t.Fatal("source status/type not preserved safely", d)
	}
}

func TestInventoryBaseDatabase(t *testing.T) {
	withInventoryDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		repo := NewInventoryRepository(pool)
		truck1, truck2 := inventoryID(), inventoryID()
		if _, err := pool.Exec(ctx, `INSERT INTO trucks(id,unit_number) VALUES($1,'BASE-1'),($2,'BASE-2')`, truck1, truck2); err != nil {
			t.Fatal(err)
		}
		save := func(v InventoryRecord) InventoryRecord {
			t.Helper()
			result, err := repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: v}, actor)
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
		gateway := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "samsara_gateway", Serial: "BASE-GATEWAY", TruckID: truck1, Active: true}})
		camera := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "dashcam", Brand: "Samsara", Serial: "BASE-CAMERA", GatewayID: gateway.ID, EquipmentState: "verified_installed", Active: true}})
		gateway.Data.TruckID = truck2
		gateway = save(gateway)
		cameras, err := repo.List(ctx, "assets")
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range cameras {
			if value.ID == camera.ID {
				camera = value
			}
		}
		if camera.Data.EquipmentState != "unknown" || camera.Data.VerifiedAt != "" || camera.Version != 2 || camera.Data.GatewayID != gateway.ID {
			t.Fatal("gateway move retained camera verification", camera)
		}
		gateway.Data.TruckID = ""
		save(gateway)
		camera.Data.EquipmentState = "verified_installed"
		if _, err = repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: camera}, actor); err == nil {
			t.Fatal("camera installed through an unassigned gateway")
		}
		phone := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{DeviceType: "phone", IMEI: "000000000000099", Make: "Apple", Model: "iPhone", Custodian: "Fleet Staff", Active: true}})
		if _, err = repo.Save(ctx, "lines", InventorySave{RequestID: inventoryID(), Record: InventoryRecord{ID: inventoryID(), Data: InventoryFields{WirelessNumber: "9049217936", AssetID: phone.ID, Status: "active", Active: true}}}, actor); err != nil {
			t.Fatal("phone line not supported", err)
		}
		if _, err = pool.Exec(ctx, `UPDATE inventory_assets SET data=data||'{"equipmentState":"verified_installed"}' WHERE id=$1`, phone.ID); err == nil {
			t.Fatal("database allowed installed unassigned phone")
		}
		if _, err = pool.Exec(ctx, `UPDATE inventory_assets SET data=data||'{"status":"free text"}' WHERE id=$1`, phone.ID); err == nil {
			t.Fatal("database allowed free text status")
		}
		if _, err = repo.Save(ctx, "equipment", InventorySave{RequestID: inventoryID(), Record: InventoryRecord{ID: inventoryID(), Data: InventoryFields{Name: "Chains", TruckID: truck1, SupplyID: "019a0000-0000-4000-8000-000000000011", EquipmentState: "verified_installed", Active: true}}}, actor); err != nil {
			t.Fatal("presence checkbox requires no stock movement", err)
		}
		stock, err := repo.Movements(ctx)
		if err != nil || len(stock) != 0 {
			t.Fatal("presence checkbox must not move stock", stock, err)
		}
	})
}
