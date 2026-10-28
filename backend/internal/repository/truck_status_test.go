package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTruckStatusesDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable MSERP_DRIVER_BOARD_TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
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
	migration, err := os.ReadFile("../../sql/149_truck_statuses.sql")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, ok := strings.Cut(strings.ReplaceAll(string(init), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n"))
	if !ok {
		t.Fatal("exact migration missing from init")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("truck_status_%d", time.Now().UnixNano())
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
				execAdmin(`INSERT INTO trucks(unit_number,active,status,notes) VALUES ('OLD-INACTIVE',false,'available','Keep historical note'),('OLD-HOLD',true,'hold','Waiting for parts'),('OLD-OOS',true,'out_of_service','Engine damaged');`)
				execAdmin(string(migration))
				readMigration, e := os.ReadFile("../../sql/162_fleet_read_performance.sql")
				if e != nil {
					t.Fatal(e)
				}
				execAdmin(string(readMigration))
				for unit, want := range map[string]string{"OLD-INACTIVE": "terminated", "OLD-HOLD": "shop", "OLD-OOS": "out_of_service"} {
					var got, reason string
					if e := admin.QueryRow(ctx, `SELECT fleet_truck_operating_status(id),status_reason FROM trucks WHERE unit_number=$1`, unit).Scan(&got, &reason); e != nil || got != want || reason == "" {
						t.Fatal(unit, got, reason, e)
					}
				}
				var note string
				if e := admin.QueryRow(ctx, `SELECT notes FROM trucks WHERE unit_number='OLD-INACTIVE'`).Scan(&note); e != nil || note != "Keep historical note" {
					t.Fatal("migration changed notes", e)
				}
			}
			execAdmin(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app;UPDATE fleet_status_control SET enabled=true`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			verifyTruckStatuses(t, ctx, pool)
			verifyTruckGPSAlerts(t, ctx, pool)
		})
	}
}

func verifyTruckStatuses(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	fleet := NewFleetRepository(pool)
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	exec := func(sql string, args ...any) { t.Helper(); _, e := pool.Exec(ctx, sql, args...); must(e) }
	scalar := func(sql string, args ...any) string {
		t.Helper()
		var v string
		must(pool.QueryRow(ctx, sql, args...).Scan(&v))
		return v
	}
	driver, e := fleet.CreateDriver(ctx, DriverInput{FullName: "Truck Status Operator", PayType: "cpm", PayRate: .7, Active: true})
	must(e)
	truck, e := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "TS-100", Status: "available", Active: true, DriverID: &driver.ID})
	must(e)
	check := func(status string, active bool, assigned bool) Truck {
		t.Helper()
		v, e := fleet.GetTruck(ctx, truck.ID)
		must(e)
		if v.OperatingStatus != status || v.Active != active || (v.DriverID != nil) != assigned {
			t.Fatalf("status=%s active=%v driver=%v; want %s %v %v", v.OperatingStatus, v.Active, v.DriverID, status, active, assigned)
		}
		var oldStatus, oldReason string
		must(pool.QueryRow(ctx, `SELECT fleet_truck_operating_status($1),fleet_truck_status_reason($1)`, truck.ID).Scan(&oldStatus, &oldReason))
		if v.OperatingStatus != oldStatus || v.StatusReason != oldReason {
			t.Fatal("fleet view changed status semantics", v, oldStatus, oldReason)
		}
		return v
	}
	change := func(status, reason string) Truck {
		t.Helper()
		v, e := fleet.GetTruck(ctx, truck.ID)
		must(e)
		v, e = fleet.ChangeTruckCondition(ctx, truck.ID, "", FleetStatusChange{Status: status, Version: v.StatusVersion, Reason: reason})
		must(e)
		return v
	}
	check("in_use", true, true)
	driver, e = fleet.GetDriver(ctx, driver.ID)
	must(e)
	_, e = fleet.ChangeAvailability(ctx, driver.ID, "", FleetStatusChange{Status: "home", Version: driver.StatusVersion})
	must(e)
	check("in_use", true, true)
	_, e = fleet.ChangeTruckCondition(ctx, truck.ID, "", FleetStatusChange{Status: "shop", Version: truck.StatusVersion, Reason: "   "})
	if e == nil {
		t.Fatal("blank reason accepted")
	}
	change("shop", "Brake repair")
	check("shop", true, true)
	other, e := fleet.CreateDriver(ctx, DriverInput{FullName: "Other Operator", PayType: "cpm", PayRate: .7, Active: true})
	must(e)
	tx, e := pool.Begin(ctx)
	must(e)
	must(setBoardActor(ctx, tx, "", "test"))
	e = assignTruck(ctx, tx, truck.ID, other.ID)
	tx.Rollback(ctx)
	if e == nil {
		t.Fatal("assigned restricted truck")
	}
	if _, e = pool.Exec(ctx, `INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date)`, driver.ID, other.ID); e == nil {
		t.Fatal("added crew to restricted truck")
	}
	change("out_of_service", "Awaiting engine replacement")
	current := check("out_of_service", false, true)
	_, e = fleet.UpdateTruck(ctx, truck.ID, TruckInput{UnitNumber: current.UnitNumber, Status: current.Status, Active: false, DriverID: &driver.ID, Notes: payTestString("Unrelated note")})
	must(e)
	check("out_of_service", false, true)
	_, e = fleet.ChangeTruckCondition(ctx, truck.ID, "", FleetStatusChange{Status: "terminated", Version: current.StatusVersion, Reason: "Owner leaving"})
	if e == nil {
		t.Fatal("terminated an employed operator's truck")
	}
	change("ready", "Repair inspected")
	check("in_use", true, true)
	before := scalar(`SELECT count(*)::text FROM fleet_status_history WHERE entity_id=$1`, truck.ID)
	change("in_use", "No change")
	check("in_use", true, true)
	if scalar(`SELECT count(*)::text FROM fleet_status_history WHERE entity_id=$1`, truck.ID) != before {
		t.Fatal("no-op wrote history")
	}
	change("shop", "Manual inspection")
	one := scalar(`INSERT INTO maintenance_records(truck_id,dispatch_status,notes) VALUES($1,'shop','Brake inspection') RETURNING id::text`, truck.ID)
	two := scalar(`INSERT INTO maintenance_records(truck_id,dispatch_status,notes) VALUES($1,'out_of_service','Engine inspection') RETURNING id::text`, truck.ID)
	check("out_of_service", false, true)
	exec(`UPDATE maintenance_records SET dispatch_status='' WHERE id=$1`, one)
	v := check("out_of_service", false, true)
	_, e = fleet.ChangeTruckCondition(ctx, truck.ID, "", FleetStatusChange{Status: "ready", Version: v.StatusVersion, Reason: "Try release"})
	if e == nil {
		t.Fatal("bypassed independent restriction")
	}
	exec(`UPDATE maintenance_records SET dispatch_status='' WHERE id=$1`, two)
	check("shop", true, true)
	change("ready", "Inspections complete")
	// Status and employment races serialize with assignment transactions.
	idle, e := fleet.CreateTruck(ctx, TruckInput{UnitNumber: "TS-001", Status: "available", Active: true})
	must(e)
	tx, e = pool.Begin(ctx)
	must(e)
	must(setBoardActor(ctx, tx, "", "test"))
	must(assignTruck(ctx, tx, idle.ID, other.ID))
	result := make(chan error, 1)
	go func() {
		_, e := fleet.ChangeTruckCondition(ctx, idle.ID, "", FleetStatusChange{Status: "shop", Version: idle.StatusVersion, Reason: "Stale inspection"})
		result <- e
	}()
	must(tx.Commit(ctx))
	if e = <-result; e != ErrDriverBoardConflict {
		t.Fatal("stale assignment race", e)
	}
	// Explicit removal while restricted preserves the restriction; rehire cannot clear it.
	change("out_of_service", "Long-term storage")
	tx, e = pool.Begin(ctx)
	must(e)
	must(setBoardActor(ctx, tx, "", "test"))
	must(releaseTruckDriver(ctx, tx, truck.ID))
	must(tx.Commit(ctx))
	check("out_of_service", false, false)
	change("ready", "Storage inspection complete")
	check("ready", true, false)
	change("terminated", "Truck left company")
	check("terminated", false, false)
	snapshot, e := readSyncLocal(ctx, pool, "")
	must(e)
	for _, row := range snapshot.Trucks {
		if row.Truck.MSERPTruckID == truck.ID && (row.Truck.OperationalActive || row.Truck.OperationalStatus != "available") {
			t.Fatal("new status leaked into provider contract", row.Truck)
		}
	}
	change("ready", "Truck returned to company")
	check("ready", true, false)
	page, e := fleet.ListTrucksPage(ctx, Pagination{Page: 1, PageSize: 1}, "TS-", TruckFilters{Status: "ready"})
	must(e)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != truck.ID {
		t.Fatal("status pagination", page)
	}
	if page.StatusCounts["ready"] != 1 || page.StatusCounts["in_use"] != 1 || len(page.StatusCounts) != 5 {
		t.Fatal("status card counts must ignore the selected status and page size", page.StatusCounts)
	}
	page, e = fleet.ListTrucksPage(ctx, Pagination{Page: 1, PageSize: 25}, "TS-")
	must(e)
	if len(page.Items) != 2 || page.Items[0].ID != truck.ID || page.Items[1].ID != idle.ID {
		t.Fatal("priority ordering", page)
	}
	page, e = fleet.ListTrucksPage(ctx, Pagination{Page: 1, PageSize: 25}, "TS-100", TruckFilters{Status: "in_use"})
	must(e)
	if page.Total != 0 || page.StatusCounts["ready"] != 1 || page.StatusCounts["in_use"] != 0 {
		t.Fatal("status cards must count the search even when the selected status is empty", page)
	}
	if _, e = pool.Exec(ctx, `UPDATE trucks SET status='maintenance',status_reason='' WHERE id=$1`, truck.ID); e == nil {
		t.Fatal("direct writer bypassed required reason")
	}
	if _, e = pool.Exec(ctx, `UPDATE trucks SET status='hold' WHERE id=$1`, truck.ID); e == nil {
		t.Fatal("Hold accepted")
	}
	if _, e = pool.Exec(ctx, `INSERT INTO maintenance_records(truck_id,dispatch_status) VALUES($1,'shop')`, truck.ID); e == nil {
		t.Fatal("work order bypassed reason")
	}
	before = scalar(`SELECT count(*)::text FROM fleet_status_history`)
	_, e = fleet.GetTruck(ctx, truck.ID)
	must(e)
	_, e = fleet.StatusHistory(ctx, "truck", truck.ID, "", "", "", "", 1, 0)
	must(e)
	if scalar(`SELECT count(*)::text FROM fleet_status_history`) != before {
		t.Fatal("read wrote history")
	}
	compareLookup := func(full, compact any, fields string) {
		t.Helper()
		a, _ := json.Marshal(full)
		b, _ := json.Marshal(compact)
		var oldRows, newRows []map[string]any
		must(json.Unmarshal(a, &oldRows))
		must(json.Unmarshal(b, &newRows))
		if len(oldRows) != len(newRows) {
			t.Fatal("lookup row count changed")
		}
		for i := range oldRows {
			for _, field := range strings.Fields(fields) {
				if !reflect.DeepEqual(oldRows[i][field], newRows[i][field]) {
					t.Fatalf("lookup field %s changed: %v vs %v", field, oldRows[i][field], newRows[i][field])
				}
			}
		}
	}
	fullTrucks, e := fleet.ListTrucks(ctx)
	must(e)
	compactTrucks, e := fleet.TruckOptions(ctx)
	must(e)
	compareLookup(fullTrucks, compactTrucks, "id unitNumber status operatingStatus active driverId driverName ownerId ownerName ownerSince ownershipVersion isCompanyOwned year make model")
	fullDrivers, e := fleet.ListDrivers(ctx)
	must(e)
	compactDrivers, e := fleet.DriverOptions(ctx)
	must(e)
	compareLookup(fullDrivers, compactDrivers, "id fullName active status employmentType driverType isOwnerOperator phone email dispatcherId dispatcherName truckId truckUnit teamRole teamPartnerId teamPartnerName teamVersion teamTruckId teamTruckUnit teamStartWeek")

}
