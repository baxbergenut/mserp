package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/datatruck"
	"os"
	"strings"
	"testing"
	"time"
)

const splitFixture = `{"id":8101,"load_id":"SPLIT-1","status":"delivered","total_pay":1813,"load_pay":1813,"total_miles":555,"trips":[{"id":101,"trip_id":"T-01","driver__full_name":"Split First","truck__unit_number":"S-A","total_load_pay":500,"mile":0,"empty_mile":40,"trip_begin_date":"2026-10-19","trip_end_date":"2026-10-20","pickup":{"city":"A","state":"NJ"},"delivery":{"city":"B","state":"NJ"}},{"id":102,"trip_id":"T-02","driver__full_name":"Split Second","truck__unit_number":"S-B","total_load_pay":1313,"mile":475,"empty_mile":40,"trip_begin_date":"2026-10-20","trip_end_date":"2026-10-20","pickup":{"city":"B","state":"NJ"},"delivery":{"city":"C","state":"PA"}}]}`

func TestLoadTripsSerialization(t *testing.T) {
	var load datatruck.Load
	if err := json.Unmarshal([]byte(splitFixture), &load); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(load)
	if err != nil {
		t.Fatal(err)
	}
	var round datatruck.Load
	if err = json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	if len(round.Trips) != 2 || round.Trips[0].ID != 101 || string(*round.Trips[1].TotalLoadPay) != "1313" {
		t.Fatal("trip allocations lost during import", string(raw))
	}
}

func TestLoadTripsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set local disposable MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	for _, migrated := range []bool{false, true} {
		t.Run(fmt.Sprintf("migrated=%v", migrated), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			admin, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close(context.Background())
			name := fmt.Sprintf("load_trips_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{name}.Sanitize()
			if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
				t.Fatal(err)
			}
			defer admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
			conf := cfg.Copy()
			conf.MaxConns = 1
			conf.ConnConfig.RuntimeParams["search_path"] = name + ",public"
			pool, err := pgxpool.NewWithConfig(ctx, conf)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := pool.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			init, err := os.ReadFile("../../sql/init.sql")
			if err != nil {
				t.Fatal(err)
			}
			migration, err := os.ReadFile("../../sql/152_gross_board_load_trips.sql")
			if err != nil {
				t.Fatal(err)
			}
			assignmentView, err := os.ReadFile("../../sql/167_clustie_load_assignments.sql")
			if err != nil {
				t.Fatal(err)
			}
			reviewMigration, err := os.ReadFile("../../sql/175_reviewed_load_splits.sql")
			if err != nil {
				t.Fatal(err)
			}
			if migrated {
				initial := strings.ReplaceAll(string(init), "\r\n", "\n")
				body := strings.ReplaceAll(string(migration), "\r\n", "\n")
				if strings.Count(initial, body) != 1 {
					t.Fatal("expected exactly one trip migration in init.sql")
				}
				initial = strings.Replace(initial, strings.ReplaceAll(string(reviewMigration), "\r\n", "\n"), "", 1)
				initial = strings.Replace(initial, strings.ReplaceAll(string(assignmentView), "\r\n", "\n"), "", 1)
				exec(strings.Replace(initial, body, "", 1))
			} else {
				exec(string(init))
			}
			const a = "81000000-0000-0000-0000-000000000001"
			const b = "81000000-0000-0000-0000-000000000002"
			exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES($1,'Split First','split first','cpm',1),($2,'Split Second','split second','cpm',1)`, a, b)
			exec(`INSERT INTO loads(id,load_id,status,total_pay,load_pay,total_miles,raw_payload) VALUES(8101,'SPLIT-1','delivered',1813,1813,555,$1::jsonb)`, splitFixture)
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,entered_original_rate,entered_miles,driver_rate) VALUES($1,'2026-10-19','SPLIT-1',8101,1813,555,200),($2,'2026-10-20','SPLIT-1',8101,1813,555,1000)`, a, b)
			if migrated {
				exec(string(migration))
				exec(string(assignmentView))
				exec(string(reviewMigration))
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app; SET ROLE mserp_app`)
			exec(`INSERT INTO trucks(id,unit_number) VALUES('82000000-0000-0000-0000-000000000001','S-A'),('82000000-0000-0000-0000-000000000002','S-B');UPDATE loads SET truck_id='82000000-0000-0000-0000-000000000002',truck_unit='S-B' WHERE id=8101`)
			exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES
 ('82000000-0000-0000-0000-000000000001',$1,'2026-10-01 00:00-04'),
 ('82000000-0000-0000-0000-000000000002',$2,'2026-10-01 00:00-04')`, a, b)
			week, _ := time.Parse(time.DateOnly, "2026-10-19")
			repo := NewGrossBoardRepository(pool)
			board, err := repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			if len(board.Entries) != 2 {
				t.Fatal("missing entries", board)
			}
			exec(`UPDATE loads SET raw_payload=jsonb_set(raw_payload,'{trips}','[]') WHERE id=8101`)
			held, err := repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range held.Entries {
				if !e.TripReview || e.OriginalRate != "" {
					t.Fatal("unselected split lost review when source trips disappeared", e)
				}
			}
			exec(`UPDATE loads SET raw_payload=$1::jsonb WHERE id=8101`, splitFixture)
			entries := board.Entries
			for i := range entries {
				e := &entries[i]
				if !e.Split || !e.TripReview || e.OriginalRate != "" || e.SystemOriginalRate != "" || len(e.Trips) != 2 {
					t.Fatal("unselected split counted as whole order", e)
				}
				id, gross, miles := 101, "500", "40"
				if e.DriverID == b {
					id, gross, miles = 102, "1313", "515"
				}
				e.LoadTripID = &id
				e.AcceptSystemValues = true
				e.OriginalRate = gross
				e.Miles = miles
			}
			stale := append([]GrossBoardEntry(nil), entries...)
			saved, err := repo.SaveEntries(ctx, entries)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range saved {
				wantGross, wantMiles, wantPay := "500.00", "40.00", "200.00"
				if e.DriverID == b {
					wantGross, wantMiles, wantPay = "1313.00", "515.00", "1000.00"
				}
				if e.OriginalRate != wantGross || e.Miles != wantMiles || e.DriverRate != wantPay || e.TripReview {
					t.Fatal("wrong selected allocation", e)
				}
			}
			// Stale clients cannot overwrite a later selection.
			if _, err = repo.SaveEntries(ctx, stale); err == nil {
				t.Fatal("stale save accepted")
			}
			// One trip cannot pay a second driver.
			conflict := saved[1]
			id := 101
			conflict.LoadTripID = &id
			conflict.AcceptSystemValues = true
			conflict.OriginalRate = "500"
			conflict.Miles = "40"
			if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{conflict}); err == nil {
				t.Fatal("trip assigned to two drivers")
			}
			// Repeating a trip must not create a second earning.
			repeat := saved[0]
			repeat.Slot = 1
			repeat.Version = 0
			repeat.AcceptSystemValues = true
			// Use an earlier second date in the same historical week to avoid dispatch availability rules.
			repeat.Date = "2026-10-21"
			repeated, err := repo.SaveEntries(ctx, []GrossBoardEntry{repeat})
			if err != nil {
				t.Fatal(err)
			}
			if !repeated[0].Duplicate {
				t.Fatal("same trip was counted twice")
			}
			var total string
			var loads int
			if err = pool.QueryRow(ctx, companyDashboardCTE+` SELECT sum(gross)::text,count(DISTINCT matched_id) FROM dated WHERE inclusion='included'`, "2026-10-19", "2026-10-25").Scan(&total, &loads); err != nil {
				t.Fatal(err)
			}
			if total != "1813.00" || loads != 1 {
				t.Fatal("dashboard double counts split", total, loads)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			truckMap, err := truckPayLoadMap(ctx, tx, "2026-10-19")
			if err != nil {
				tx.Rollback(ctx)
				t.Fatal(err)
			}
			for _, e := range saved {
				want := "82000000-0000-0000-0000-000000000001"
				if e.DriverID == b {
					want = "82000000-0000-0000-0000-000000000002"
				}
				if truckMap[e.DriverID+":"+payCommentKey(e.Date, e.Slot, e.LoadNumber)].TruckID != want {
					tx.Rollback(ctx)
					t.Fatal("trip ownership ignored the Clustie assignment", truckMap)
				}
			}
			pay, err := readDriverPaySourceWeek(ctx, tx, week, "")
			tx.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(pay.Drivers) != 2 {
				t.Fatal("missing payroll drivers", pay)
			}
			for _, d := range pay.Drivers {
				if len(d.Loads) != 1 {
					t.Fatal(d)
				}
				l := d.Loads[0]
				want := "40.00"
				if d.ID == b {
					want = "515.00"
				}
				if l.TotalMiles != want || l.Fee != want || l.LoadTripID == nil {
					t.Fatal("payroll uses parent miles", l)
				}
			}
			exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,load_trip_id,entered_original_rate,entered_miles,driver_rate) SELECT driver_id,'2026-10-26',1,load_number,load_record_id,load_trip_id,entered_original_rate,entered_miles,driver_rate FROM gross_board_entries WHERE driver_id=$1`, a)
			nextWeek := week.AddDate(0, 0, 7)
			next, err := repo.Get(ctx, nextWeek)
			if err != nil {
				t.Fatal(err)
			}
			if len(next.Entries) != 1 || !next.Entries[0].Duplicate {
				t.Fatal("cross-week trip counted again", next.Entries)
			}
			first := saved[0]
			accept := PaySourceAcceptance{DriverID: first.DriverID, Date: first.Date, Slot: first.Slot, Version: first.Version, LoadRecordID: 8101, OriginalRate: first.SystemOriginalRate, Miles: first.SystemMiles}
			if err = NewDriverPayRepository(pool).AcceptPaySource(ctx, accept); err != nil {
				t.Fatal("payroll trip acceptance", err)
			}
			// Missing source trips hold for review without falling back to parent amounts.
			exec(`UPDATE loads SET raw_payload=jsonb_set(raw_payload,'{trips}','[]') WHERE id=8101`)
			board, err = repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range board.Entries {
				if !e.TripReview || e.SystemOriginalRate != "" || e.OriginalRate != "" {
					t.Fatal("missing trip fell back to whole load", e)
				}
			}
			exec(`UPDATE loads SET raw_payload=$1::jsonb WHERE id=8101`, splitFixture)
			board, err = repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE loads SET raw_payload=jsonb_set(raw_payload,'{trips,0,total_load_pay}','700') WHERE id=8101`)
			mismatch, err := repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range mismatch.Entries {
				if !e.TripReview || e.SystemOriginalRate != "" {
					t.Fatal("unreconciled trips accepted", e)
				}
				if !strings.Contains(e.TripReviewReason, "$2,013.00") || !strings.Contains(e.TripReviewReason, "$1,813.00") {
					t.Fatal("missing actionable allocation mismatch", e.TripReviewReason)
				}
			}
			matches, err := repo.SearchLoads(ctx, "SPLIT-1")
			if err != nil || len(matches) != 1 || matches[0].TripReviewReason == "" {
				t.Fatal("load search must retain invalid allocation reason", err, matches)
			}
			if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{board.Entries[0]}); err == nil || !strings.Contains(err.Error(), "$2,013.00") {
				t.Fatal("stale selection must explain the source mismatch", err)
			}
			checkReviewedLoadSplit(t, ctx, pool, repo)
			exec(`UPDATE loads SET raw_payload=$1::jsonb WHERE id=8101`, splitFixture)
			// Financial history stays frozen; deletion cannot bypass the guard.
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19','{}')`, a)
			for _, e := range board.Entries {
				if e.DriverID != a {
					continue
				}
				e.DriverRate = "999"
				if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{e}); err == nil {
					t.Fatal("finalized split changed")
				}
				e.LoadNumber = ""
				e.LoadRecordID = nil
				e.LoadTripID = nil
				e.OriginalRate = ""
				e.DriverRate = ""
				e.Miles = ""
				e.EnteredOriginalRate = ""
				e.EnteredMiles = ""
				if _, err = repo.SaveEntries(ctx, []GrossBoardEntry{e}); err == nil {
					t.Fatal("finalized split deleted")
				}
			}
		})
	}
}
