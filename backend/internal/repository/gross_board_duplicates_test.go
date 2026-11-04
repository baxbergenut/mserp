package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGrossBoardDuplicateClaimsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const other = "18200000-0000-0000-0000-000000000099"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Other Driver','other driver','cpm',0.75)`, other); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,total_pay,load_pay,total_miles,raw_payload) VALUES
 (88201,'DUP-CLAIM','booked',1000,1000,200,'{}'),(88202,'DUP-CLAIM','invoiced',1000,1000,205,'{}')`); err != nil {
			t.Fatal(err)
		}
		repo := NewGrossBoardRepository(pool)
		old, current := 88201, 88202
		base := GrossBoardEntry{DriverID: projectionDriver, Date: "2026-10-06", LoadNumber: "DUP-CLAIM", LoadRecordID: &old, DriverRate: "750"}
		saved, err := repo.SaveEntries(ctx, []GrossBoardEntry{base})
		if err != nil {
			t.Fatal(err)
		}
		reject := func(name string, rows []GrossBoardEntry) {
			t.Helper()
			if _, err := repo.SaveEntries(ctx, rows); err == nil || !strings.Contains(err.Error(), "already on Gross Board") {
				t.Fatalf("%s: duplicate not rejected: %v", name, err)
			}
		}
		double := base
		double.Slot = 1
		reject("same day", []GrossBoardEntry{double})
		double.LoadRecordID = &current
		reject("replacement provider ID", []GrossBoardEntry{double})
		double.Date = "2026-10-13"
		double.LoadNumber = " dup-claim "
		reject("other week and normalized number", []GrossBoardEntry{double})
		double.DriverID = other
		double.LoadRecordID = nil
		reject("other driver and unresolved number", []GrossBoardEntry{double})
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM gross_board_extra_entries`).Scan(&count); err != nil || count != 0 {
			t.Fatal("rejected writes persisted", count, err)
		}
		// The entire batch rolls back, including an unrelated edit before the duplicate.
		edit := saved[0]
		edit.DriverRate = "701"
		reject("batch rollback", []GrossBoardEntry{edit, double})
		var gross string
		if err := pool.QueryRow(ctx, `SELECT driver_rate::text FROM gross_board_entries WHERE driver_id=$1 AND service_date='2026-10-06'`, projectionDriver).Scan(&gross); err != nil || gross != "750.00" {
			t.Fatal("partial batch committed", gross, err)
		}
		// Moving to an earlier date succeeds even though the insertion sorts first.
		clear := GrossBoardEntry{DriverID: base.DriverID, Date: base.Date, Version: saved[0].Version}
		moved := base
		moved.Date = "2026-10-04"
		moved.LoadRecordID = &current
		if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{clear, moved}); err != nil {
			t.Fatal("atomic move", err)
		}
		// Two new manual claims in one request also conflict before any commit.
		x := GrossBoardEntry{DriverID: projectionDriver, Date: "2026-10-08", LoadNumber: "MANUAL-DUP", DriverRate: "100"}
		y := x
		y.Slot = 1
		reject("manual batch", []GrossBoardEntry{x, y})
		// Historical duplicates may be edited/removed without expanding their claims.
		if _, err = pool.Exec(ctx, `INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,driver_rate) VALUES($1,'2026-10-04',1,'DUP-CLAIM',25)`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		legacy := GrossBoardEntry{DriverID: projectionDriver, Date: "2026-10-04", Slot: 1, LoadNumber: "DUP-CLAIM", DriverRate: "30", Version: 1}
		if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{legacy}); err != nil {
			t.Fatal("legacy edit blocked", err)
		}
		// A freed slot does not reserve the old number forever.
		legacy.Deleted = true
		legacy.Version = 2
		legacy.LoadNumber = ""
		if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{legacy}); err != nil {
			t.Fatal(err)
		}
		// Separate connections race for the same manual number; exactly one commits.
		if err := NewDriverPayRepository(pool).EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		for _, protocol := range []string{"global", "resources"} {
			if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode=$1 WHERE singleton`, protocol); err != nil {
				t.Fatal(err)
			}
			date := "2026-10-09"
			if protocol == "resources" {
				date = "2026-10-10"
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, driver := range []string{projectionDriver, other} {
				go func(id string) {
					<-start
					_, err := repo.SaveChanges(ctx, []GrossBoardEntry{{DriverID: id, Date: date, LoadNumber: "RACE-CLAIM-" + protocol, DriverRate: "100"}}, "")
					results <- err
				}(driver)
			}
			close(start)
			success, conflict := 0, 0
			for range 2 {
				select {
				case err := <-results:
					if err == nil {
						success++
					} else if strings.Contains(err.Error(), "already on Gross Board") {
						conflict++
					} else {
						t.Fatal(err)
					}
				case <-time.After(20 * time.Second):
					t.Fatal("concurrent claim deadlocked")
				}
			}
			if success != 1 || conflict != 1 {
				t.Fatalf("race: %d successes, %d conflicts", success, conflict)
			}
		}
	})
}
