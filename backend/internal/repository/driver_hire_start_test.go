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

func TestDriverHireStartDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("driver_hire_start_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(t *testing.T, sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(t, `CREATE SCHEMA `+quoted+`; SET search_path TO `+quoted+`,public`)
			defer admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			source, err := os.ReadFile("../../sql/init.sql")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				before, _, ok := strings.Cut(string(source), "-- Driver address consolidation (migration 080).")
				if !ok {
					t.Fatal("missing migration boundary")
				}
				exec(t, before)
				applyLaterTestMigrations(t, ctx, admin, "079")
			} else {
				exec(t, string(source))
			}
			exec(t, `GRANT USAGE ON SCHEMA `+quoted+` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+quoted+` TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA `+quoted+` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, app)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			fleet := NewFleetRepository(pool)
			parse := func(date string) *time.Time { d, _ := time.Parse(time.DateOnly, date); return &d }
			sequence := 0
			fixture := func(t *testing.T) (Driver, DriverInput) {
				t.Helper()
				sequence++
				dp, err := fleet.CreateDispatcher(ctx, DispatcherInput{FullName: fmt.Sprintf("Hire Dispatcher %d", sequence), Active: true})
				if err != nil {
					t.Fatal(err)
				}
				input := DriverInput{FullName: fmt.Sprintf("Hire Driver %d", sequence), PayType: "cpm", PayRate: .75, Active: true, HireDate: parse("2026-10-07"), DispatcherID: &dp.ID,
					NewTruck: &TruckInput{UnitNumber: fmt.Sprintf("HIRE-%d", sequence), Status: "available", Active: true}}
				d, err := fleet.CreateDriver(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				input.NewTruck = nil
				input.TruckID = d.TruckID
				return d, input
			}
			state := func(t *testing.T, driver string) string {
				t.Helper()
				var s string
				if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('hire',d.hire_date,'roster',d.roster_start_week,
 'truck',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.assigned_at,a.id) FROM truck_driver_assignments a WHERE driver_id=d.id),
 'dispatcher',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.assigned_at,a.id) FROM driver_dispatcher_assignments a WHERE driver_id=d.id),
 'escrow',(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM driver_escrows e WHERE driver_id=d.id))::text FROM drivers d WHERE id=$1`, driver).Scan(&s); err != nil {
					t.Fatal(err)
				}
				return s
			}
			checkDates := func(t *testing.T, driver, week, escrowDate string) {
				t.Helper()
				var correct bool
				if err := pool.QueryRow(ctx, `SELECT roster_start_week=$2::date
 AND (SELECT bool_and((assigned_at AT TIME ZONE 'America/New_York')::date=$2::date) FROM truck_driver_assignments WHERE driver_id=d.id)
 AND (SELECT bool_and((assigned_at AT TIME ZONE 'America/New_York')::date=$2::date) FROM driver_dispatcher_assignments WHERE driver_id=d.id)
 AND (SELECT bool_and(start_date=$3::date AND amount=2500 AND opening_paid=0) FROM driver_escrows WHERE driver_id=d.id)
 FROM drivers d WHERE id=$1`, driver, week, escrowDate).Scan(&correct); err != nil || !correct {
					t.Fatalf("dates not synchronized: %s %v", state(t, driver), err)
				}
			}
			t.Run("forward and backward reports", func(t *testing.T) {
				d, input := fixture(t)
				input.HireDate = parse("2026-10-12")
				if _, err := fleet.UpdateDriver(ctx, d.ID, input); err != nil {
					t.Fatal(err)
				}
				checkDates(t, d.ID, "2026-10-12", "2026-10-12")
				for _, week := range []string{"2026-10-05", "2026-10-12"} {
					board, err := NewGrossBoardRepository(pool).Get(ctx, *parse(week))
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, row := range board.Drivers {
						found = found || row.ID == d.ID
					}
					if found != (week == "2026-10-12") {
						t.Fatalf("board visibility for %s: %v", week, found)
					}
					pay, err := NewDriverPayRepository(pool).GetDriverWeek(ctx, *parse(week), d.ID)
					if err != nil || (len(pay.Drivers) == 1) != (week == "2026-10-12") {
						t.Fatalf("pay visibility for %s: %+v %v", week, pay, err)
					}
				}
				input.HireDate = parse("2026-09-30")
				if _, err := fleet.UpdateDriver(ctx, d.ID, input); err != nil {
					t.Fatal(err)
				}
				checkDates(t, d.ID, "2026-09-28", "2026-09-30")
				before := state(t, d.ID)
				if _, err := fleet.UpdateDriver(ctx, d.ID, input); err != nil || state(t, d.ID) != before {
					t.Fatalf("unchanged hire date changed setup: %v", err)
				}
			})
			t.Run("same week and independent escrow", func(t *testing.T) {
				d, input := fixture(t)
				exec(t, `INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount) VALUES($1,'Independent','2026-10-07',100)`, d.ID)
				input.HireDate = parse("2026-10-09")
				if _, err := fleet.UpdateDriver(ctx, d.ID, input); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM driver_escrows WHERE driver_id=$1 AND ((amount=2500 AND start_date='2026-10-09') OR (amount=100 AND start_date='2026-10-07'))`, d.ID).Scan(&count); err != nil || count != 2 {
					t.Fatalf("escrow selection: %d %v", count, err)
				}
			})
			t.Run("newly known hire and cleared date preserve unknown history", func(t *testing.T) {
				d, input := fixture(t)
				exec(t, `UPDATE drivers SET hire_date=NULL,roster_start_week=NULL WHERE id=$1`, d.ID)
				exec(t, `UPDATE driver_dispatcher_assignments SET start_known=false WHERE driver_id=$1`, d.ID)
				input.HireDate = parse("2026-10-12")
				if _, err := fleet.UpdateDriver(ctx, d.ID, input); err != nil {
					t.Fatal(err)
				}
				var correct bool
				if err := pool.QueryRow(ctx, `SELECT roster_start_week='2026-10-12'
 AND (SELECT bool_and((assigned_at AT TIME ZONE 'America/New_York')::date='2026-10-05' AND NOT start_known) FROM driver_dispatcher_assignments WHERE driver_id=d.id)
 FROM drivers d WHERE id=$1`, d.ID).Scan(&correct); err != nil || !correct {
					t.Fatalf("unknown history changed: %s %v", state(t, d.ID), err)
				}
				input.HireDate = nil
				if _, err := fleet.UpdateDriver(ctx, d.ID, input); err != nil {
					t.Fatal(err)
				}
				var roster string
				if err := pool.QueryRow(ctx, `SELECT roster_start_week::text FROM drivers WHERE id=$1`, d.ID).Scan(&roster); err != nil || roster != "2026-10-12" {
					t.Fatalf("clearing hire erased roster: %s %v", roster, err)
				}
			})
			t.Run("later transfers and reactivation remain", func(t *testing.T) {
				d, input := fixture(t)
				next, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "HIRE-LATER", Active: true, Status: "available"})
				if err != nil {
					t.Fatal(err)
				}
				input.TruckID, input.AssignmentWeek = &next.ID, "2026-10-26"
				if _, err := fleet.UpdateDriver(ctx, d.ID, input); err != nil {
					t.Fatal(err)
				}
				exec(t, `UPDATE drivers SET roster_start_week='2026-10-26' WHERE id=$1`, d.ID)
				input.HireDate = parse("2026-10-14")
				if _, err := fleet.UpdateDriver(ctx, d.ID, input); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM truck_driver_assignments WHERE driver_id=$1 AND
 ((truck_id=$2 AND (assigned_at AT TIME ZONE 'America/New_York')::date='2026-10-12' AND (unassigned_at AT TIME ZONE 'America/New_York')::date='2026-10-26')
 OR (truck_id=$3 AND (assigned_at AT TIME ZONE 'America/New_York')::date='2026-10-26' AND unassigned_at IS NULL))`, d.ID, d.TruckID, next.ID).Scan(&count); err != nil || count != 2 {
					t.Fatalf("later transfer changed: %d %v", count, err)
				}
				var roster string
				if err := pool.QueryRow(ctx, `SELECT roster_start_week::text FROM drivers WHERE id=$1`, d.ID).Scan(&roster); err != nil || roster != "2026-10-26" {
					t.Fatalf("reactivation moved: %s %v", roster, err)
				}
			})
			for _, guard := range []string{"driver finalized", "investor finalized", "saved work", "saved payroll", "collected escrow", "truck overlap", "later assignment", "team"} {
				t.Run(guard, func(t *testing.T) {
					d, input := fixture(t)
					switch guard {
					case "driver finalized":
						exec(t, `INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-05','{}')`, d.ID)
					case "investor finalized":
						exec(t, `INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits,finalized,report) SELECT id,'2026-10-05',owner_id,'{}',true,'{}' FROM trucks WHERE id=$1`, d.TruckID)
					case "saved work":
						exec(t, `INSERT INTO gross_board_entries(driver_id,service_date,load_number) VALUES($1,'2026-10-06','SAVED')`, d.ID)
					case "saved payroll":
						exec(t, `INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES($1,'2026-10-05')`, d.ID)
					case "collected escrow":
						exec(t, `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) SELECT id,'2026-10-05',100 FROM driver_escrows WHERE driver_id=$1`, d.ID)
					case "truck overlap":
						other, _ := fixture(t)
						exec(t, `INSERT INTO truck_driver_assignments(driver_id,truck_id,assigned_at,unassigned_at) VALUES($1,$2,'2026-09-28 04:00+00','2026-10-05 04:00+00')`, other.ID, d.TruckID)
					case "later assignment":
						exec(t, `UPDATE truck_driver_assignments SET unassigned_at='2026-10-12 04:00+00' WHERE driver_id=$1`, d.ID)
					case "team":
						other, _ := fixture(t)
						exec(t, `INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,'2026-10-05')`, d.ID, other.ID)
					}
					before := state(t, d.ID)
					input.HireDate = parse("2026-10-12")
					if guard == "truck overlap" {
						input.HireDate = parse("2026-09-30")
					}
					if _, err := fleet.UpdateDriver(ctx, d.ID, input); err == nil {
						t.Fatal("unsafe correction accepted")
					}
					if state(t, d.ID) != before {
						t.Fatal("rejected correction changed dates")
					}
				})
			}
		})
	}
}
