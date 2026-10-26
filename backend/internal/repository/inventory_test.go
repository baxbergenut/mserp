package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInventorySourcePreview(t *testing.T) {
	path := os.Getenv("MSERP_INVENTORY_SOURCE_SNAPSHOT")
	if path == "" {
		t.Skip("optional private source snapshot not supplied")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var source InventorySnapshot
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	withInventoryDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		// Source identities are isolated fixtures, never production fleet writes.
		for _, sheet := range source.Sheets {
			if sheet.Name != "Truck list" {
				continue
			}
			for index, row := range sheet.Rows {
				if index == 0 {
					continue
				}
				unit, vin := inventoryCell(row, 1), inventoryCell(row, 4)
				if unit == "" {
					continue
				}
				_, err = pool.Exec(ctx, `INSERT INTO trucks(unit_number,vin) SELECT $1,nullif($2,'') WHERE NOT EXISTS(SELECT 1 FROM trucks WHERE unit_number=$1 OR vin=nullif($2,''))`, unit, vin)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		batch, err := NewInventoryRepository(pool).PreviewImport(ctx, source, actor)
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		flagged := 0
		for _, row := range batch.Rows {
			counts[row.Collection]++
			if len(row.Issues) > 0 {
				flagged++
			}
		}
		t.Logf("Source preview: %d tabs, %d outcomes, %d flagged; categories=%v", len(source.Sheets), len(batch.Rows), flagged, counts)
	})
}

func TestInventoryValidation(t *testing.T) {
	base := InventoryRecord{ID: inventoryID(), Data: InventoryFields{Active: true, DeviceType: "tablet", IMEI: "000000000000001"}}
	if e := validateInventory("assets", base); e != nil {
		t.Fatal(e)
	}
	for _, imei := range []string{"123", "1e14", " 354154261264896", "3541542612648960"} {
		v := base
		v.Data.IMEI = imei
		if validateInventory("assets", v) == nil {
			t.Fatalf("accepted malformed IMEI %q", imei)
		}
	}
	v := base
	v.Data.TruckID = inventoryID()
	v.Data.Custodian = "Fleet staff"
	if validateInventory("assets", v) == nil {
		t.Fatal("accepted two holders")
	}
	v = base
	v.Data.DeviceType = "pt30"
	v.Data.GatewayID = inventoryID()
	if validateInventory("assets", v) == nil {
		t.Fatal("noncamera linked to gateway")
	}
	v = base
	v.Data.Condition = "lost"
	v.Data.TruckID = inventoryID()
	if validateInventory("assets", v) == nil {
		t.Fatal("lost device kept truck assignment")
	}
	if inventorySourcePhone("(904) 921-7936") != "9049217936" || inventorySourceDate("4/27/2026") != "2026-04-27" {
		t.Fatal("source normalization failed")
	}
}

func withInventoryDatabase(t *testing.T, check func(*testing.T, context.Context, *pgxpool.Pool, InventoryActor)) {
	withInventoryDatabaseSeed(t, nil, check)
}
func withInventoryDatabaseSeed(t *testing.T, seed func(*testing.T, context.Context, *pgx.Conn), check func(*testing.T, context.Context, *pgxpool.Pool, InventoryActor)) {
	t.Helper()
	dsn := os.Getenv("MSERP_INVENTORY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_INVENTORY_TEST_DATABASE_URL to a disposable localhost _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/119_fleet_inventory.sql")
	if err != nil {
		t.Fatal(err)
	}
	baseMigration, err := os.ReadFile("../../sql/121_inventory_base.sql")
	if err != nil {
		t.Fatal(err)
	}
	statusMigration, err := os.ReadFile("../../sql/150_trailer_statuses.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("inventory_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public`)
			defer admin.Exec(context.Background(), `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`)
			if mode == "fresh" {
				exec(string(source))
			} else {
				normalized := strings.ReplaceAll(string(source), "\r\n", "\n")
				body := strings.ReplaceAll(string(migration), "\r\n", "\n")
				baseBody := strings.ReplaceAll(string(baseMigration), "\r\n", "\n")
				if !strings.Contains(normalized, body) {
					t.Fatal("init schema must contain the exact migration")
				}
				statusBody := strings.ReplaceAll(string(statusMigration), "\r\n", "\n")
				if !strings.Contains(normalized, statusBody) {
					t.Fatal("init must include trailer status migration")
				}
				exec(strings.Replace(strings.Replace(strings.Replace(normalized, statusBody, "", 1), baseBody, "", 1), body, "", 1))
				exec(string(migration))
				if seed != nil {
					seed(t, ctx, admin)
				}
				exec(string(baseMigration))
				exec(string(statusMigration))
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app;GRANT ALL ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			testCfg := cfg.Copy()
			testCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			testCfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, testCfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			var actor InventoryActor
			actor.Name = "Inventory reviewer"
			if e = pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('inventory-reviewer','$2a$10$test-only') RETURNING id::text`).Scan(&actor.ID); e != nil {
				t.Fatal(e)
			}
			check(t, ctx, pool, actor)
		})
	}
}
func TestInventoryDatabase(t *testing.T) {
	withInventoryDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		repo := NewInventoryRepository(pool)
		truck := inventoryID()
		if _, err := pool.Exec(ctx, `INSERT INTO trucks(id,unit_number,vin) VALUES($1,'022','3AKJHHFG3KSKV9306')`, truck); err != nil {
			t.Fatal(err)
		}
		save := func(collection string, v InventoryRecord) InventoryRecord {
			t.Helper()
			result, err := repo.Save(ctx, collection, InventorySave{RequestID: inventoryID(), Record: v}, actor)
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
		tablet := InventoryRecord{ID: inventoryID(), Data: InventoryFields{Active: true, DeviceType: "tablet", IMEI: "000000000000001", TruckID: truck, EquipmentState: "verified_installed", VerifiedBy: "forged"}}
		input := InventorySave{RequestID: inventoryID(), Record: tablet}
		tablet, err := repo.Save(ctx, "assets", input, actor)
		if err != nil {
			t.Fatal(err)
		}
		if tablet.Version != 1 || tablet.Data.VerifiedBy != actor.Name || tablet.Data.VerifiedAt == "" {
			t.Fatal("verification identity was not server owned")
		}
		replay, err := repo.Save(ctx, "assets", input, actor)
		if err != nil || replay.Version != 1 {
			t.Fatal("save replay", err)
		}
		input.Record.Data.Notes = "changed bytes"
		if _, e := repo.Save(ctx, "assets", input, actor); !errors.Is(e, ErrInventoryConflict) {
			t.Fatal("changed replay accepted", e)
		}
		stale := tablet
		stale.Version = 0
		if _, e := repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: stale}, actor); !errors.Is(e, ErrInventoryConflict) {
			t.Fatal("stale edit accepted", e)
		}
		duplicate := tablet
		duplicate.ID = inventoryID()
		duplicate.Version = 0
		duplicate.Data.IMEI = "000000000000002"
		if _, e := repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: duplicate}, actor); e == nil {
			t.Fatal("two active tablets assigned to same truck")
		}
		gateway := save("assets", InventoryRecord{ID: inventoryID(), Data: InventoryFields{Active: true, DeviceType: "samsara_gateway", Serial: "GATEWAY-001", TruckID: truck}})
		camera := save("assets", InventoryRecord{ID: inventoryID(), Data: InventoryFields{Active: true, DeviceType: "dashcam", Brand: "Samsara", Serial: "CAMERA-001", GatewayID: gateway.ID}})
		gateway.Data.Active = false
		gateway.Data.TruckID = ""
		if _, e := repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: gateway}, actor); e == nil {
			t.Fatal("gateway archived with linked camera")
		}
		camera.Data.GatewayID = tablet.ID
		if _, e := repo.Save(ctx, "assets", InventorySave{RequestID: inventoryID(), Record: camera}, actor); e == nil {
			t.Fatal("camera linked to tablet")
		}
		line := save("lines", InventoryRecord{ID: inventoryID(), Data: InventoryFields{Active: true, WirelessNumber: "9049217936", AssetID: tablet.ID}})
		tablet.Data.TruckID = ""
		tablet.Data.EquipmentState = "unknown"
		tablet = save("assets", tablet)
		replacement := save("assets", InventoryRecord{ID: inventoryID(), Data: InventoryFields{Active: true, DeviceType: "tablet", IMEI: "000000000000003", TruckID: truck}})
		line.Data.AssetID = replacement.ID
		save("lines", line)
		events, e := repo.History(ctx, "lines", line.ID)
		if e != nil || len(events) != 2 || events[0].Action != "transfer" {
			t.Fatal("line replacement history", events, e)
		}
		trailer := save("trailers", InventoryRecord{ID: inventoryID(), Data: InventoryFields{Active: true, UnitNumber: "MS003", Status: "ready", Condition: "unknown", VIN: "1UYVS2530K7824719", StorageLocation: "Troy yard"}})
		trailer.Data.Active = false
		save("trailers", trailer)
		supplies, e := repo.List(ctx, "supplies")
		if e != nil || len(supplies) != 4 {
			t.Fatal("seeded supplies", e)
		}
		supply := "019a0000-0000-4000-8000-000000000011"
		location := "location:019a0000-0000-4000-8000-000000000001"
		opening := InventoryMovement{ID: inventoryID(), SupplyID: supply, Kind: "opening", From: "external", To: location, Quantity: 3, PhysicalCount: true, Reason: "Physical opening count"}
		if e = repo.Move(ctx, opening, actor); e != nil {
			t.Fatal(e)
		}
		if e = repo.Move(ctx, opening, actor); e != nil {
			t.Fatal("movement replay", e)
		}
		opening.Quantity = 4
		if e = repo.Move(ctx, opening, actor); !errors.Is(e, ErrInventoryConflict) {
			t.Fatal("changed movement replay accepted", e)
		}
		issue := InventoryMovement{ID: inventoryID(), SupplyID: supply, Kind: "issue", From: location, To: "truck:" + truck, Quantity: 2, Reason: "Driver pickup"}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); m := issue; m.ID = inventoryID(); results <- repo.Move(ctx, m, actor) }()
		}
		wg.Wait()
		close(results)
		successes := 0
		for result := range results {
			if result == nil {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("concurrent issues: %d committed", successes)
		}
		returned := InventoryMovement{ID: inventoryID(), SupplyID: supply, Kind: "return", From: "truck:" + truck, To: location, Quantity: 1, Reason: "Returned one set"}
		if e = repo.Move(ctx, returned, actor); e != nil {
			t.Fatal(e)
		}
		reversal := InventoryMovement{ID: inventoryID(), SupplyID: supply, Kind: "reversal", From: location, To: "truck:" + truck, Quantity: 1, Reason: "Correction", ReversesID: returned.ID}
		if e = repo.Move(ctx, reversal, actor); e != nil {
			t.Fatal(e)
		}
		reversal.ID = inventoryID()
		if e = repo.Move(ctx, reversal, actor); e == nil {
			t.Fatal("same entry reversed twice")
		}
		if _, e = pool.Exec(ctx, `UPDATE inventory_stock_movements SET reason='erase'`); e == nil {
			t.Fatal("immutable stock history was edited")
		}
		zero := InventoryMovement{ID: inventoryID(), SupplyID: "019a0000-0000-4000-8000-000000000012", Kind: "opening", From: "external", To: location, PhysicalCount: true, Reason: "Physically verified zero extinguishers"}
		if e = repo.Move(ctx, zero, actor); e != nil {
			t.Fatal("zero count", e)
		}
		balances, e := repo.Stock(ctx)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, balance := range balances {
			if balance.SupplyID == zero.SupplyID && balance.Holder == location && balance.Quantity == 0 && balance.Verified {
				found = true
			}
		}
		if !found {
			t.Fatal("verified zero stock not represented")
		}
		ifta := save("equipment", InventoryRecord{ID: inventoryID(), Data: InventoryFields{Name: "IFTA Sticker", TruckID: truck, BatchYear: "2026", Sent: true, Attached: true, EquipmentState: "unknown", Active: true}})
		if ifta.Data.SentDate != "" || ifta.Data.VerifiedAt != "" {
			t.Fatal("unknown dates invented")
		}
		stickerSupply := "019a0000-0000-4000-8000-000000000014"
		receipt := InventoryMovement{ID: inventoryID(), SupplyID: stickerSupply, Kind: "receipt", From: "external", To: location, Quantity: 10, Reason: "Purchased decal pairs"}
		if e = repo.Move(ctx, receipt, actor); e != nil {
			t.Fatal(e)
		}
		stickerIssue := InventoryMovement{ID: inventoryID(), SupplyID: stickerSupply, Kind: "issue", From: location, To: "truck:" + truck, Quantity: 1, Reason: "Issue decal pair"}
		if e = repo.Move(ctx, stickerIssue, actor); e == nil {
			t.Fatal("unverified opening inventory was issued")
		}
		count := InventoryMovement{ID: inventoryID(), SupplyID: stickerSupply, Kind: "adjustment", From: "external", To: location, Quantity: 0, PhysicalCount: true, Reason: "Counted all ten pairs; no adjustment needed"}
		if e = repo.Move(ctx, count, actor); e != nil {
			t.Fatal(e)
		}
		if e = repo.Move(ctx, stickerIssue, actor); e != nil {
			t.Fatal(e)
		}
		consumableReturn := InventoryMovement{ID: inventoryID(), SupplyID: stickerSupply, Kind: "return", From: "truck:" + truck, To: location, Quantity: 1, Reason: "Attempted reusable return"}
		if e = repo.Move(ctx, consumableReturn, actor); e == nil {
			t.Fatal("consumable accepted reusable return")
		}
		consumed := InventoryMovement{ID: inventoryID(), SupplyID: stickerSupply, Kind: "use", From: "truck:" + truck, To: "consumed", Quantity: 1, Reason: "Installed pair"}
		if e = repo.Move(ctx, consumed, actor); e != nil {
			t.Fatal(e)
		}
		stockRecord, err := repo.List(ctx, "supplies")
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range stockRecord {
			if item.ID == stickerSupply {
				item.Data.Active = false
				if _, err = repo.Save(ctx, "supplies", InventorySave{RequestID: inventoryID(), Record: item}, actor); err == nil {
					t.Fatal("supply with stock was archived")
				}
			}
		}
	})
}

