package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTrailerStatusesDatabase(t *testing.T) {
	seed := func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
		for i := 0; i < 34; i++ {
			if _, err := conn.Exec(ctx, `INSERT INTO fleet_trailers(id,data) VALUES(gen_random_uuid(),jsonb_build_object('unitNumber',$1::text,'status','stored_unused','condition','unknown','active',true,'notes','Original source notes'))`, fmt.Sprintf("LEGACY%02d", i)); err != nil {
				t.Fatal(err)
			}
		}
	}
	withInventoryDatabaseSeed(t, seed, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor InventoryActor) {
		repo := NewInventoryRepository(pool)
		roster, err := repo.List(ctx, "trailers")
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range roster {
			if v.Data.Status != "ready" || !v.Data.Active || v.Data.Notes != "Original source notes" || !strings.Contains(v.Data.StatusReason, "confirmed Ready") {
				t.Fatalf("migration lost approval or provenance: %+v", v)
			}
			history, err := repo.History(ctx, "trailers", v.ID)
			var before InventoryRecord
			if len(history) > 0 {
				_ = json.Unmarshal(history[0].Before, &before)
			}
			if err != nil || len(history) != 1 || before.Data.Status != "stored_unused" {
				t.Fatalf("migration audit: %+v %v", history, err)
			}
		}
		if len(roster) != 0 && len(roster) != 34 {
			t.Fatalf("migrated count %d", len(roster))
		}
		truck := inventoryID()
		otherTruck := inventoryID()
		if _, err = pool.Exec(ctx, `INSERT INTO trucks(id,unit_number) VALUES($1,'TRUCK1'),($2,'TRUCK2')`, truck, otherTruck); err != nil {
			t.Fatal(err)
		}
		save := func(v InventoryRecord, reason string) InventoryRecord {
			t.Helper()
			saved, e := repo.Save(ctx, "trailers", InventorySave{RequestID: inventoryID(), Record: v, Reason: reason}, actor)
			if e != nil {
				t.Fatal(e)
			}
			return saved
		}
		reject := func(v InventoryRecord, reason string) {
			t.Helper()
			if _, e := repo.Save(ctx, "trailers", InventorySave{RequestID: inventoryID(), Record: v, Reason: reason}, actor); e == nil {
				t.Fatalf("accepted invalid trailer %+v", v.Data)
			}
		}
		a := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{UnitNumber: "A", Status: "ready", Notes: "Retained note"}}, "")
		if !a.Data.Active {
			t.Fatal("active must be derived")
		}
		history, _ := repo.History(ctx, "trailers", a.ID)
		forged := a
		forged.Data.Active = false
		forged.Data.StatusReason = "forged"
		noOp := save(forged, "")
		after, _ := repo.History(ctx, "trailers", a.ID)
		if noOp.Version != a.Version || len(after) != len(history) {
			t.Fatal("no-op created revision/history")
		}
		b := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{UnitNumber: "B", Status: "ready"}}, "")
		draft := a
		draft.Data.Status = "in_use"
		reject(draft, "")
		stale := a
		a.Data.TruckID = truck
		a = save(a, "")
		if a.Data.Status != "in_use" || a.TruckUnit != "TRUCK1" {
			t.Fatal("assignment not projected", a)
		}
		if _, e := repo.Save(ctx, "trailers", InventorySave{RequestID: inventoryID(), Record: stale}, actor); !errors.Is(e, ErrInventoryConflict) {
			t.Fatal("missing stale fence", e)
		}
		draft = b
		draft.Data.TruckID = truck
		reject(draft, "")
		for _, status := range []string{"shop", "out_of_service"} {
			draft = a
			draft.Data.Status = status
			reject(draft, "")
			a = save(draft, "Repair inspection")
			if a.Data.TruckID != truck || a.Data.Active != (status == "shop") {
				t.Fatal("restriction lost assignment or active projection")
			}
			draft = a
			draft.Data.TruckID = otherTruck
			reject(draft, "")
			draft = a
			draft.Data.Status = "ready"
			reject(draft, "")
			a = save(draft, "Inspection passed")
			if a.Data.Status != "in_use" {
				t.Fatal("return must use current assignment")
			}
		}
		draft = a
		draft.Data.Status = "terminated"
		reject(draft, "Leaving")
		draft.Data.TruckID = ""
		reject(draft, "Leaving") // Must explicitly resolve assignment first.
		a.Data.TruckID = ""
		a = save(a, "")
		if a.Data.Status != "ready" {
			t.Fatal("release must derive Ready")
		}
		a.Data.Status = "terminated"
		a = save(a, "Company departure")
		draft = a
		draft.Data.TruckID = truck
		reject(draft, "")
		a.Data.Status = "ready"
		a = save(a, "Rejoined fleet")
		if !a.Data.Active || a.Data.Status != "ready" {
			t.Fatal("reversible termination")
		}
		a.Data.Status = "shop"
		a = save(a, "Axle repair")
		page, e := repo.TrailersPage(ctx, Pagination{Page: 99, PageSize: 1}, TrailerFilters{Search: "Axle"})
		if e != nil || page.Total != 1 || page.Page.Page != 1 || page.Items[0].ID != a.ID || page.StatusCounts["shop"] != 1 {
			t.Fatal("notes search/paging", page, e)
		}
		a.Data.Status = "ready"
		a = save(a, "Repair complete")
		// Competing assignments must not both succeed, even with different record versions.
		candidates := []InventoryRecord{a, b}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, v := range candidates {
			wg.Add(1)
			go func(v InventoryRecord) {
				defer wg.Done()
				v.Data.TruckID = otherTruck
				_, err := repo.Save(ctx, "trailers", InventorySave{RequestID: inventoryID(), Record: v}, actor)
				results <- err
			}(v)
		}
		wg.Wait()
		close(results)
		success := 0
		for e := range results {
			if e == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatalf("concurrent assignment successes=%d", success)
		}
		// A restricted truck cannot acquire a new trailer.
		if _, e = pool.Exec(ctx, `UPDATE trucks SET status='maintenance',status_reason='Repair' WHERE id=$1`, truck); e != nil {
			t.Fatal(e)
		}
		c := save(InventoryRecord{ID: inventoryID(), Data: InventoryFields{UnitNumber: "C", Status: "ready"}}, "")
		c.Data.TruckID = truck
		reject(c, "")
		// Restriction survives release; only an explicit, reasoned return clears it.
		a, _ = repo.Trailer(ctx, a.ID)
		a.Data.Status = "shop"
		a = save(a, "Restricted")
		a.Data.TruckID = ""
		a = save(a, "")
		if a.Data.Status != "shop" || a.Data.StatusReason != "Restricted" {
			t.Fatal("release cleared restriction")
		}
		replay := InventorySave{RequestID: inventoryID(), Record: a, Reason: "Explicit return"}
		replay.Record.Data.Status = "ready"
		first, e := repo.Save(ctx, "trailers", replay, actor)
		if e != nil {
			t.Fatal(e)
		}
		again, e := repo.Save(ctx, "trailers", replay, actor)
		if e != nil || first.Version != again.Version {
			t.Fatal("idempotent replay", e)
		}
		// Importing metadata must neither release restrictions nor replace assignment.
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		draft = first
		draft.Data.Status = "terminated"
		draft.Data.Active = false
		draft.Data.TruckID = truck
		draft.Data.Notes = "Updated source note"
		imported, e := inventorySaveTx(ctx, tx, "trailers", draft, actor, inventoryID(), "import-test", true)
		if e != nil || imported.Data.Status != first.Data.Status || imported.Data.TruckID != first.Data.TruckID {
			t.Fatal("import overwrote lifecycle", imported, e)
		}
		newImport, e := inventorySaveTx(ctx, tx, "trailers", InventoryRecord{ID: inventoryID(), Data: InventoryFields{UnitNumber: "IMPORTED", Status: "ready"}}, actor, inventoryID(), "import-new", true)
		if e != nil || newImport.Data.Status != "shop" || newImport.Data.StatusReason == "" {
			t.Fatal("import manufactured readiness", newImport, e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `UPDATE fleet_trailers SET data=jsonb_set(data,'{active}','false') WHERE id=$1`, a.ID); e == nil {
			t.Fatal("database accepted independent active change")
		}
	})
}

func TestTrailerDirectoryProjection(t *testing.T) {
	records := []InventoryRecord{}
	for i, status := range []string{"terminated", "in_use", "shop", "ready", "out_of_service", "ready"} {
		records = append(records, InventoryRecord{ID: fmt.Sprint(i), Data: InventoryFields{UnitNumber: fmt.Sprint(9 - i), Status: status, StorageLocation: "Yard"}})
	}
	page := paginateTrailers(records, Pagination{PageSize: 2}, TrailerFilters{})
	if page.Total != 6 || page.TotalPages != 3 || page.Items[0].Data.Status != "ready" || page.Items[1].Data.Status != "ready" {
		t.Fatal(page)
	}
	page = paginateTrailers(records, Pagination{Page: 2, PageSize: 2}, TrailerFilters{})
	if page.Items[0].Data.Status != "shop" || page.Items[1].Data.Status != "in_use" {
		t.Fatal(page)
	}
	page = paginateTrailers(records, Pagination{Page: 3, PageSize: 2}, TrailerFilters{Status: "shop"})
	if page.Page.Page != 1 || page.Total != 1 || page.StatusCounts["ready"] != 2 {
		t.Fatal(page)
	}
}
