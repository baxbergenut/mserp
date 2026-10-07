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
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
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
				applyLaterTestMigrations(t, ctx, admin, "049")
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

			// A setup week is the first roster week, even without a truck.
			futureInput := DriverInput{FullName: "Future Driver", PayType: "cpm", PayRate: 0.75, Active: true, DispatcherID: &mark.ID, AssignmentWeek: week(2)}
			future, err := repo.CreateDriver(ctx, futureInput)
			if err != nil {
				t.Fatal(err)
			}
			assertVisible := func(offset int, want bool) {
				t.Helper()
				board, err := NewGrossBoardRepository(pool).Get(ctx, monday.AddDate(0, 0, offset*7))
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, d := range board.Drivers {
					if d.ID == future.ID {
						found = true
					}
				}
				if found != want {
					t.Fatalf("future driver visible in week %d: %v, want %v", offset, found, want)
				}
			}
			assertVisible(-1, false)
			assertVisible(0, false)
			assertVisible(2, true)
			// Reactivation starts a new roster period without erasing old history.
			dormantInput := DriverInput{FullName: "Reactivated", PayType: "cpm", PayRate: .75, Active: false, AssignmentWeek: week(-4)}
			dormant, err := repo.CreateDriver(ctx, dormantInput)
			if err != nil {
				t.Fatal(err)
			}
			dormantInput.Active = true
			dormantInput.AssignmentWeek = week(0)
			if _, err = repo.UpdateDriver(ctx, dormant.ID, dormantInput); err != nil {
				t.Fatal(err)
			}
			for _, offset := range []int{-1, 0} {
				board, err := NewGrossBoardRepository(pool).Get(ctx, monday.AddDate(0, 0, offset*7))
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, d := range board.Drivers {
					if d.ID == dormant.ID {
						found = true
					}
				}
				if found != (offset == 0) {
					t.Fatalf("reactivation week %d visible=%v", offset, found)
				}
			}
			oldHistory, err := repo.DriverAssignmentHistory(ctx, dormant.ID)
			if err != nil || len(oldHistory) != 1 || oldHistory[0].AssignedAt.In(loc).Format(time.DateOnly) != week(-4) {
				t.Fatalf("reactivation changed old history: %+v %v", oldHistory, err)
			}
			// Legacy unknown starts must not acquire an invented cutoff.
			exec(`UPDATE drivers SET roster_start_week=NULL WHERE id=$1`, future.ID)
			assertVisible(-1, true)
			exec(`UPDATE drivers SET roster_start_week=$2 WHERE id=$1`, future.ID, week(2))
			// Pre-setup escrow projections cannot manufacture earlier payroll.
			exec(`UPDATE driver_escrows SET start_date=$2::date WHERE driver_id=$1`, future.ID, week(-1))
			tx, err = pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			prior, err := readDriverPaySourceWeek(ctx, tx, monday, future.ID)
			_ = tx.Rollback(ctx)
			if err != nil || len(prior.Drivers) != 0 {
				t.Fatalf("pre-setup payroll: %+v %v", prior, err)
			}
			// Explicit historical plans remain visible for review.
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number) VALUES($1,$2,'EARLIER PLAN')`, future.ID, week(-1))
			assertVisible(-1, true)
			// Existing-driver changes accept future weeks and earlier corrections
			// across planned changes without overlapping history periods.
			futureInput.DispatcherID = &cameron.ID
			futureInput.TruckID = &truck2.ID
			futureInput.AssignmentWeek = week(3)
			if _, err = repo.UpdateDriver(ctx, future.ID, futureInput); err != nil {
				t.Fatal(err)
			}
			futureInput.DispatcherID = &mark.ID
			futureInput.AssignmentWeek = week(1)
			futureInput.TruckID = &truck1.ID
			if _, err = repo.UpdateDriver(ctx, future.ID, futureInput); err != nil {
				t.Fatal(err)
			}
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM truck_driver_assignments a JOIN truck_driver_assignments b ON a.id<b.id AND (a.driver_id=b.driver_id OR a.truck_id=b.truck_id) AND tstzrange(a.assigned_at,a.unassigned_at,'[)') && tstzrange(b.assigned_at,b.unassigned_at,'[)')`).Scan(&overlaps); err != nil || overlaps != 0 {
				t.Fatalf("backdated overlap %d: %v", overlaps, err)
			}
			// Same-week and later finalized payroll protect every fleet entry point.
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,$2,'{}')`, future.ID, week(2))
			futureInput.AssignmentWeek = week(1)
			futureInput.DispatcherID = &cameron.ID
			if _, err = repo.UpdateDriver(ctx, future.ID, futureInput); err == nil || !strings.Contains(err.Error(), "Reopen finalized payroll") {
				t.Fatalf("dispatcher settlement guard: %v", err)
			}
			futureInput.DispatcherID = &mark.ID
			futureInput.TruckID = &truck2.ID
			if _, err = repo.UpdateDriver(ctx, future.ID, futureInput); err == nil || !strings.Contains(err.Error(), "Reopen finalized payroll") {
				t.Fatalf("truck settlement guard: %v", err)
			}
			if _, err = repo.UpdateTruck(ctx, truck1.ID, TruckInput{UnitNumber: "ONE", Status: "assigned", Active: true, DriverID: &other.ID, AssignmentWeek: week(2)}); err == nil || !strings.Contains(err.Error(), "Reopen finalized payroll") {
				t.Fatalf("displaced driver guard: %v", err)
			}
			if _, err = repo.UpdateDispatcher(ctx, mark.ID, DispatcherInput{FullName: "Mark", Active: true, DriverIDs: []string{}, AssignmentWeek: week(2)}); err == nil || !strings.Contains(err.Error(), "Reopen finalized payroll") {
				t.Fatalf("dispatcher form guard: %v", err)
			}
			// Frozen payroll on a different driver does not prevent this change.
			if _, err = repo.UpdateDriver(ctx, other.ID, DriverInput{FullName: "Other", PayType: "cpm", Active: true, DispatcherID: &cameron.ID, AssignmentWeek: week(0)}); err != nil {
				t.Fatal(err)
			}
			// A boundary after all finalized payroll is valid.
			futureInput.AssignmentWeek = week(3)
			if _, err = repo.UpdateDriver(ctx, future.ID, futureInput); err != nil {
				t.Fatal(err)
			}
			// Reopening permits backdating across the previous boundary.
			exec(`UPDATE payroll_settlements SET finalized=false WHERE driver_id=$1`, future.ID)
			futureInput.AssignmentWeek = week(0)
			futureInput.TruckID = &truck1.ID
			if _, err = repo.UpdateDriver(ctx, future.ID, futureInput); err != nil {
				t.Fatal(err)
			}
			// Truck settlements protect the target truck even while unassigned.
			exec(`INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized) SELECT id,$2,owner_id,'{}',true FROM trucks WHERE id=$1`, truck2.ID, week(0))
			futureInput.TruckID = &truck2.ID
			if _, err = repo.UpdateDriver(ctx, future.ID, futureInput); err == nil || !strings.Contains(err.Error(), "Reopen finalized investor payroll") {
				t.Fatalf("investor settlement guard: %v", err)
			}
			// A former driver's period ending at the boundary is unaffected.
			spare, err := repo.CreateTruck(ctx, TruckInput{UnitNumber: "SPARE", Status: "available", Active: true})
			if err != nil {
				t.Fatal(err)
			}
			formerInput := DriverInput{FullName: "Former", PayType: "cpm", Active: true, TruckID: &spare.ID, AssignmentWeek: week(-2)}
			former, err := repo.CreateDriver(ctx, formerInput)
			if err != nil {
				t.Fatal(err)
			}
			formerInput.TruckID = nil
			formerInput.AssignmentWeek = week(-1)
			if _, err = repo.UpdateDriver(ctx, former.ID, formerInput); err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,$2,'{}')`, former.ID, week(0))
			if _, err = repo.UpdateTruck(ctx, spare.ID, TruckInput{UnitNumber: "SPARE", Status: "assigned", Active: true, DriverID: &other.ID, AssignmentWeek: week(-1)}); err != nil {
				t.Fatalf("unaffected former driver blocked assignment: %v", err)
			}
		})
	}
}
