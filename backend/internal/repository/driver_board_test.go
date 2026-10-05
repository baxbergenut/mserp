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

func TestDriverBoardTypes(t *testing.T) {
	for _, tc := range []struct {
		pay                  string
		owner, investor, own bool
		want                 string
	}{
		{"gross_percentage", true, true, true, "O"}, {"cpm", false, false, false, "M"},
		{"gross_percentage", false, true, false, "%-O"}, {"cpm", false, true, false, "M-O"},
		{"gross_percentage", true, true, false, "%-O"}, {"gross_percentage", false, false, false, "%"},
		{"gross_percentage", false, true, true, "O"}, {"cpm", true, true, true, "M"},
	} {
		if got := driverBoardType(tc.pay, tc.owner, tc.investor, tc.own); got != tc.want {
			t.Errorf("%+v: got %s", tc, got)
		}
	}
}

func TestDriverBoardDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_DRIVER_BOARD_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("a disposable local _test database is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/045_driver_board.sql")
	if err != nil {
		t.Fatal(err)
	}
	historyMigration, err := os.ReadFile("../../sql/046_driver_board_history.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), string(historyMigration)) {
		t.Fatal("history migration must match fresh schema")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("driver_board_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "migration" {
				before, _, ok := strings.Cut(string(source), string(migration))
				if !ok {
					t.Fatal("migration must match fresh schema")
				}
				exec(before)
				exec(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active) VALUES('Legacy','legacy','cpm',0,false)`)
				exec(string(migration))
				exec(string(historyMigration))
				for _, name := range []string{"047_assignment_effective_week.sql", "048_status_board_loads.sql", "049_dispatcher_updaters.sql", "050_five_eld_locations.sql"} {
					migration, err := os.ReadFile("../../sql/" + name)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(strings.ReplaceAll(string(source), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n")) {
						t.Fatal("migration must match schema", name)
					}
					exec(string(migration))
				}
			} else {
				exec(string(source))
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			// Test ownership separately: blanket grants must not hide migration omissions.
			var owner string
			if err := admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename='driver_board'`, schema).Scan(&owner); err != nil || owner != "mserp_app" {
				t.Fatalf("table ownership: %s %v", owner, err)
			}
			if err := admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename='driver_board_history'`, schema).Scan(&owner); err != nil || owner != "mserp_app" {
				t.Fatalf("history ownership: %s %v", owner, err)
			}
			if err := admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename='five_eld_locations'`, schema).Scan(&owner); err != nil || owner != "mserp_app" {
				t.Fatalf("Five ELD location ownership: %s %v", owner, err)
			}
			appcfg := cfg.Copy()
			appcfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appcfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, appcfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			fleet := NewFleetRepository(pool)
			repo := NewDriverBoardRepository(pool)
			testDriverBoardHistory(t, ctx, pool, repo, fleet)
			testDriverBoardLoads(t, ctx, pool, repo, fleet)
			testCurrentLoadAutofill(t, ctx, pool, repo, fleet)
			testBoardProgress(t, ctx, pool, repo, fleet)
			home := "Louisville, KY"
			input := DriverInput{FullName: "Board Driver", PayType: "cpm", PayRate: 0.65, Active: true, DriverHome: &home}
			driver, err := fleet.CreateDriver(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			other, err := fleet.CreateDriver(ctx, DriverInput{FullName: "Other Driver", PayType: "gross_percentage", PayRate: 80, Active: true})
			if err != nil {
				t.Fatal(err)
			}
			vin := "1M8GDM9AXKP042788"
			if _, err = fleet.CreateTruck(ctx, TruckInput{UnitNumber: "ELD-17", VIN: &vin, DriverID: &driver.ID, Status: "available", Active: true, IsCompanyOwned: true}); err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO five_eld_locations(vin,provider_truck_number,latitude,longitude,reported_at,fetched_at)
			 VALUES($1,'17',41.881,-87.623,now(),now())`, vin)
			exec(`INSERT INTO five_eld_sync_state(singleton,last_attempt_at,last_success_at) VALUES(true,now(),now())`)
			repo = NewDriverBoardRepository(pool, true)
			week, _ := time.Parse("2006-01-02", "2026-09-28")
			exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(999,'BOARD-LOAD','Delivered',1234.56,1234.56,450.25,'{}')`)
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate,entered_original_rate) VALUES($1,'2026-09-28','BOARD-LOAD',999,1100.10,1250.15)`, driver.ID)
			board, err := repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			if len(board.Drivers) != 2 || len(board.GrossEntries) != 1 || board.GrossEntries[0].OriginalRate != "1250.15" || board.GrossEntries[0].Miles != "450.25" {
				t.Fatalf("board read: %+v", board)
			}
			if !board.ELD.Configured || board.Drivers[0].Location == nil || board.Drivers[0].Location.Latitude != 41.881 || board.Drivers[0].Location.Longitude != -87.623 {
				t.Fatalf("Five ELD board location: %+v", board.Drivers)
			}
			exec(`UPDATE five_eld_locations SET reported_at=now()-interval '16 minutes' WHERE vin=$1`, vin)
			staleBoard, err := repo.Get(ctx, week)
			if err != nil || staleBoard.Drivers[0].Location != nil {
				t.Fatalf("stale Five ELD location was exposed: %+v err=%v", staleBoard.Drivers, err)
			}
			var draft DriverBoardEntry
			for _, e := range board.Entries {
				if e.DriverID == driver.ID {
					draft = e
				}
			}
			if draft.DriverHome != home || draft.Version != 0 {
				t.Fatal("profile home not loaded")
			}
			draft.CurrentLoad = "Manual dispatch"
			draft.Status = "ENROUTE"
			draft.DriverHome = "Memphis, TN"
			draft.Notes = "Gate 2"
			saved, err := repo.Save(ctx, []DriverBoardEntry{draft})
			if err != nil {
				t.Fatal(err)
			}
			if saved[0].Version != 1 || saved[0].HomeVersion != 1 {
				t.Fatalf("tokens: %+v", saved)
			}
			if _, err = repo.Save(ctx, []DriverBoardEntry{draft}); !errors.Is(err, ErrDriverBoardConflict) {
				t.Fatal("stale initial write accepted", err)
			}
			profile, err := fleet.GetDriver(ctx, driver.ID)
			if err != nil || profile.DriverHome != "Memphis, TN" {
				t.Fatal("board home not synced", err)
			}
			if _, err = fleet.UpdateDriver(ctx, driver.ID, input); !errors.Is(err, ErrDriverBoardConflict) {
				t.Fatal("stale profile overwrote home", err)
			}
			input.DriverHome = nil
			profile, err = fleet.UpdateDriver(ctx, driver.ID, input)
			if err != nil || profile.DriverHome != "Memphis, TN" {
				t.Fatal("legacy profile update erased home", err)
			}
			input.DriverHome = &home
			input.HomeVersion = profile.HomeVersion
			profile, err = fleet.UpdateDriver(ctx, driver.ID, input)
			if err != nil || profile.HomeVersion != 2 {
				t.Fatal("profile home update", err)
			}
			if _, err = repo.Save(ctx, saved); !errors.Is(err, ErrDriverBoardConflict) {
				t.Fatal("profile/board conflict not enforced", err)
			}
			// A conflict anywhere in a batch must roll back the other row too.
			if _, err = repo.Save(ctx, []DriverBoardEntry{{DriverID: other.ID, Notes: "must roll back"}, draft}); !errors.Is(err, ErrDriverBoardConflict) {
				t.Fatal("batch conflict", err)
			}
			var count int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board WHERE driver_id=$1`, other.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial batch committed", err)
			}
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_board_history WHERE driver_id=$1`, other.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal("rolled back batch leaked history", err)
			}
			next, err := repo.Get(ctx, week.AddDate(0, 0, 7))
			if err != nil {
				t.Fatal(err)
			}
			if len(next.GrossEntries) != 0 {
				t.Fatal("weekly loads carried forward")
			}
			for _, e := range next.Entries {
				if e.DriverID == driver.ID && (e.Notes != "Gate 2" || e.DriverHome != home) {
					t.Fatal("live fields did not carry forward")
				}
			}
			cleared := saved[0]
			cleared.HomeVersion = profile.HomeVersion
			cleared.DriverHome = home
			cleared.CurrentLoad = ""
			cleared.Notes = ""
			cleared.Status = ""
			clearedRows, err := repo.Save(ctx, []DriverBoardEntry{cleared})
			if err != nil || clearedRows[0].Version != 2 {
				t.Fatal("clear lost version", err)
			}
			exec(`UPDATE drivers SET active=false WHERE id=$1`, driver.ID)
			if _, err = repo.Save(ctx, clearedRows); !errors.Is(err, ErrDriverBoardConflict) {
				t.Fatal("inactive driver save accepted", err)
			}
		})
	}
}
