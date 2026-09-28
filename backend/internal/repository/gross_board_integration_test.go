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
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE dispatchers(id uuid PRIMARY KEY,full_name text);
 CREATE TEMP TABLE drivers(id uuid PRIMARY KEY,full_name text,dispatcher_id uuid,active boolean);
 CREATE TEMP TABLE trucks(id uuid PRIMARY KEY,unit_number text);
 CREATE TEMP TABLE truck_driver_assignments(driver_id uuid,truck_id uuid,unassigned_at timestamptz);
 CREATE TEMP TABLE loads(id integer PRIMARY KEY,load_id text,total_pay numeric(10,2),total_miles numeric(10,2),driver_name text,pickup_time timestamptz,pickup_appointment_time timestamptz);
 CREATE TEMP TABLE gross_board_entries(driver_id uuid REFERENCES pg_temp.drivers(id),service_date date,load_number text,load_record_id integer REFERENCES pg_temp.loads(id),original_rate numeric(12,2),driver_rate numeric(12,2),miles numeric(12,2),version integer DEFAULT 1,updated_at timestamptz DEFAULT now(),PRIMARY KEY(driver_id,service_date));
 INSERT INTO dispatchers VALUES('00000000-0000-0000-0000-000000000010','Dispatch A');
 INSERT INTO drivers VALUES('00000000-0000-0000-0000-000000000001','Test Driver','00000000-0000-0000-0000-000000000010',true),('00000000-0000-0000-0000-000000000002','Inactive',null,false);
 INSERT INTO loads VALUES(1,'L100',1234.56,500.25,'Test Driver','2026-09-28 00:01Z',null),(2,'DUP',200,100,'Test Driver',null,null),(3,'DUP',300,150,'Other',null,null);`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewGrossBoardRepository(pool)
	week, _ := time.Parse(time.DateOnly, "2026-09-28")
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
	if entry.LoadRecordID == nil || *entry.LoadRecordID != 1 || entry.OriginalRate != "1234.56" || entry.Miles != "500.25" || entry.DriverRate != "1000.10" || entry.Version != 1 {
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
	if err != nil || board.Entries[0].OriginalRate != "1500.01" {
		t.Fatal("source refresh failed", err)
	}
	// Replacing a confirmed load with free text unlocks manual values.
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
		if e.Date == base.Date && (e.LoadRecordID == nil || e.OriginalRate != "50.25" || e.Miles != "") {
			t.Fatalf("late confirmation failed: %+v", e)
		}
	}
	empty, err := repo.Get(ctx, week.AddDate(0, 0, 7))
	if err != nil || len(empty.Entries) != 0 {
		t.Fatal("week isolation failed", err)
	}
}