func TestInventoryImportDatabase(t *testing.T) {
	withInventoryDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		repo := NewInventoryRepository(pool)
		truck := inventoryID()
		if _, err := pool.Exec(ctx, `INSERT INTO trucks(id,unit_number,vin) VALUES($1,'022','3AKJHHFG3KSKV9306')`, truck); err != nil {
			t.Fatal(err)
		}
		row := make([]string, 19)
		row[1] = "777T (022)"
		row[4] = "3AKJHHFG3KSKV9306"
		row[11] = "G3BV-N92-ESK"
		row[12] = "354624630315355"
		row[13] = "904-921-7936"
		snapshot := InventorySnapshot{SpreadsheetID: "fleet-fixture", CapturedAt: time.Now().UTC().Format(time.RFC3339), Sheets: []InventorySheet{
			{Name: "Truck list", Rows: [][]string{{"headers"}, row}},
			{Name: "Samsara Inventory", Rows: [][]string{{"headers"}, {"CAMERA-1", "G3BV-N92-E5K", "#022", "Active"}}},
			{Name: "IFTA 26", Rows: [][]string{{"headers"}, {"777T (022)", "3AKJHHFG3KSKV9306", "TRUE", "TRUE"}}},
			{Name: "Inventory", Rows: [][]string{{"headers"}, {"022", "TRUE", "FALSE"}, {"009", "TRUE"}, {"010", "FALSE"}}},
			{Name: "Weekly Report", Rows: [][]string{{"headers"}, {"022", "1"}}},
		}}
		batch, err := repo.PreviewImport(ctx, snapshot, actor)
		if err != nil {
			t.Fatal(err)
		}
		replayed, e := repo.PreviewImport(ctx, snapshot, actor)
		if e != nil || replayed.ID != batch.ID {
			t.Fatal("snapshot replay", e)
		}
		if _, e = repo.CommitImport(ctx, batch.ID, batch.Version, actor); e == nil {
			t.Fatal("unreviewed import committed")
		}
		unresolved := 0
		for index := range batch.Rows {
			item := &batch.Rows[index]
			item.Reviewed = true
			if item.Collection == "" {
				continue
			}
			item.Reason = "Reviewed fixture identity"
			if len(item.Issues) > 0 {
				unresolved++
			}
			if item.Record.Data.Serial == "G3BV-N92-E5K" || item.Record.Data.DeviceType == "dashcam" || item.Collection == "equipment" && item.Record.Data.TruckID == "" {
				item.Decision = "exclude"
				item.Reason = "Unresolved identity excluded for review"
			}
		}
		if unresolved == 0 {
			t.Fatal("ambiguous #unit was silently matched")
		}
		batch, err = repo.ReviewImport(ctx, batch.ID, InventoryImportReview{Version: batch.Version, Rows: batch.Rows})
		if err != nil {
			t.Fatal(err)
		}
		committed, err := repo.CommitImport(ctx, batch.ID, batch.Version, actor)
		if err != nil {
			t.Fatal(err)
		}
		if committed.Status != "committed" {
			t.Fatal("not committed")
		}
		if _, err = repo.CommitImport(ctx, batch.ID, batch.Version, actor); err != nil {
			t.Fatal("commit replay", err)
		}
		assets, e := repo.List(ctx, "assets")
		if e != nil || len(assets) != 2 {
			t.Fatal("asset count/replay", len(assets), e)
		}
		for _, asset := range assets {
			if asset.Data.TruckID != truck {
				t.Fatal("unique VIN did not resolve alias")
			}
		}
		observations, e := repo.List(ctx, "equipment")
		if e != nil {
			t.Fatal(e)
		}
		for _, observation := range observations {
			if observation.Data.VerifiedAt != "" {
				t.Fatal("checkbox established physical verification")
			}
		}
		balances, e := repo.Stock(ctx)
		if e != nil || len(balances) != 0 {
			t.Fatal("checkbox established stock")
		}
	})
}
