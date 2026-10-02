package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

// Connection-local temporary tables shadow production names; no application
// records are changed, even when the supplied database contains real data.
func TestGrossBoardDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_GROSS_BOARD_TEST_DATABASE_URL for PostgreSQL checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer pool.Close()
	var appRoleExists bool
	if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mserp_app')").Scan(&appRoleExists); err != nil {
		t.Fatal(err)
	}
	if appRoleExists {
		if _, err = pool.Exec(ctx, "SET ROLE mserp_app"); err != nil {
			t.Fatal(err)
		}
		t.Log("Checking migration and repository queries as mserp_app")
	}
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE dispatchers(id uuid PRIMARY KEY,full_name text);
 CREATE TEMP TABLE drivers(id uuid PRIMARY KEY,full_name text,dispatcher_id uuid,active boolean);
 CREATE TEMP TABLE trucks(id uuid PRIMARY KEY,unit_number text);
 CREATE TEMP TABLE truck_driver_assignments(driver_id uuid,truck_id uuid,assigned_at timestamptz,unassigned_at timestamptz);
 CREATE TEMP TABLE driver_dispatcher_assignments(driver_id uuid,dispatcher_id uuid,dispatcher_name text,assigned_at timestamptz,unassigned_at timestamptz,start_known boolean);
 CREATE TEMP TABLE loads(id integer PRIMARY KEY,load_id text,total_pay numeric(10,2),total_miles numeric(10,2),driver_name text,pickup_time timestamptz,pickup_appointment_time timestamptz);
 CREATE TEMP TABLE gross_board_entries(driver_id uuid REFERENCES pg_temp.drivers(id),service_date date,load_number text,load_record_id integer REFERENCES pg_temp.loads(id),original_rate numeric(12,2),driver_rate numeric(12,2),miles numeric(12,2),version integer DEFAULT 1,updated_at timestamptz DEFAULT now(),PRIMARY KEY(driver_id,service_date));
 INSERT INTO dispatchers VALUES('00000000-0000-0000-0000-000000000010','Dispatch A');
 INSERT INTO drivers VALUES('00000000-0000-0000-0000-000000000001','Test Driver','00000000-0000-0000-0000-000000000010',true),('00000000-0000-0000-0000-000000000002','Inactive',null,false);
 INSERT INTO loads VALUES(1,'L100',1234.56,500.25,'Test Driver','2026-09-28 00:01Z',null),(2,'DUP',200,100,'Test Driver',null,null),(3,'DUP',300,150,'Other',null,null);`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/027_add_gross_board_review_values.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	repo := NewGrossBoardRepository(pool)
	statusMigration, err := os.ReadFile("../../sql/028_add_gross_board_day_status.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(statusMigration)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `CREATE TEMP TABLE gross_board_extra_entries (LIKE pg_temp.gross_board_entries INCLUDING DEFAULTS);
 ALTER TABLE pg_temp.gross_board_extra_entries ADD COLUMN slot integer NOT NULL, ADD COLUMN deleted boolean NOT NULL DEFAULT false,
 ADD PRIMARY KEY(driver_id,service_date,slot);`); err != nil {
		t.Fatal(err)
	}
	week, _ := time.Parse(time.DateOnly, "2026-09-28")
	cancelledMigration, err := os.ReadFile("../../sql/039_load_cancelled_status.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(cancelledMigration)); err != nil {
		t.Fatal(err)
	}
	base := GrossBoardEntry{DriverID: "00000000-0000-0000-0000-000000000001", Date: "2026-09-28", LoadNumber: "l100", OriginalRate: "999", Miles: "999", DriverRate: "1000.10"}
	if err = repo.Save(ctx, []GrossBoardEntry{base}); err != nil {
		t.Fatal(err)
	}
	board, err := repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Drivers) != 1 || len(board.Entries) != 1 {
		t.Fatalf("unexpected board: %+v", board)
	}
	entry := board.Entries[0]
	if entry.EnteredOriginalRate != "999.00" || entry.EnteredMiles != "999.00" {
		t.Fatalf("comparison values were discarded: %+v", entry)
	}
	if entry.LoadRecordID == nil || *entry.LoadRecordID != 1 || entry.OriginalRate != "999.00" || entry.Miles != "999.00" || entry.SystemOriginalRate != "1234.56" || entry.SystemMiles != "500.25" || entry.DriverRate != "1000.10" || entry.Version != 1 {
		t.Fatalf("source fields not enforced: %+v", entry)
	}
	if err = repo.Save(ctx, []GrossBoardEntry{base}); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatalf("expected insert conflict, got %v", err)
	}
	// The failed batch must roll back even a nonconflicting earlier date.
	earlier := base
	earlier.Date = "2026-09-27"
	earlier.LoadNumber = "HOME"
	if err = repo.Save(ctx, []GrossBoardEntry{earlier, base}); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_temp.gross_board_entries`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed batch was not atomic", err, count)
	}
	// Linked source values stay current after sync.
	if _, err = pool.Exec(ctx, `UPDATE pg_temp.loads SET total_pay=1500.01 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil || board.Entries[0].OriginalRate != "999.00" || board.Entries[0].SystemOriginalRate != "1500.01" {
		t.Fatal("source refresh failed", err)
	}
	// Replacing a confirmed load with free text unlocks manual values.
	// Explicit zero overrides both source fields; clearing removes the override.
	zero := board.Entries[0]
	zero.EnteredOriginalRate, zero.OriginalRate = "0", "0"
	zero.EnteredMiles, zero.Miles = "0", "0"
	zeroRows, err := repo.SaveEntries(ctx, []GrossBoardEntry{zero})
	if err != nil || zeroRows[0].OriginalRate != "0.00" || zeroRows[0].Miles != "0.00" {
		t.Fatal("zero override lost", err, zeroRows)
	}
	reset := zeroRows[0]
	reset.EnteredOriginalRate, reset.OriginalRate = "", ""
	reset.EnteredMiles, reset.Miles = "", ""
	resetRows, err := repo.SaveEntries(ctx, []GrossBoardEntry{reset})
	if err != nil || resetRows[0].OriginalRate != "1500.01" || resetRows[0].Miles != "500.25" {
		t.Fatal("cleared override did not use source", err, resetRows)
	}
	entry.Version = resetRows[0].Version
	entry.LoadNumber = "HOME"
	entry.LoadRecordID = nil
	entry.OriginalRate = "0.10"
	entry.Miles = "0.20"
	if err = repo.Save(ctx, []GrossBoardEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if err = repo.Save(ctx, []GrossBoardEntry{entry}); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal("stale update was accepted", err)
	}
	// An ambiguous exact number remains unconfirmed until explicit selection.
	second := base
	second.Date = "2026-09-29"
	second.LoadNumber = "DUP"
	if err = repo.Save(ctx, []GrossBoardEntry{second}); err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range board.Entries {
		if e.Date == second.Date && e.LoadRecordID != nil {
			t.Fatal("ambiguous number auto-confirmed")
		}
	}
	id := 3
	second.LoadRecordID = &id
	second.Version = 1
	if err = repo.Save(ctx, []GrossBoardEntry{second}); err != nil {
		t.Fatal(err)
	}
	second.Version = 2
	second.LoadNumber = "WRONG"
	if err = repo.Save(ctx, []GrossBoardEntry{second}); !errors.Is(err, ErrGrossBoardLoad) {
		t.Fatal("mismatched ID accepted", err)
	}
	matches, err := repo.SearchLoads(ctx, "L100")
	if err != nil || len(matches) != 1 || matches[0].PickupDate != "2026-09-28" {
		t.Fatal("load search/date failed", err, matches)
	}
	// A plan becomes confirmed when a unique system number appears later.
	if _, err = pool.Exec(ctx, `INSERT INTO pg_temp.loads(id,load_id,total_pay) VALUES(4,'HOME',50.25)`); err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range board.Entries {
		if e.Date == base.Date && (e.LoadRecordID == nil || e.OriginalRate != "0.10" || e.Miles != "0.20" || e.SystemOriginalRate != "50.25") {
			t.Fatalf("late confirmation failed: %+v", e)
		}
	}
	empty, err := repo.Get(ctx, week.AddDate(0, 0, 7))
	if err != nil || len(empty.Entries) != 0 {
		t.Fatal("week isolation failed", err)
	}

	// Cross-week history includes plans, not future weeks or incomplete rates.
	const carryDriver = "00000000-0000-0000-0000-000000000003"
	if _, err = pool.Exec(ctx, `INSERT INTO pg_temp.drivers VALUES($1,'Carry Driver',null,false)`, carryDriver); err != nil {
		t.Fatal(err)
	}
	plans := []GrossBoardEntry{
		{DriverID: carryDriver, Date: "2026-09-21", LoadNumber: "LATER-100", OriginalRate: "1000", DriverRate: "800", Miles: "400"},
		{DriverID: carryDriver, Date: "2026-09-28", LoadNumber: "PLAN-2", OriginalRate: "400", DriverRate: "750"},
		{DriverID: carryDriver, Date: "2026-09-29", LoadNumber: "INCOMPLETE", OriginalRate: "9999"},
		{DriverID: carryDriver, Date: "2026-10-05", LoadNumber: "PLAN-3", OriginalRate: "500", DriverRate: "350"},
	}
	if err = repo.Save(ctx, plans); err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, balance := range board.Balances {
		if balance.DriverID == carryDriver {
			found = true
			if balance.OpeningBalance != "200.00" {
				t.Fatalf("wrong carry: %+v", balance)
			}
		}
	}
	if !found {
		t.Fatal("inactive driver's carry was hidden")
	}
	history, err := repo.BalanceHistory(ctx, carryDriver, week)
	if err != nil || len(history) != 3 || history[2].Balance != "-150.00" || history[2].Change != "" {
		t.Fatalf("future or incomplete entry changed balance: %+v, %v", history, err)
	}
	// An unmatched number is retained exactly; no fuzzy linking.
	if _, err = pool.Exec(ctx, `INSERT INTO pg_temp.loads(id,load_id,total_pay,total_miles) VALUES(10,'LATER-100',900.25,430),(11,'PLAN-22',999,200)`); err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range board.Entries {
		if e.DriverID == carryDriver && e.Date == "2026-09-28" && (e.LoadNumber != "PLAN-2" || e.LoadRecordID != nil) {
			t.Fatal("near match changed the plan", e)
		}
	}
	prior, err := repo.Get(ctx, week.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	var linked GrossBoardEntry
	for _, e := range prior.Entries {
		if e.DriverID == carryDriver {
			linked = e
		}
	}
	if linked.LoadRecordID == nil || linked.OriginalRate != "1000.00" || linked.SystemOriginalRate != "900.25" || linked.EnteredOriginalRate != "1000.00" || linked.EnteredMiles != "400.00" || linked.Miles != "400.00" || linked.SystemMiles != "430.00" {
		t.Fatalf("late match lost reference or source values: %+v", linked)
	}
	// Ordinary autosaves must not dismiss a discrepancy.
	saved, err := repo.SaveEntries(ctx, []GrossBoardEntry{linked})
	if err != nil || saved[0].EnteredOriginalRate != "1000.00" {
		t.Fatal("autosave erased comparison", err)
	}
	linked = saved[0]
	linked.AcceptSystemValues = true
	if _, err = pool.Exec(ctx, `UPDATE pg_temp.loads SET total_pay=900.50 WHERE id=10`); err != nil {
		t.Fatal(err)
	}
	if err = repo.Save(ctx, []GrossBoardEntry{linked}); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal("stale source review accepted", err)
	}
	linked.OriginalRate = "900.50"
	linked.Miles = "430.00"
	saved, err = repo.SaveEntries(ctx, []GrossBoardEntry{linked})
	if err != nil || saved[0].EnteredOriginalRate != "900.50" || saved[0].EnteredMiles != "430.00" || saved[0].AcceptSystemValues {
		t.Fatal("review did not record exact source values", err, saved)
	}
	// Repeating the same system load on another day never creates more balance.
	duplicate := GrossBoardEntry{DriverID: carryDriver, Date: "2026-09-30", LoadNumber: "LATER-100", DriverRate: "100"}
	saved, err = repo.SaveEntries(ctx, []GrossBoardEntry{duplicate})
	if err != nil || !saved[0].Duplicate {
		t.Fatal("repeated load was not identified", err)
	}
	history, err = repo.BalanceHistory(ctx, carryDriver, week)
	if err != nil || history[len(history)-1].Balance != "-249.50" || history[len(history)-1].Change != "" {
		t.Fatal("repeated load changed running balance", err, history)
	}
	board, err = repo.Get(ctx, week.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	for _, balance := range board.Balances {
		if balance.DriverID == carryDriver && (balance.OpeningBalance != "-249.50" || balance.OpeningIncomplete != 1) {
			t.Fatal("next week did not carry corrected balance", balance)
		}
	}
	// A status is distinct from load text, even when an imported load is named HOME.
	statusDay := GrossBoardEntry{DriverID: carryDriver, Date: "2026-09-22", DayStatus: "HOME"}
	saved, err = repo.SaveEntries(ctx, []GrossBoardEntry{statusDay})
	if err != nil || saved[0].DayStatus != "HOME" || saved[0].LoadRecordID != nil || saved[0].OriginalRate != "" {
		t.Fatal("status matched a load or failed to persist", saved, err)
	}
	statusDay = saved[0]
	if err = repo.Save(ctx, []GrossBoardEntry{{DriverID: carryDriver, Date: "2026-09-22", DayStatus: "SHOP"}}); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal("status bypassed version check", err)
	}
	board, err = repo.Get(ctx, week.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	for _, balance := range board.Balances {
		if balance.DriverID == carryDriver && (balance.OpeningBalance != "-249.50" || balance.OpeningIncomplete != 1) {
			t.Fatal("status affected carry or incomplete count", balance)
		}
	}
	history, err = repo.BalanceHistory(ctx, carryDriver, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range history {
		if line.Date == statusDay.Date {
			t.Fatal("status appeared as incomplete rate history", line)
		}
	}
	// Replacing and clearing statuses keep normal version semantics.
	statusDay.DayStatus = ""
	statusDay.LoadNumber = "STATUS-TO-PLAN"
	statusDay.OriginalRate, statusDay.DriverRate = "10", "5"
	saved, err = repo.SaveEntries(ctx, []GrossBoardEntry{statusDay})
	if err != nil || saved[0].DayStatus != "" || saved[0].OriginalRate != "10.00" {
		t.Fatal("status could not become a load plan", err, saved)
	}
	cleared := GrossBoardEntry{DriverID: carryDriver, Date: statusDay.Date, Version: saved[0].Version, DayStatus: "RESET"}
	saved, err = repo.SaveEntries(ctx, []GrossBoardEntry{cleared})
	if err != nil || saved[0].OriginalRate != "" || saved[0].DriverRate != "" {
		t.Fatal("plan values leaked into status", err, saved)
	}
	cleared = saved[0]
	cleared.DayStatus = ""
	if err = repo.Save(ctx, []GrossBoardEntry{cleared}); err != nil {
		t.Fatal(err)
	}
	// Database constraints also reject invalid status payloads outside HTTP.
	for _, slot := range []int{0, 1} {
		cancelled := GrossBoardEntry{DriverID: carryDriver, Date: "2026-10-02", Slot: slot, DayStatus: "LOAD CANCELLED"}
		saved, err = repo.SaveEntries(ctx, []GrossBoardEntry{cancelled})
		if err != nil || len(saved) != 1 || saved[0].DayStatus != "LOAD CANCELLED" || saved[0].LoadRecordID != nil {
			t.Fatalf("cancelled status slot %d: %v %+v", slot, err, saved)
		}
	}
	bad := GrossBoardEntry{DriverID: carryDriver, Date: "2026-10-01", DayStatus: "HOME", OriginalRate: "1"}
	if err = repo.Save(ctx, []GrossBoardEntry{bad}); err == nil {
		t.Fatal("database accepted money on a status day")
	}
}
