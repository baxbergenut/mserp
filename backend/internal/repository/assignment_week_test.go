package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAssignmentWeekDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL to a disposable test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") {
		t.Fatal("a separate _test database is required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(loc)
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(int(now.Weekday())+6)%7)
	week := func(offset int) string { return monday.AddDate(0, 0, offset*7).Format(time.DateOnly) }
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("assignment_week_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			init, err := os.ReadFile("../../sql/init.sql")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				before, _, ok := strings.Cut(string(init), "-- Keep the old trigger contract for imports")
				if !ok {
					t.Fatal("missing week migration boundary")
				}
				exec(before + "COMMIT;")
				migration, err := os.ReadFile("../../sql/047_assignment_effective_week.sql")
				if err != nil {
					t.Fatal(err)
				}
				exec(string(migration))
				updaterMigration, err := os.ReadFile("../../sql/049_dispatcher_updaters.sql")
				if err != nil {
					t.Fatal(err)
				}
				exec(string(updaterMigration))
			} else {
				exec(string(init))
			}
			exec(`GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app`)
			appcfg := cfg.Copy()
			appcfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appcfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, appcfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := NewFleetRepository(pool)
			cameron, err := repo.CreateDispatcher(ctx, DispatcherInput{FullName: "Cameron", Active: true})
			if err != nil {
				t.Fatal(err)
			}
			mark, err := repo.CreateDispatcher(ctx, DispatcherInput{FullName: "Mark", Active: true})
			if err != nil {
				t.Fatal(err)
			}
			truck1, err := repo.CreateTruck(ctx, TruckInput{UnitNumber: "ONE", Status: "available", Active: true})
			if err != nil {
				t.Fatal(err)
			}
			truck2, err := repo.CreateTruck(ctx, TruckInput{UnitNumber: "TWO", Status: "available", Active: true})
			if err != nil {
				t.Fatal(err)
			}
			input := DriverInput{FullName: "Didi", PayType: "cpm", PayRate: 0.75, Active: true, DispatcherID: &cameron.ID, TruckID: &truck1.ID, AssignmentWeek: week(-4)}
			driver, err := repo.CreateDriver(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			// No-op profile saves retain the exact original truck period.
			before, err := repo.DriverAssignmentHistory(ctx, driver.ID)
			if err != nil {
				t.Fatal(err)
			}
			input.AssignmentWeek = week(0)
			if _, err = repo.UpdateDriver(ctx, driver.ID, input); err != nil {
				t.Fatal(err)
			}
			after, err := repo.DriverAssignmentHistory(ctx, driver.ID)
			if err != nil || len(after) != len(before) {
				t.Fatalf("no-op history: %v %v", after, err)
			}
			for i := range before {
				if before[i].ID != after[i].ID || !before[i].AssignedAt.Equal(after[i].AssignedAt) {
					t.Fatal("no-op changed history")
				}
			}
			input.DispatcherID = &mark.ID
			input.TruckID = &truck2.ID
			input.AssignmentWeek = week(-1)
			if _, err = repo.UpdateDriver(ctx, driver.ID, input); err != nil {
				t.Fatal(err)
			}
			check := func(offset int, dispatcher, truck string) {
				t.Helper()
				board, err := NewGrossBoardRepository(pool).Get(ctx, monday.AddDate(0, 0, offset*7))
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range board.Drivers {
					if d.ID == driver.ID {
						if d.DispatcherName != dispatcher || d.TruckUnit != truck {
							t.Fatalf("week %d: %+v", offset, d)
						}
						return
					}
				}
				t.Fatal("driver missing")
			}
			check(-2, "Cameron", "ONE")
			check(-1, "Mark", "TWO")
			check(0, "Mark", "TWO")
			// Payroll headings must follow the same historical assignment.
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate) VALUES($1,$2,'PLAN',1000)`, driver.ID, week(-2))
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			report, err := readDriverPaySourceWeek(ctx, tx, monday.AddDate(0, 0, -14), driver.ID)
			_ = tx.Rollback(ctx)
			if err != nil || len(report.Drivers) != 1 || report.Drivers[0].DispatcherName != "Cameron" || report.Drivers[0].TruckUnit != "ONE" {
				t.Fatalf("payroll history: %+v %v", report, err)
			}
			// Repeated edits within the same week create no overlapping periods.
			input.DispatcherID = &cameron.ID
			input.TruckID = &truck1.ID
			if _, err = repo.UpdateDriver(ctx, driver.ID, input); err != nil {
				t.Fatal(err)
			}
			check(-2, "Cameron", "ONE")
			check(-1, "Cameron", "ONE")
			var overlaps int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM truck_driver_assignments a JOIN truck_driver_assignments b ON a.id<b.id AND (a.driver_id=b.driver_id OR a.truck_id=b.truck_id) AND tstzrange(a.assigned_at,a.unassigned_at,'[)') && tstzrange(b.assigned_at,b.unassigned_at,'[)')`).Scan(&overlaps); err != nil || overlaps != 0 {
				t.Fatalf("overlap %d: %v", overlaps, err)
			}
			// Invalid/crossing boundaries roll the whole profile edit back.
			input.FullName = "Must Roll Back"
			input.DispatcherID = &mark.ID
			input.AssignmentWeek = week(-3)
			if _, err = repo.UpdateDriver(ctx, driver.ID, input); err == nil {
				t.Fatal("accepted boundary before a later change")
			}
			input.AssignmentWeek = week(1)
			if _, err = repo.UpdateDriver(ctx, driver.ID, input); err == nil {
				t.Fatal("accepted future week")
			}
			input.AssignmentWeek = monday.AddDate(0, 0, -1).Format(time.DateOnly)
			if _, err = repo.UpdateDriver(ctx, driver.ID, input); err == nil {
				t.Fatal("accepted Sunday")
			}
			saved, err := repo.GetDriver(ctx, driver.ID)
			if err != nil || saved.FullName != "Didi" {
				t.Fatalf("failed mutation changed profile: %+v %v", saved, err)
			}
			// Truck-side assignment displaces the previous driver at the same boundary.
			other, err := repo.CreateDriver(ctx, DriverInput{FullName: "Other", PayType: "cpm", Active: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.UpdateTruck(ctx, truck1.ID, TruckInput{UnitNumber: "ONE", Status: "assigned", Active: true, DriverID: &other.ID, AssignmentWeek: week(0)}); err != nil {
				t.Fatal(err)
			}
			check(-1, "Cameron", "ONE")
			check(0, "Cameron", "")
			// Dispatcher form removal also has an effective week, including unassignment.
			if _, err = repo.UpdateDispatcher(ctx, cameron.ID, DispatcherInput{FullName: "Cameron", Active: true, DriverIDs: []string{}, AssignmentWeek: week(0)}); err != nil {
				t.Fatal(err)
			}
			check(-1, "Cameron", "ONE")
			check(0, "Unassigned", "")
		})
	}
}
