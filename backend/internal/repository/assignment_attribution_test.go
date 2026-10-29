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

func TestAssignmentAttributionDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ASSIGNMENT_HISTORY_TEST_DATABASE_URL to a disposable test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") {
		t.Fatal("a separate _test database is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/166_assignment_attribution.sql")
	if err != nil {
		t.Fatal(err)
	}
	assignmentView, err := os.ReadFile("../../sql/167_clustie_load_assignments.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(init), "\r\n", "\n")
	change := strings.ReplaceAll(string(migration), "\r\n", "\n")
	boundary := strings.LastIndex(source, change)
	if boundary < 0 {
		t.Fatal("fresh schema must contain the exact attribution migration")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("assignment_actor_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, q, args...); e != nil {
					t.Fatal(e)
				}
			}
			scalar := func(q string, args ...any) string {
				t.Helper()
				var v string
				if e := admin.QueryRow(ctx, q, args...).Scan(&v); e != nil {
					t.Fatal(e)
				}
				return v
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public`)
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "fresh" {
				exec(source)
			} else {
				exec(source[:boundary])
			}
			legacyDriver := scalar(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Legacy','legacy','cpm',0.75) RETURNING id::text`)
			legacyTruck := scalar(`INSERT INTO trucks(unit_number) VALUES('LEGACY') RETURNING id::text`)
			exec(`INSERT INTO truck_driver_assignments(driver_id,truck_id,assigned_at,source) VALUES($1,$2,'2026-10-12 00:00-04','Original import note')`, legacyDriver, legacyTruck)
			exec(`INSERT INTO loads(id,load_id,status,total_pay,load_pay,truck_unit) VALUES(9166,'LEGACY-AUTHORITY','delivered',100,100,'Wrong source unit')`)
			exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES($1,'2026-10-19','LEGACY-AUTHORITY',9166)`, legacyDriver)
			if mode == "migration" {
				exec(change)
				exec(string(assignmentView))
			}
			if got := scalar(`SELECT truck_id::text FROM clustie_loads WHERE id=9166`); got != legacyTruck {
				t.Fatal("upgrade ignored existing native assignments", got)
			}
			if got := scalar(`SELECT truck_unit FROM loads WHERE id=9166`); got != "Wrong source unit" {
				t.Fatal("upgrade rewrote source provenance", got)
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
			legacy, err := repo.DriverAssignmentHistory(ctx, legacyDriver)
			if err != nil || len(legacy) != 2 {
				t.Fatalf("legacy history: %+v %v", legacy, err)
			}
			for _, e := range legacy {
				if mode == "migration" && (e.ActorName != "" || e.ActorID != "" || e.RecordedAt != nil) {
					t.Fatalf("invented historical attribution: %+v", e)
				}
			}
			alice := scalar(`INSERT INTO app_users(username,password_hash,email,role_id) SELECT 'Alice','$2test','alice@example.test',id FROM app_roles WHERE system_role RETURNING id::text`)
			bob := scalar(`INSERT INTO app_users(username,password_hash,email,role_id) SELECT 'Bob','$2test','bob@example.test',id FROM app_roles WHERE system_role RETURNING id::text`)
			dispatcher := scalar(`INSERT INTO dispatchers(full_name,normalized_name) VALUES('Dispatcher','dispatcher') RETURNING id::text`)
			driver, err := repo.CreateDriver(ctx, DriverInput{FullName: "Attributed Driver", PayType: "cpm", PayRate: 0.75, Active: true, ChargeActor: alice, DispatcherID: &dispatcher, AssignmentWeek: "2026-10-20"})
			if err != nil {
				t.Fatal(err)
			}
			first, err := repo.CreateTruck(ctx, TruckInput{UnitNumber: "ACTOR-1", Active: true, Status: "available", DriverID: &driver.ID, StatusActor: alice, AssignmentWeek: "2026-10-20"})
			if err != nil {
				t.Fatal(err)
			}
			second, err := repo.CreateTruck(ctx, TruckInput{UnitNumber: "ACTOR-2", Active: true, Status: "available", DriverID: &driver.ID, StatusActor: bob, AssignmentWeek: "2026-10-24"})
			if err != nil {
				t.Fatal(err)
			}
			history := func() []AssignmentHistoryEntry {
				t.Helper()
				v, e := repo.DriverAssignmentHistory(ctx, driver.ID)
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			byName := func(name string) AssignmentHistoryEntry {
				t.Helper()
				for _, e := range history() {
					if e.Name == name {
						return e
					}
				}
				t.Fatalf("missing %s", name)
				return AssignmentHistoryEntry{}
			}
			assertActor := func(e AssignmentHistoryEntry, id, name string) {
				t.Helper()
				if e.ActorID != id || e.ActorName != name || e.RecordedAt == nil {
					t.Fatalf("missing actor snapshot: %+v", e)
				}
			}
			assertActor(byName("Dispatcher"), alice, "Alice")
			assertActor(byName("ACTOR-1"), alice, "Alice")
			assertActor(byName("ACTOR-2"), bob, "Bob")
			old := byName("ACTOR-1")
			if old.EndedByID != bob || old.EndedByName != "Bob" || old.EndedRecordedAt == nil || old.UnassignedAt == nil {
				t.Fatalf("missing release actor: %+v", old)
			}
			if !old.RecordedAt.After(old.AssignedAt) {
				t.Fatal("backdated effective date replaced recorded time")
			}
			truckHistory, err := repo.TruckAssignmentHistory(ctx, first.ID)
			if err != nil || len(truckHistory) != 1 || truckHistory[0].ActorID != alice || truckHistory[0].EndedByID != bob {
				t.Fatalf("truck history: %+v %v", truckHistory, err)
			}
			// No-op saves and reads must retain attribution, not claim the latest viewer.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = setBoardActor(ctx, tx, alice, "profile"); err != nil {
				t.Fatal(err)
			}
			if err = assignTruck(ctx, tx, second.ID, driver.ID); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			assertActor(byName("ACTOR-2"), bob, "Bob")
			if len(history()) != 3 {
				t.Fatal("no-op added history")
			}
			// Failed transactions never leak an end attribution or replacement period.
			tx, err = pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = setBoardActor(ctx, tx, alice, "profile"); err != nil {
				t.Fatal(err)
			}
			if err = assignTruck(ctx, tx, first.ID, driver.ID); err != nil {
				t.Fatal(err)
			}
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if v := byName("ACTOR-2"); v.UnassignedAt != nil || v.EndedByName != "" || len(history()) != 3 {
				t.Fatalf("rollback leaked: %+v", v)
			}
			// A dispatcher-only form edit has its own actor, independently of account edits.
			_, err = repo.UpdateDispatcher(ctx, dispatcher, DispatcherInput{FullName: "Dispatcher", Active: true, AssignmentActor: bob, DriverIDs: []string{}, AssignmentWeek: "2026-10-24"})
			if err != nil {
				t.Fatal(err)
			}
			assertActor(byName("Unassigned"), bob, "Bob")
			if v := byName("Dispatcher"); v.EndedByID != bob {
				t.Fatalf("dispatcher release: %+v", v)
			}
			_, err = repo.UpdateDispatcher(ctx, dispatcher, DispatcherInput{FullName: "Dispatcher", Active: true, AssignmentActor: alice, DriverIDs: []string{driver.ID}, AssignmentWeek: "2026-10-25"})
			if err != nil {
				t.Fatal(err)
			}
			if err = repo.DeleteDispatcher(ctx, dispatcher, bob); err != nil {
				t.Fatal(err)
			}
			for _, v := range history() {
				if v.Kind == "dispatcher" && v.UnassignedAt == nil {
					assertActor(v, bob, "Bob")
				}
			}
			exec(`UPDATE app_users SET username='Renamed' WHERE id=$1`, alice)
			exec(`DELETE FROM app_users WHERE id=$1`, bob)
			assertActor(byName("ACTOR-1"), alice, "Alice")
			assertActor(byName("ACTOR-2"), bob, "Bob")
			// Unattributed automation is explicitly System, with no actor leaked by the pool.
			if _, err = pool.Exec(ctx, `UPDATE truck_driver_assignments SET unassigned_at=now() WHERE truck_id=$1 AND unassigned_at IS NULL`, second.ID); err != nil {
				t.Fatal(err)
			}
			if v := byName("ACTOR-2"); v.EndedByName != "System" || v.EndedByID != "" {
				t.Fatalf("system release: %+v", v)
			}
		})
	}
}
