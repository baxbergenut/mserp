package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFleetStatusDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable local MSERP_DRIVER_BOARD_TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/137_fleet_status_history.sql")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, ok := strings.Cut(strings.ReplaceAll(string(init), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n"))
	if !ok {
		t.Fatal("fresh schema must contain exact migration")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("fleet_status_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			execAdmin := func(sql string) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
			}
			execAdmin(`CREATE SCHEMA ` + quoted + `;SET search_path TO ` + quoted + `,public;GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public;DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "fresh" {
				execAdmin(string(init))
			} else {
				execAdmin(prefix)
				execAdmin(string(migration))
				applyLaterTestMigrations(t, ctx, admin, "137")
			}
			for _, name := range []string{"fleet_status_control", "fleet_status_rules", "fleet_status_history"} {
				var owner string
				if err = admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename=$2`, schema, name).Scan(&owner); err != nil || owner != "mserp_app" {
					t.Fatal("runtime owner", name, owner, err)
				}
			}
			execAdmin(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := pool.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			scalar := func(sql string, args ...any) string {
				t.Helper()
				var v string
				if e := pool.QueryRow(ctx, sql, args...).Scan(&v); e != nil {
					t.Fatal(e)
				}
				return v
			}
			fleet := NewFleetRepository(pool)
			board := NewDriverBoardRepository(pool)
			actor := scalar(`INSERT INTO app_users(username,password_hash,email,role_id) VALUES('Status Reviewer','$2a$test','status-review@example.test',(SELECT id FROM app_roles WHERE system_role)) RETURNING id::text`)
			d, e := fleet.CreateDriver(ctx, DriverInput{FullName: "Status Test Driver", PayType: "cpm", PayRate: 0.6, Active: true})
			if e != nil {
				t.Fatal(e)
			}
			truck, e := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "STATUS-1", Status: "assigned", Active: true, IsCompanyOwned: true, DriverID: &d.ID})
			if e != nil {
				t.Fatal(e)
			}
			exec(`INSERT INTO driver_board(driver_id,status,notes) VALUES($1,'HOME','Preserved notes')`, d.ID)
			preview, e := fleet.PreviewStatuses(ctx)
			if e != nil {
				t.Fatal(e)
			}
			holdBeforeActivation := scalar(`INSERT INTO maintenance_records(truck_id,dispatch_status,notes) VALUES($1,'shop','Repair required') RETURNING id::text`, truck.ID)
			if e = fleet.ActivateStatuses(ctx, preview.Revision); e == nil {
				t.Fatal("work-order change escaped activation fence")
			}
			exec(`UPDATE maintenance_records SET dispatch_status='' WHERE id=$1`, holdBeforeActivation)
			preview, e = fleet.PreviewStatuses(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = fleet.ActivateStatuses(ctx, "stale"); e == nil {
				t.Fatal("stale activation accepted")
			}
			if e = fleet.ActivateStatuses(ctx, preview.Revision); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT availability_status FROM drivers WHERE id=$1`, d.ID); got != "home" {
				t.Fatal(got)
			}
			save := func(status string) error {
				entry, e := boardEntryForTest(ctx, pool, d.ID)
				if e != nil {
					return e
				}
				entry.Status = status
				entry.StatusReason = "Reported equipment issue"
				entry.StatusEdited = true
				_, e = board.Save(ctx, []DriverBoardEntry{entry}, actor)
				return e
			}
			if e = save("DISPATCHED"); e == nil {
				t.Fatal("dispatched an unavailable driver")
			}
			if e = save("READY"); e != nil {
				t.Fatal(e)
			}
			if e = save("DISPATCHED"); e != nil {
				t.Fatal(e)
			}
			for _, status := range []string{"COVERED", "ENROUTE", "RESERVED", "DEADHEAD", "Issue with PU/Del", "In the YARD", "Hold for Load"} {
				if e = save(status); e != nil {
					t.Fatalf("%s: %v", status, e)
				}
			}
			for label, availability := range map[string]string{"HOME": "home", "VACATION": "vacation", "REST": "rest", "Family issue": "family_issue", "Health issue": "health_issue", "NEW DRIVER": "new_driver"} {
				if e = save(label); e != nil {
					t.Fatalf("%s: %v", label, e)
				}
				if got := scalar(`SELECT availability_status FROM drivers WHERE id=$1`, d.ID); got != availability {
					t.Fatal(label, got)
				}
				if got := scalar(`SELECT fleet_truck_operating_status($1)`, truck.ID); got != "in_use" {
					t.Fatal("personal reason affected truck", got)
				}
			}
			if e = save("NEW DRIVER"); e != nil {
				t.Fatal(e)
			}
			if e = save("SHOP"); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT fleet_truck_operating_status($1)`, truck.ID); got != "shop" {
				t.Fatal(got)
			}
			if got := scalar(`SELECT status FROM driver_board WHERE driver_id=$1`, d.ID); got != "NEW DRIVER" {
				t.Fatal("lost personal reason", got)
			}
			if e = save("READY"); e == nil {
				t.Fatal("READY released a shop restriction")
			}
			fresh, e := fleet.GetTruck(ctx, truck.ID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = fleet.ChangeTruckCondition(ctx, truck.ID, "", FleetStatusChange{Status: "ready", Version: fresh.StatusVersion, Reason: "Repairs checked; return to service"}); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT status FROM driver_board WHERE driver_id=$1`, d.ID); got != "NEW DRIVER" {
				t.Fatal(got)
			}
			if e = save("READY"); e != nil {
				t.Fatal(e)
			}
			for _, label := range []string{"STOP", "ACCIDENT"} {
				if e = save(label); e != nil {
					t.Fatal(label, e)
				}
				fresh, _ = fleet.GetTruck(ctx, truck.ID)
				if _, e = fleet.ChangeTruckCondition(ctx, truck.ID, "", FleetStatusChange{Status: "ready", Version: fresh.StatusVersion, Reason: "Repairs checked; return to service"}); e != nil {
					t.Fatal(e)
				}
				if got := scalar(`SELECT status FROM driver_board WHERE driver_id=$1`, d.ID); got != "READY" {
					t.Fatal("release did not return to READY", got)
				}
			}
			if e = save("Left"); e == nil {
				t.Fatal("board silently terminated driver")
			}
			if got := scalar(`SELECT notes FROM driver_board WHERE driver_id=$1`, d.ID); got != "Preserved notes" {
				t.Fatal(got)
			}
			freshDriver, _ := fleet.GetDriver(ctx, d.ID)
			oldVersion := freshDriver.StatusVersion
			if _, e = fleet.ChangeAvailability(ctx, d.ID, "", FleetStatusChange{Status: "family_issue", Version: oldVersion}); e != nil {
				t.Fatal(e)
			}
			if _, e = fleet.ChangeAvailability(ctx, d.ID, "", FleetStatusChange{Status: "active", Version: oldVersion}); e == nil {
				t.Fatal("stale availability overwrite")
			}
			countBefore := scalar(`SELECT count(*)::text FROM fleet_status_history`)
			freshDriver, _ = fleet.GetDriver(ctx, d.ID)
			if _, e = fleet.ChangeAvailability(ctx, d.ID, "", FleetStatusChange{Status: "family_issue", Version: freshDriver.StatusVersion}); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT count(*)::text FROM fleet_status_history`); got != countBefore {
				t.Fatal("no-op history", got, countBefore)
			}
			if _, e = fleet.ChangeAvailability(ctx, d.ID, "", FleetStatusChange{Status: "active", Version: freshDriver.StatusVersion, EffectiveAt: time.Now().Add(-time.Hour)}); e == nil {
				t.Fatal("backdating before previous transition accepted")
			}
			history, e := fleet.StatusHistory(ctx, "driver", d.ID, "", "", "", "", 1, 0)
			if e != nil {
				t.Fatal(e)
			}
			if len(history.Items) == 0 {
				t.Fatal("missing history")
			}
			for _, event := range history.Items {
				if event.Track == "dispatch" {
					t.Fatal("profile history includes operational dispatch status", event.Status)
				}
				if event.EntityKind != "driver" || event.EntityID != d.ID || event.Track == "assigned_truck" {
					t.Fatal("driver history includes another entity's status", event)
				}
			}
			truckHistory, e := fleet.StatusHistory(ctx, "truck", truck.ID, "condition", "shop", "", "", 1, history.Snapshot)
			if e != nil || len(truckHistory.Items) == 0 {
				t.Fatal("truck condition history missing from truck profile", truckHistory, e)
			}
			assignedHistory, e := fleet.StatusHistory(ctx, "driver", d.ID, "assigned_truck", "", "", "", 1, history.Snapshot)
			if e != nil || len(assignedHistory.Items) != 0 || assignedHistory.HasMore {
				t.Fatal("driver history exposes truck statuses through filters", assignedHistory, e)
			}
			if got := scalar(`SELECT count(*)::text FROM fleet_status_history WHERE entity_id=$1 AND track='dispatch'`, d.ID); got == "0" {
				t.Fatal("operational dispatch audit was lost")
			}
			dispatchHistory, e := fleet.StatusHistory(ctx, "driver", d.ID, "dispatch", "", "", "", 1, history.Snapshot)
			if e != nil || len(dispatchHistory.Items) != 0 || dispatchHistory.HasMore {
				t.Fatal("profile history exposes dispatch through filters", dispatchHistory, e)
			}
			if got := scalar(`SELECT actor_name FROM fleet_status_history WHERE source='board' ORDER BY id DESC LIMIT 1`); got != "Status Reviewer" {
				t.Fatal("lost actor", got)
			}
			if e = save("READY"); e != nil {
				t.Fatal(e)
			}
			one := scalar(`INSERT INTO maintenance_records(truck_id,dispatch_status,notes) VALUES($1,'shop','Repair required') RETURNING id::text`, truck.ID)
			two := scalar(`INSERT INTO maintenance_records(truck_id,dispatch_status,notes) VALUES($1,'out_of_service','Engine replacement') RETURNING id::text`, truck.ID)
			exec(`UPDATE maintenance_records SET dispatch_status='' WHERE id=$1`, one)
			if got := scalar(`SELECT status FROM driver_board WHERE driver_id=$1`, d.ID); got != "STOP" {
				t.Fatal("one release cleared another hold", got)
			}
			fresh, _ = fleet.GetTruck(ctx, truck.ID)
			if _, e = fleet.ChangeTruckCondition(ctx, truck.ID, actor, FleetStatusChange{Status: "ready", Version: fresh.StatusVersion, Reason: "Repairs checked; return to service"}); e == nil {
				t.Fatal("manual release bypassed work order")
			}
			exec(`UPDATE maintenance_records SET dispatch_status='' WHERE id=$1`, two)
			if got := scalar(`SELECT status FROM driver_board WHERE driver_id=$1`, d.ID); got != "READY" {
				t.Fatal(got)
			}
			partner, e := fleet.CreateDriver(ctx, DriverInput{FullName: "Team Status Driver", PayType: "cpm", PayRate: 0.6, Active: true})
			if e != nil {
				t.Fatal(e)
			}
			exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date)`, d.ID, partner.ID)
			if e = save("SHOP"); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT fleet_truck_operating_status(fleet_status_driver_truck($1))`, partner.ID); got != "shop" {
				t.Fatal("team truck restriction missing", got)
			}
			partner, _ = fleet.GetDriver(ctx, partner.ID)
			if _, e = fleet.ChangeAvailability(ctx, partner.ID, actor, FleetStatusChange{Status: "family_issue", Version: partner.StatusVersion}); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT availability_status FROM drivers WHERE id=$1`, d.ID); got != "active" {
				t.Fatal("partner's personal status affected main driver", got)
			}
			fresh, _ = fleet.GetTruck(ctx, truck.ID)
			if _, e = fleet.ChangeTruckCondition(ctx, truck.ID, actor, FleetStatusChange{Status: "ready", Version: fresh.StatusVersion, Reason: "Repairs checked; return to service"}); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT availability_status FROM drivers WHERE id=$1`, partner.ID); got != "family_issue" {
				t.Fatal(got)
			}
			// Backdate the newest transition between the previous transition and now.
			freshDriver, _ = fleet.GetDriver(ctx, d.ID)
			effective := time.Now()
			time.Sleep(5 * time.Millisecond)
			if _, e = fleet.ChangeAvailability(ctx, d.ID, actor, FleetStatusChange{Status: "rest", Version: freshDriver.StatusVersion, EffectiveAt: effective}); e != nil {
				t.Fatal("valid backdate rejected", e)
			}
			var savedEffective time.Time
			if e = pool.QueryRow(ctx, `SELECT effective_at FROM fleet_status_history WHERE entity_id=$1 AND track='availability' ORDER BY id DESC LIMIT 1`, d.ID).Scan(&savedEffective); e != nil || savedEffective.Sub(effective).Abs() > time.Microsecond {
				t.Fatal(savedEffective, effective, e)
			}
			beforeReads := scalar(`SELECT count(*)::text FROM fleet_status_history`)
			if _, e = board.Get(ctx, time.Now()); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT count(*)::text FROM fleet_status_history`); got != beforeReads {
				t.Fatal("read wrote history")
			}
			if e = save("READY"); e != nil {
				t.Fatal(e)
			}
			if e = save("ENROUTE"); e != nil {
				t.Fatal(e)
			}
			gross := NewGrossBoardRepository(pool)
			plans, e := gross.SaveEntries(ctx, []GrossBoardEntry{{DriverID: d.ID, Date: boardLoadToday(time.Now()), LoadNumber: "STATUS-NEXT", Slot: 0}}, actor)
			if e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT status FROM driver_board WHERE driver_id=$1`, d.ID); got != "RESERVED" {
				t.Fatal("queue did not persist reserved", got)
			}
			plans[0].LoadNumber = ""
			plans[0].Deleted = true
			if _, e = gross.SaveEntries(ctx, plans, actor); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT status FROM driver_board WHERE driver_id=$1`, d.ID); got != "ENROUTE" {
				t.Fatal("queue did not persist enroute", got)
			}
			if _, e = pool.Exec(ctx, `DELETE FROM fleet_status_history`); e == nil {
				t.Fatal("mutable history")
			}
			// Intermediate projections within one action never create fake periods.
			if e = save("READY"); e != nil {
				t.Fatal(e)
			}
			beforeAction := scalar(`SELECT count(*)::text FROM fleet_status_history`)
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = setBoardActor(ctx, tx, actor, "profile"); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, `UPDATE drivers SET availability_status='rest' WHERE id=$1`, d.ID); e != nil {
				tx.Rollback(ctx)
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, `UPDATE drivers SET availability_status='active' WHERE id=$1`, d.ID); e != nil {
				tx.Rollback(ctx)
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			if got := scalar(`SELECT count(*)::text FROM fleet_status_history`); got != beforeAction {
				t.Fatal("intermediate states leaked into committed history", got, beforeAction)
			}
			testDriverLeaveWorkflow(t, ctx, pool, actor)
		})
	}
}
func boardEntryForTest(ctx context.Context, pool *pgxpool.Pool, id string) (DriverBoardEntry, error) {
	tx, e := pool.Begin(ctx)
	if e != nil {
		return DriverBoardEntry{}, e
	}
	defer tx.Rollback(ctx)
	return boardEntry(ctx, tx, id)
}
