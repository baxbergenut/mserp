package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDriverPayDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_PAY_TEST_DATABASE_URL to an isolated administrator database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("isolated database unavailable")
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("driver_pay_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err := admin.Exec(ctx, `CREATE SCHEMA `+quoted+`; SET search_path TO `+quoted+`,public`); err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			source, err := os.ReadFile("../../sql/init.sql")
			if err != nil {
				t.Fatal(err)
			}
			migration, err := os.ReadFile("../../sql/029_add_driver_pay.sql")
			if err != nil {
				t.Fatal(err)
			}
			body := strings.TrimSuffix(strings.TrimPrefix(strings.ReplaceAll(string(migration), "\r\n", "\n"), "BEGIN;\n"), "COMMIT;\n")
			costMigration, err := os.ReadFile("../../sql/030_add_driver_pay_cost_overrides.sql")
			if err != nil {
				t.Fatal(err)
			}
			costBody := strings.TrimSuffix(strings.TrimPrefix(strings.ReplaceAll(string(costMigration), "\r\n", "\n"), "BEGIN;\n"), "COMMIT;\n")
			cancelledMigration, err := os.ReadFile("../../sql/039_load_cancelled_status.sql")
			if err != nil {
				t.Fatal(err)
			}
			settlementMigration, err := os.ReadFile("../../sql/038_payroll_settlements.sql")
			if err != nil {
				t.Fatal(err)
			}
			sql := strings.ReplaceAll(string(source), "\r\n", "\n")
			if mode == "migration" {
				// Restore the pre-039 status checks before removing migration 029's
				// exact schema block to construct the legacy database.
				sql = strings.ReplaceAll(sql, ", 'LOAD CANCELLED'", "")
				sql = strings.Replace(sql, strings.ReplaceAll(string(settlementMigration), "\r\n", "\n"), "", 1)
				sql = strings.Replace(sql, costBody, "", 1)
				sql = strings.Replace(sql, body, "", 1)
				if strings.Contains(sql, "CREATE TABLE driver_pay_weeks") {
					t.Fatal("failed to isolate legacy schema")
				}
			}
			if _, err := admin.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
			// Seed an existing slot-zero entry before applying the additive migration.
			var driver string
			if err := admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active) VALUES('Payroll Example','payroll example','cpm',0.75,false) RETURNING id`).Scan(&driver); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,raw_payload) VALUES
   (11,'LOAD-A','dispatched',1000,1200,847.34,'2026-09-28 00:01:00+00','{"trip":{"mile":725.78,"empty_mile":121.56},"stops":[{"stop_type":"delivery","ordering":3,"location":{"city":"Last","state":"PA"}},{"stop_type":"pickup","ordering":1,"location":{"city":"First","state":"OH"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Middle","state":"WV"}}]}'),
   (12,'LOAD-B','cancelled',600,700,100.01,NULL,'{}'),
   (13,'DUPLICATE','delivered',1,1,1,NULL,'{}'),(14,'DUPLICATE','delivered',2,2,2,NULL,'{}');
   INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES('`+driver+`','2026-09-28','LOAD-A',11,1000)`); err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				if _, err := admin.Exec(ctx, string(migration)); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.Exec(ctx, string(costMigration)); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.Exec(ctx, string(cancelledMigration)); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "migration" {
				if _, err := admin.Exec(ctx, string(settlementMigration)); err != nil {
					t.Fatal(err)
				}
			}
			// Only grant legacy tables here: new table ownership must come from SQL.
			if _, err := admin.Exec(ctx, `GRANT USAGE ON SCHEMA `+quoted+` TO mserp_app;
 GRANT SELECT ON app_users TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,loads,dispatchers,truck_driver_assignments,trucks,gross_board_entries,fuel_transactions,fuel_transaction_items,tolls TO mserp_app`); err != nil {
				t.Fatal(err)
			}
			config, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal("invalid test configuration")
			}
			config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			config.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			board := NewGrossBoardRepository(pool)
			pay := NewDriverPayRepository(pool)
			seedDriverPayCosts(t, ctx, pool, driver)
			monday, _ := time.Parse(time.DateOnly, "2026-09-28")
			batch := []GrossBoardEntry{
				{DriverID: driver, Date: "2026-09-28", Slot: 1, LoadNumber: "LOAD-B", OriginalRate: "1", DriverRate: "600", Miles: "1"},
				{DriverID: driver, Date: "2026-09-28", Slot: 2, LoadNumber: "NOT-IMPORTED", OriginalRate: "900", DriverRate: "800", Miles: "999"},
				{DriverID: driver, Date: "2026-09-29", LoadNumber: "DUPLICATE"},
			}
			saved, err := board.SaveEntries(ctx, batch)
			if err != nil {
				t.Fatal(err)
			}
			if saved[0].OriginalRate != "700.00" || saved[0].Miles != "100.01" {
				t.Fatalf("source fields not authoritative: %+v", saved[0])
			}
			report, err := pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Drivers) != 1 || len(report.Drivers[0].Loads) != 4 {
				t.Fatalf("all planned/inactive/non-delivered entries must appear: %+v", report)
			}
			loads := report.Drivers[0].Loads
			if report.Drivers[0].FuelTotal != "90.30" || report.Drivers[0].TollTotal != "12.25" {
				t.Fatalf("weekly costs (must not multiply by load slots): fuel=%s toll=%s", report.Drivers[0].FuelTotal, report.Drivers[0].TollTotal)
			}
			if loads[0].PickupDate != "2026-09-28" || loads[0].PickupLocation != "First, OH" || loads[0].DeliveryLocation != "Last, PA" || loads[0].Fee != "635.51" || loads[0].LoadedMiles != "725.78" {
				t.Fatalf("source mapping: %+v", loads[0])
			}
			if loads[2].Fee != "" || loads[2].TotalMiles != "" || loads[2].OriginalRate != "" || len(loads[2].Issues) == 0 {
				t.Fatalf("unmatched data fabricated: %+v", loads[2])
			}
			if loads[3].LoadRecordID != nil {
				t.Fatal("ambiguous load matched automatically")
			}
			edits := report.Drivers[0].Edits
			edits.Notes = "Weekly note"
			fuelOverride, tollOverride := "-80.15", "0.00"
			edits.FuelOverride, edits.TollOverride = &fuelOverride, &tollOverride
			edits.Comments[loads[0].CommentKey] = "Checked receipt"
			edits.Adjustments = []DriverPayAdjustment{{ID: "00000000-0000-0000-0000-000000000001", Kind: "reimbursement", Name: "Parking", Amount: "35.25"}}
			updated, err := pay.Save(ctx, edits)
			if err != nil || updated.Version != 1 {
				t.Fatalf("save: %+v %v", updated, err)
			}
			if _, err := pay.Save(ctx, edits); !errors.Is(err, ErrDriverPayConflict) {
				t.Fatalf("stale edits: %v", err)
			}
			updated.Notes = "Second note"
			if _, err := pay.Save(ctx, updated); err != nil {
				t.Fatal(err)
			}
			// An atomic batch containing a stale slot must not save its other changes.
			stale := saved[1]
			stale.Version = 0
			another := saved[0]
			another.DriverRate = "123"
			if _, err := board.SaveEntries(ctx, []GrossBoardEntry{another, stale}); !errors.Is(err, ErrGrossBoardConflict) {
				t.Fatalf("stale board write: %v", err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			if report.Drivers[0].Loads[1].DriverGross != "600.00" || report.Drivers[0].Edits.Notes != "Second note" || len(report.Drivers[0].Edits.Adjustments) != 1 {
				t.Fatal("transaction or edit persistence failed")
			}
			persisted := report.Drivers[0].Edits
			if persisted.FuelOverride == nil || *persisted.FuelOverride != fuelOverride || persisted.TollOverride == nil || *persisted.TollOverride != tollOverride {
				t.Fatal("cost overrides not persisted")
			}
			persisted.FuelOverride, persisted.TollOverride = nil, nil
			if _, err := pay.Save(ctx, persisted); err != nil {
				t.Fatal(err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil || report.Drivers[0].Edits.FuelOverride != nil || report.Drivers[0].Edits.TollOverride != nil {
				t.Fatal("reset overrides failed", err)
			}
			// Profile percentage uses board driver gross, not the original load gross.
			if _, err := pool.Exec(ctx, `UPDATE drivers SET pay_type='gross_percentage',pay_rate=30 WHERE id=$1`, driver); err != nil {
				t.Fatal(err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil {
				t.Fatal(err)
			}
			if report.Drivers[0].Loads[0].Fee != "300.00" || report.Drivers[0].Loads[2].Fee != "240.00" {
				t.Fatal("percentage basis or provisional plan fee incorrect")
			}
			removed := saved[0]
			removed.Deleted = true
			removed.LoadRecordID = nil
			removed.LoadNumber = ""
			removed.OriginalRate = ""
			removed.DriverRate = ""
			removed.Miles = ""
			if _, err := board.SaveEntries(ctx, []GrossBoardEntry{removed}); err != nil {
				t.Fatal(err)
			}
			if _, err := board.SaveEntries(ctx, []GrossBoardEntry{saved[0]}); !errors.Is(err, ErrGrossBoardConflict) {
				t.Fatal("removed slot lost concurrency version")
			}
			report, err = pay.Get(ctx, monday)
			if err != nil || len(report.Drivers[0].Loads) != 3 {
				t.Fatalf("removed extra slot: %+v %v", report, err)
			}
			// Extra slots participate in carry and retain source review metadata.
			nextBoard, err := board.Get(ctx, monday.AddDate(0, 0, 7))
			if err != nil || len(nextBoard.Balances) != 1 || nextBoard.Balances[0].OpeningBalance != "300.00" {
				t.Fatalf("extra-slot carry: %+v %v", nextBoard, err)
			}
			restored := removed
			restored.Deleted = false
			restored.Version++
			restored.LoadNumber = "LOAD-B"
			restored.OriginalRate = "650"
			restored.DriverRate = "600"
			restored.Miles = "90"
			restored.EnteredOriginalRate = "650"
			restored.EnteredMiles = "90"
			restoredRows, err := board.SaveEntries(ctx, []GrossBoardEntry{restored})
			if err != nil {
				t.Fatal(err)
			}
			restored = restoredRows[0]
			if restored.EnteredOriginalRate != "650.00" || restored.OriginalRate != "700.00" {
				t.Fatalf("extra-slot review: %+v", restored)
			}
			restored.AcceptSystemValues = true
			accepted, err := board.SaveEntries(ctx, []GrossBoardEntry{restored})
			if err != nil || accepted[0].EnteredOriginalRate != "700.00" {
				t.Fatalf("extra-slot acceptance: %+v %v", accepted, err)
			}
			status := GrossBoardEntry{DriverID: driver, Date: "2026-09-30", Slot: 1, DayStatus: "HOME"}
			if _, err := board.SaveEntries(ctx, []GrossBoardEntry{status}); err != nil {
				t.Fatal(err)
			}
			nextBoard, err = board.Get(ctx, monday.AddDate(0, 0, 7))
			if err != nil || nextBoard.Balances[0].OpeningBalance != "400.00" {
				t.Fatalf("status affected carry: %+v %v", nextBoard, err)
			}
			report, err = pay.Get(ctx, monday)
			if err != nil || len(report.Drivers[0].Loads) != 4 {
				t.Fatalf("status appeared in payroll: %+v %v", report, err)
			}
			// Old binaries still update exactly one legacy daily entry.
			result, err := pool.Exec(ctx, `UPDATE gross_board_entries SET driver_rate=900 WHERE driver_id=$1 AND service_date=$2`, driver, monday)
			if err != nil || result.RowsAffected() != 1 {
				t.Fatal("legacy writes changed")
			}
			next, err := pay.Get(ctx, monday.AddDate(0, 0, 7))
			if err != nil || len(next.Drivers) != 0 {
				t.Fatal("week boundary leaked")
			}
			if _, err := board.SaveEntries(ctx, []GrossBoardEntry{{DriverID: driver, Date: "2026-10-05", LoadNumber: "NEXT-WEEK"}}); err != nil {
				t.Fatal(err)
			}
			next, err = pay.Get(ctx, monday.AddDate(0, 0, 7))
			if err != nil || len(next.Drivers) != 1 {
				t.Fatal("next week report", err)
			}
			// The fuel boundary fixture belongs to next week; no tolls or manual
			// overrides may carry forward from the previous driver's week.
			if next.Drivers[0].FuelTotal != "999.00" || next.Drivers[0].TollTotal != "0" || next.Drivers[0].Edits.FuelOverride != nil || next.Drivers[0].Edits.TollOverride != nil {
				t.Fatal("next week costs or overrides leaked")
			}
		})
	}
}
