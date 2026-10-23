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
	"mserp/internal/fleetscope"
)

func TestTerminationOffboardingDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_FLEETSCOPE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable MSERP_FLEETSCOPE_TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/148_termination_truck_actions.sql")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, found := strings.Cut(strings.ReplaceAll(string(init), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n"))
	if !found {
		t.Fatal("fresh schema missing exact migration")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("termination_trucks_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			execAdmin := func(sql string) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			execAdmin(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "fresh" {
				execAdmin(string(init))
			} else {
				execAdmin(prefix)
				execAdmin(string(migration))
			}
			execAdmin(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, app)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			verifyTerminationOffboarding(t, ctx, pool, execAdmin)
		})
	}
}

func verifyTerminationOffboarding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, execAdmin func(string)) {
	fleet := NewFleetRepository(pool)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, err := pool.Exec(ctx, sql, args...); must(err) }
	scalar := func(sql string, args ...any) string {
		t.Helper()
		var s string
		must(pool.QueryRow(ctx, sql, args...).Scan(&s))
		return s
	}
	actor := scalar(`INSERT INTO app_users(username,password_hash,email,role_id) VALUES('Termination Reviewer','$2a$test','termination@example.test',(SELECT id FROM app_roles WHERE system_role)) RETURNING id::text`)
	exec(`UPDATE fleet_status_control SET enabled=true`)
	dispatcher, err := fleet.CreateDispatcher(ctx, DispatcherInput{FullName: "Offboarding Dispatcher", Active: true})
	must(err)
	newDriver := func(name string) (Driver, DriverInput) {
		t.Helper()
		input := DriverInput{FullName: name, Active: true, PayType: "cpm", PayRate: .75, DispatcherID: &dispatcher.ID, EscrowAmount: "0"}
		d, err := fleet.CreateDriver(ctx, input)
		must(err)
		return d, input
	}
	newTruck := func(unit string, owner, driver *string) Truck {
		t.Helper()
		v, err := fleet.CreateTruck(ctx, TruckInput{UnitNumber: unit, Active: true, Status: "available", OwnerDriverID: owner, DriverID: driver})
		must(err)
		return v
	}
	today := time.Now().Format(time.DateOnly)
	terminate := func(d Driver) (Driver, error) {
		return fleet.ChangeDriverStatus(ctx, d.ID, actor, DriverStatusChange{Status: "terminated", TerminationDate: today, UpdatedAt: d.UpdatedAt, ChargePauseWeek: ChargeCurrentWeek()})
	}

	for _, route := range []string{"profile", "status", "fleetscope"} {
		t.Run(route, func(t *testing.T) {
			d, input := newDriver("Owner " + route)
			truck := newTruck("OWNER-"+route, &d.ID, &d.ID)
			d, err = fleet.GetDriver(ctx, d.ID)
			must(err)
			assignment := scalar(`SELECT id::text FROM truck_driver_assignments WHERE driver_id=$1 AND unassigned_at IS NULL`, d.ID)
			input.TruckID = nil // old clients clear their draft truck when terminating
			switch route {
			case "profile":
				input.Status = "terminated"
				input.ChargeActor = actor
				input.StatusVersion = &d.StatusVersion
				d, err = fleet.UpdateDriver(ctx, d.ID, input)
				must(err)
			case "status":
				d, err = terminate(d)
				must(err)
			case "fleetscope":
				company := scalar(`SELECT gen_random_uuid()::text`)
				external := scalar(`SELECT gen_random_uuid()::text`)
				event := scalar(`SELECT gen_random_uuid()::text`)
				exec(`INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source) VALUES($1,$2,$3,'review')`, company, external, d.ID)
				_, err = fleet.AcceptFleetScopeTermination(ctx, fleetscope.Event{Version: 1, Type: "driver.terminated", EventID: event, CompanyID: company, OccurredAt: time.Now(), TerminationDate: today, Driver: fleetscope.Driver{ID: external, FullName: d.FullName}}, "owner-termination")
				must(err)
				d, err = fleet.GetDriver(ctx, d.ID)
				must(err)
			}
			truck, err = fleet.GetTruck(ctx, truck.ID)
			must(err)
			if d.Active || d.DispatcherID != nil || d.TruckID == nil || *d.TruckID != truck.ID || truck.Active || truck.DriverID == nil || *truck.DriverID != d.ID {
				t.Fatalf("owner termination: driver=%+v truck=%+v", d, truck)
			}
			if got := scalar(`SELECT id::text FROM truck_driver_assignments WHERE driver_id=$1 AND unassigned_at IS NULL`, d.ID); got != assignment {
				t.Fatal("retained link was recreated")
			}
			if got := scalar(`SELECT outcome FROM driver_termination_truck_actions WHERE truck_id=$1`, truck.ID); got != "inactivated" {
				t.Fatal(got)
			}
			if _, err = NewGrossBoardRepository(pool).SaveEntries(ctx, []GrossBoardEntry{{DriverID: d.ID, Date: boardLoadToday(time.Now()), LoadNumber: "TERMINATED-BLOCKED", Slot: 0}}); err == nil {
				t.Fatal("retained terminated owner accepted a new load")
			}
			// Neither editor may silently discard the retained link on a later save.
			input.Status = "terminated"
			input.Active = false
			input.Notes = ptrForTerminationTest("Contact updated")
			input.ChargeActor = ""
			d, err = fleet.UpdateDriver(ctx, d.ID, input)
			must(err)
			truck, err = fleet.UpdateTruck(ctx, truck.ID, TruckInput{UnitNumber: truck.UnitNumber, Active: false, Status: truck.Status})
			must(err)
			if truck.DriverID == nil || *truck.DriverID != d.ID {
				t.Fatal("truck edit removed terminated owner")
			}
			if _, err = fleet.CreateTruck(ctx, TruckInput{UnitNumber: "FORBIDDEN-" + route, Active: true, Status: "available", DriverID: &d.ID}); err == nil {
				t.Fatal("new assignment to terminated driver accepted")
			}
			if got := scalar(`SELECT count(*)::text FROM driver_termination_truck_actions WHERE truck_id=$1`, truck.ID); got != "1" {
				t.Fatal("duplicate receipt", got)
			}
			// Rehire does not reactivate equipment or restore a dispatcher.
			d, err = fleet.ChangeDriverStatus(ctx, d.ID, actor, DriverStatusChange{Status: "active", UpdatedAt: d.UpdatedAt})
			must(err)
			truck, err = fleet.GetTruck(ctx, truck.ID)
			must(err)
			if !d.Active || d.DispatcherID != nil || truck.Active {
				t.Fatal("rehire changed truck/dispatcher")
			}
			if _, err = NewGrossBoardRepository(pool).SaveEntries(ctx, []GrossBoardEntry{{DriverID: d.ID, Date: boardLoadToday(time.Now()), LoadNumber: "INACTIVE-TRUCK-BLOCKED", Slot: 0}}); !errors.Is(err, ErrTruckUnavailable) {
				t.Fatal("rehire bypassed inactive truck", err)
			}
			d, err = terminate(d)
			must(err)
			if got := scalar(`SELECT outcome FROM driver_termination_truck_actions WHERE truck_id=$1 AND termination_id=(SELECT termination_id FROM drivers WHERE id=$2)`, truck.ID, d.ID); got != "already_inactive" {
				t.Fatal("new termination did not get its own outcome", got)
			}
		})
	}

	t.Run("company truck", func(t *testing.T) {
		d, _ := newDriver("Company Driver")
		truck := newTruck("COMPANY", nil, &d.ID)
		d, err = fleet.GetDriver(ctx, d.ID)
		must(err)
		d, err = terminate(d)
		must(err)
		truck, err = fleet.GetTruck(ctx, truck.ID)
		must(err)
		if d.TruckID != nil || d.DispatcherID != nil || truck.DriverID != nil || !truck.Active {
			t.Fatal("company truck not released safely")
		}
	})

	t.Run("independent investor truck", func(t *testing.T) {
		d, _ := newDriver("Investor Hired Driver")
		owner := scalar(`INSERT INTO investors(full_name) VALUES('Independent Owner') RETURNING id::text`)
		truck, createErr := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "INVESTOR-HIRED", Active: true, Status: "available", OwnerID: &owner, DriverID: &d.ID})
		must(createErr)
		d, err = fleet.GetDriver(ctx, d.ID)
		must(err)
		d, err = terminate(d)
		must(err)
		truck, err = fleet.GetTruck(ctx, truck.ID)
		must(err)
		if d.TruckID != nil || d.DispatcherID != nil || truck.DriverID != nil || !truck.Active || truck.OwnerID != owner {
			t.Fatal("investor truck not released with ownership preserved")
		}
	})

	for _, continuing := range []bool{true, false} {
		t.Run(fmt.Sprintf("investor with hired driver active=%v", continuing), func(t *testing.T) {
			owner, _ := newDriver(fmt.Sprintf("Shared Owner %v", continuing))
			operator, _ := newDriver(fmt.Sprintf("Active Operator %v", continuing))
			truck := newTruck(fmt.Sprintf("SHARED-DIRECT-%v", continuing), &owner.ID, &operator.ID)
			if !continuing {
				exec(`UPDATE investors SET active=false WHERE driver_id=$1`, owner.ID)
			}
			owner, err = fleet.GetDriver(ctx, owner.ID)
			must(err)
			_, err = terminate(owner)
			must(err)
			truck, err = fleet.GetTruck(ctx, truck.ID)
			must(err)
			if !truck.Active || truck.DriverID == nil || *truck.DriverID != operator.ID {
				t.Fatal("active driver truck changed")
			}
			want := "0"
			if !continuing {
				want = "1"
			}
			if got := scalar(`SELECT count(*)::text FROM driver_termination_truck_actions WHERE truck_id=$1`, truck.ID); got != want {
				t.Fatal("incorrect investor offboarding receipt", got)
			}
			if !continuing && scalar(`SELECT outcome FROM driver_termination_truck_actions WHERE truck_id=$1`, truck.ID) != "held" {
				t.Fatal("inactive investor conflict not held")
			}
			if _, err = NewGrossBoardRepository(pool).SaveEntries(ctx, []GrossBoardEntry{{DriverID: owner.ID, Date: boardLoadToday(time.Now()), LoadNumber: "HELD-OWNER-BLOCKED", Slot: 0}}); err == nil {
				t.Fatal("terminated owner dispatched on an active held truck")
			}
			page, err := NewCustomTaskRepository(pool).List(ctx, Pagination{}, truck.UnitNumber, "open")
			must(err)
			if (continuing && page.Total != 0) || (!continuing && page.Total != 1) {
				t.Fatal("incorrect investor review task count", page.Total)
			}
			hidden, err := NewCustomTaskRepository(pool).List(WithTaskViewer(ctx, operator.ID, false), Pagination{}, truck.UnitNumber, "open")
			must(err)
			if hidden.Total != 0 {
				t.Fatal("review task leaked")
			}
		})
	}

	t.Run("active team driver", func(t *testing.T) {
		owner, _ := newDriver("Team Owner")
		teammate, _ := newDriver("Active Teammate")
		truck := newTruck("SHARED-TEAM", &owner.ID, &owner.ID)
		exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,$3::date)`, owner.ID, teammate.ID, ChargeCurrentWeek())
		owner, err = fleet.GetDriver(ctx, owner.ID)
		must(err)
		_, err = terminate(owner)
		must(err)
		truck, err = fleet.GetTruck(ctx, truck.ID)
		must(err)
		if !truck.Active || truck.DriverID == nil || *truck.DriverID != owner.ID {
			t.Fatal("team truck changed")
		}
		if got := scalar(`SELECT outcome FROM driver_termination_truck_actions WHERE truck_id=$1`, truck.ID); got != "held" {
			t.Fatal(got)
		}
	})

	t.Run("terminated teammate loses inherited current assignments", func(t *testing.T) {
		main, _ := newDriver("Working Main")
		teammate, _ := newDriver("Departing Teammate")
		truck := newTruck("TEAM-COMPANY", nil, &main.ID)
		exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,$3::date)`, main.ID, teammate.ID, ChargeCurrentWeek())
		teammate, err = fleet.GetDriver(ctx, teammate.ID)
		must(err)
		if teammate.DispatcherID == nil || teammate.TeamTruckID == nil {
			t.Fatal("missing inherited setup")
		}
		teammate, err = terminate(teammate)
		must(err)
		if teammate.DispatcherID != nil || teammate.DispatcherName != nil || teammate.TruckID != nil || teammate.TeamTruckID != nil || teammate.TeamTruckUnit != nil || teammate.TruckOperatingStatus != "" {
			t.Fatal("terminated teammate retained current assignments", teammate)
		}
		truck, err = fleet.GetTruck(ctx, truck.ID)
		must(err)
		if !truck.Active || truck.DriverID == nil || *truck.DriverID != main.ID {
			t.Fatal("teammate termination changed main driver truck")
		}
		if scalar(`SELECT count(*)::text FROM driver_teams WHERE main_driver_id=$1 AND team_driver_id=$2 AND end_week IS NULL`, main.ID, teammate.ID) != "1" {
			t.Fatal("termination rewrote weekly team history")
		}
	})

	t.Run("review failure rolls back termination", func(t *testing.T) {
		owner, _ := newDriver("Rollback Owner")
		operator, _ := newDriver("Rollback Operator")
		truck := newTruck("ROLLBACK-REVIEW", &owner.ID, &operator.ID)
		exec(`UPDATE investors SET active=false WHERE driver_id=$1`, owner.ID)
		execAdmin(`ALTER TABLE custom_tasks ADD CONSTRAINT reject_offboarding_review CHECK(title <> 'Review terminated owner''s truck ROLLBACK-REVIEW')`)
		owner, err = fleet.GetDriver(ctx, owner.ID)
		must(err)
		if _, err = terminate(owner); err == nil {
			t.Fatal("review failure accepted")
		}
		execAdmin(`ALTER TABLE custom_tasks DROP CONSTRAINT reject_offboarding_review`)
		owner, err = fleet.GetDriver(ctx, owner.ID)
		must(err)
		truck, err = fleet.GetTruck(ctx, truck.ID)
		must(err)
		if !owner.Active || owner.DispatcherID == nil || !truck.Active {
			t.Fatal("partial termination after review failure")
		}
	})

	t.Run("existing records cleanup", func(t *testing.T) {
		owner, _ := newDriver("Legacy Owner")
		truck := newTruck("LEGACY-OWNER", &owner.ID, &owner.ID)
		companyDriver, _ := newDriver("Legacy Company")
		companyTruck := newTruck("LEGACY-COMPANY", nil, &companyDriver.ID)
		sharedOwner, _ := newDriver("Legacy Shared Owner")
		active, _ := newDriver("Legacy Active")
		shared := newTruck("LEGACY-SHARED", &sharedOwner.ID, &active.ID)
		unassignedOwner, _ := newDriver("Legacy Unassigned Owner")
		unassigned := newTruck("LEGACY-UNASSIGNED", &unassignedOwner.ID, nil)
		exec(`UPDATE drivers SET status='terminated' WHERE id=ANY($1::uuid[])`, []string{owner.ID, companyDriver.ID, sharedOwner.ID, unassignedOwner.ID})
		preview, err := fleet.PreviewTerminationCleanup(ctx)
		must(err)
		exec(`UPDATE trucks SET notes='Snapshot changed',updated_at=now() WHERE id=$1`, truck.ID)
		if _, err = fleet.ApplyTerminationCleanup(ctx, preview.Revision); !errors.Is(err, ErrDriverBoardConflict) {
			t.Fatal("stale cleanup accepted", err)
		}
		preview, err = fleet.PreviewTerminationCleanup(ctx)
		must(err)
		_, err = fleet.ApplyTerminationCleanup(ctx, preview.Revision)
		must(err)
		truck, err = fleet.GetTruck(ctx, truck.ID)
		must(err)
		companyTruck, err = fleet.GetTruck(ctx, companyTruck.ID)
		must(err)
		shared, err = fleet.GetTruck(ctx, shared.ID)
		must(err)
		if truck.Active || truck.DriverID == nil || *truck.DriverID != owner.ID || !companyTruck.Active || companyTruck.DriverID != nil || !shared.Active || shared.DriverID == nil || *shared.DriverID != active.ID {
			t.Fatal("incorrect bulk cleanup")
		}
		if scalar(`SELECT count(*)::text FROM drivers WHERE NOT active AND dispatcher_id IS NOT NULL`) != "0" {
			t.Fatal("terminated dispatcher retained")
		}
		if scalar(`SELECT count(*)::text FROM truck_driver_assignments WHERE truck_id=$1`, unassigned.ID) != "0" {
			t.Fatal("cleanup invented a driving assignment")
		}
		unassigned, err = fleet.GetTruck(ctx, unassigned.ID)
		must(err)
		if !unassigned.Active || scalar(`SELECT count(*)::text FROM driver_termination_truck_actions WHERE truck_id=$1`, unassigned.ID) != "0" {
			t.Fatal("unassigned active-investor truck entered driver offboarding")
		}
		before := scalar(`SELECT count(*)::text FROM driver_termination_truck_actions`)
		// Continuing investor trucks stay independent when the hired driver leaves.
		exec(`UPDATE drivers SET status='terminated' WHERE id=$1`, active.ID)
		preview, err = fleet.PreviewTerminationCleanup(ctx)
		must(err)
		_, err = fleet.ApplyTerminationCleanup(ctx, preview.Revision)
		must(err)
		if scalar(`SELECT count(*)::text FROM driver_termination_truck_actions`) != before {
			t.Fatal("cleanup retry duplicated actions")
		}
		shared, err = fleet.GetTruck(ctx, shared.ID)
		must(err)
		if !shared.Active {
			t.Fatal("retry silently inactivated continuing investor truck")
		}
	})
}

func ptrForTerminationTest(s string) *string { return &s }
