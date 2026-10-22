package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
)

func TestDriverSetupDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_RELAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_RELAY_TEST_DATABASE_URL to a disposable local _test database")
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
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("driver_setup_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			if mode == "migration" {
				before, _, ok := strings.Cut(string(source), "-- Driver address consolidation (migration 080).")
				if !ok {
					t.Fatal("migration boundary")
				}
				exec(before)
				exec(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,driver_home,city,state) VALUES
    ('Comma','comma','cpm',0,'Louisville, KY',NULL,NULL),
    ('Space','space','cpm',0,'Salt Lake City UT',NULL,NULL),
    ('Period','period','cpm',0,'West Palm Beach. FL',NULL,NULL),
    ('State only','state only','cpm',0,'AZ',NULL,NULL),
    ('Preserve','preserve','cpm',0,'Columbus, OH','COLUMBUS','OH'),
    ('Conflict','conflict','cpm',0,'Dallas, TX','Chicago','IL'),
    ('Ambiguous','ambiguous','cpm',0,'Il.FL,NC',NULL,NULL)`)
				applyLaterTestMigrations(t, ctx, admin, "079")
				var good int
				if err := admin.QueryRow(ctx, `SELECT count(*) FROM drivers WHERE (full_name='Comma' AND city='Louisville' AND state='KY') OR (full_name='Space' AND city='Salt Lake City' AND state='UT') OR (full_name='Period' AND city='West Palm Beach' AND state='FL') OR (full_name='State only' AND city IS NULL AND state='AZ') OR (full_name='Preserve' AND city='COLUMBUS' AND state='OH') OR (full_name='Conflict' AND city='Chicago' AND state='IL') OR (full_name='Ambiguous' AND city IS NULL AND state IS NULL)`).Scan(&good); err != nil || good != 7 {
					t.Fatalf("address transfer: %d %v", good, err)
				}
				if err := admin.QueryRow(ctx, `SELECT count(*) FROM profile_notes WHERE body LIKE 'Former home entry:%'`).Scan(&good); err != nil || good != 2 {
					t.Fatalf("unresolved evidence not preserved: %d %v", good, err)
				}
			} else {
				exec(string(source))
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, app)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			fleet := NewFleetRepository(pool)
			date := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
			input := DriverInput{FullName: "Wizard Driver", PayType: "cpm", PayRate: .65, Active: true, Status: "active", HireDate: &date, NewTruck: &TruckInput{UnitNumber: "WIZARD-01", Active: true, Status: "available"}, NewTruckOwnedByDriver: true}
			driver, err := fleet.CreateDriver(ctx, input)
			if err != nil || driver.TruckID == nil {
				t.Fatalf("combined create: %+v %v", driver, err)
			}
			truck, err := fleet.GetTruck(ctx, *driver.TruckID)
			if err != nil || truck.DriverID == nil || *truck.DriverID != driver.ID || truck.Status != "assigned" || truck.IsCompanyOwned || truck.OwnerName != driver.FullName {
				t.Fatalf("truck/ownership: %+v %v", truck, err)
			}
			checkStart := func(id string) {
				t.Helper()
				var correct bool
				if err := pool.QueryRow(ctx, `SELECT roster_start_week='2026-10-07'
 AND EXISTS(SELECT 1 FROM truck_driver_assignments WHERE driver_id=d.id AND unassigned_at IS NULL AND (assigned_at AT TIME ZONE 'America/New_York')::date='2026-10-07')
 AND EXISTS(SELECT 1 FROM driver_dispatcher_assignments WHERE driver_id=d.id AND unassigned_at IS NULL AND (assigned_at AT TIME ZONE 'America/New_York')::date='2026-10-07')
 FROM drivers d WHERE id=$1`, id).Scan(&correct); err != nil || !correct {
					t.Fatalf("setup must preserve the Wednesday hire date: %v", err)
				}
			}
			checkStart(driver.ID)
			raw, _ := json.Marshal(driver)
			if strings.Contains(string(raw), "driverHome") || strings.Contains(string(raw), "homeVersion") {
				t.Fatal("retired address exposed")
			}
			count := func(table string) int {
				t.Helper()
				var n int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			beforeDrivers, beforeTrucks, beforeOwners := count("drivers"), count("trucks"), count("investors")
			input.FullName = "Duplicate Truck Driver"
			if _, err = fleet.CreateDriver(ctx, input); err == nil {
				t.Fatal("duplicate truck accepted")
			}
			if count("drivers") != beforeDrivers || count("trucks") != beforeTrucks || count("investors") != beforeOwners {
				t.Fatal("failed truck creation leaked rows")
			}
			input.FullName = "Invalid Service Driver"
			input.NewTruck = &TruckInput{UnitNumber: "ROLLBACK-TRUCK", Active: true, Status: "available"}
			if _, err = fleet.CreateDriver(WithDriverServiceIntents(ctx, []DriverServiceIntent{{Service: "invalid", Action: "add"}}, ""), input); err == nil {
				t.Fatal("invalid service accepted")
			}
			if count("drivers") != beforeDrivers || count("trucks") != beforeTrucks || count("investors") != beforeOwners {
				t.Fatal("failed setup leaked rows")
			}
			input.NewTruck = nil
			input.NewTruckOwnedByDriver = false
			input.TruckID = driver.TruckID
			input.FullName = "Existing Truck Driver"
			existing, err := fleet.CreateDriver(ctx, input)
			if err != nil || existing.TruckID == nil || *existing.TruckID != *driver.TruckID || count("trucks") != beforeTrucks {
				t.Fatal("existing truck path", err)
			}
			// The same transaction helper is used by FleetScope completion.
			var actor string
			if err = pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Wizard actor','$2test') RETURNING id`).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			hire := fleetscope.Event{Version: 1, Type: "driver.hired", CompanyID: "08000000-0000-0000-0000-000000000001", EventID: "08000000-0000-0000-0000-000000000002", OccurredAt: time.Now(), Driver: fleetscope.Driver{ID: "08000000-0000-0000-0000-000000000003", FullName: "Intake Wizard", DriverType: "company", HireDate: "2026-10-07"}}
			intake, err := fleet.AcceptFleetScopeHire(ctx, hire, "wizard-test")
			if err != nil {
				t.Fatal(err)
			}
			input.TruckID = nil
			input.NewTruck = &TruckInput{UnitNumber: "INTAKE-TRUCK", Active: true, Status: "available"}
			input.FullName = "Intake Wizard"
			completed, err := fleet.CompleteDriverIntake(WithTaskViewer(ctx, actor, true), intake.IntakeID, actor, "", &input, true)
			if err != nil || completed.TruckID == nil {
				t.Fatalf("intake truck: %+v %v", completed, err)
			}
			checkStart(completed.ID)
		})
	}
}
