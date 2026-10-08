package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/prepass"
)

// Runs in both fresh and migrated schemas under the application role.
func testTruckCostAliases(t *testing.T, ctx context.Context, pool *pgxpool.Pool, truck, otherTruck, fuelID string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE trucks SET unit_number='RENAMED-COST-TRUCK' WHERE id=$1`, truck)
	costs, err := readTruckCostAllocations(ctx, tx, "2026-09-28")
	if err != nil || costs[truck] == nil || costs[truck].fuel != 155000 {
		t.Fatalf("fuel lost after truck rename: %+v %v", costs[truck], err)
	}
	exec(`INSERT INTO truck_unit_aliases(unit_key,truck_id) VALUES('2',$1),('02',$1)`, truck)
	exec(`UPDATE fuel_transactions SET prompts='[{"label":"Truck #","value":" 02 "}]' WHERE id=$1`, fuelID)
	costs, err = readTruckCostAllocations(ctx, tx, "2026-09-28")
	if err != nil || costs[truck] == nil || costs[truck].fuel != 155000 {
		t.Fatalf("numeric fuel alias: %+v %v", costs[truck], err)
	}
	exec(`INSERT INTO truck_unit_aliases(unit_key,truck_id) VALUES('02',$1)`, otherTruck)
	costs, err = readTruckCostAllocations(ctx, tx, "2026-09-28")
	if err != nil || (costs[truck] != nil && costs[truck].fuel != 0) || (costs[otherTruck] != nil && costs[otherTruck].fuel != 0) {
		t.Fatalf("ambiguous fuel alias must stay unallocated: %+v %v", costs, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Exercise the real importer and reconciliation, including conflicting aliases.
	if _, err := pool.Exec(ctx, `UPDATE trucks SET unit_number='RENAMED-COST-TRUCK' WHERE id=$1`, truck); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM tolls WHERE prepass_toll_id=987654321`); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trucks SET unit_number='TRUCK-A' WHERE id=$1`, truck); err != nil {
			t.Error(err)
		}
	}()
	repo := NewTollRepository(pool)
	day := time.Date(2020, 1, 6, 0, 0, 0, 0, time.UTC)
	value := prepass.Transaction{TollID: 987654321, AccountNumber: json.Number("123"), PostDateTime: "2020-01-06T12:00:00Z", VehicleNumber: " truck-a ", TollCharge: json.Number("12.30")}
	result, err := repo.UpsertDay(ctx, "production", day, []prepass.Transaction{value}, day, false)
	if err != nil || result.Unmatched != 0 {
		t.Fatalf("import renamed truck: %+v %v", result, err)
	}
	assertLink := func(want bool) {
		t.Helper()
		var linked bool
		if err := pool.QueryRow(ctx, `SELECT coalesce(truck_id=$1,false) FROM tolls WHERE prepass_toll_id=987654321`, truck).Scan(&linked); err != nil || linked != want {
			t.Fatalf("toll linked=%v want=%v: %v", linked, want, err)
		}
	}
	assertLink(true)
	if _, err := pool.Exec(ctx, `UPDATE tolls SET truck_id=NULL WHERE prepass_toll_id=987654321`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReconcileTruckAssignments(ctx); err != nil {
		t.Fatal(err)
	}
	assertLink(true)
	if _, err := pool.Exec(ctx, `INSERT INTO truck_unit_aliases(unit_key,truck_id) VALUES('TRUCK-A',$1)`, otherTruck); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM truck_unit_aliases WHERE unit_key='TRUCK-A' AND truck_id=$1`, otherTruck); err != nil {
			t.Error(err)
		}
	}()
	if _, err := pool.Exec(ctx, `UPDATE tolls SET truck_id=NULL WHERE prepass_toll_id=987654321`); err != nil {
		t.Fatal(err)
	}
	result, err = repo.UpsertDay(ctx, "production", day, []prepass.Transaction{value}, day, false)
	if err != nil || result.Unmatched != 1 {
		t.Fatalf("ambiguous import: %+v %v", result, err)
	}
	if err := repo.ReconcileTruckAssignments(ctx); err != nil {
		t.Fatal(err)
	}
	assertLink(false)
}
