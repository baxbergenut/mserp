package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMaintenanceDue(t *testing.T) {
	today := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	last := "2026-10-01"
	current := "2026-10-14"
	base := 1000
	now := 1050
	interval := 100
	days := 30
	for _, tc := range []struct {
		miles int
		want  string
	}{{1050, "current"}, {1095, "upcoming"}, {1100, "due"}, {1101, "overdue"}} {
		v := MaintenanceDue{LastDate: &last, LastMiles: &base, CurrentDate: &current, CurrentMiles: &tc.miles, Schedule: &MaintenanceSchedule{Enabled: true, IntervalMiles: &interval, IntervalDays: &days, WarningMiles: 10, WarningDays: 5}}
		calculateMaintenanceDue(&v, today)
		if v.Status != tc.want {
			t.Fatal(tc, v)
		}
	}
	missing := MaintenanceDue{LastDate: &last, CurrentDate: &current, CurrentMiles: &now, Schedule: &MaintenanceSchedule{Enabled: true, IntervalMiles: &interval}}
	calculateMaintenanceDue(&missing, today)
	if missing.Status != "missing_information" {
		t.Fatal(missing)
	}
	old := "2026-10-01"
	stale := MaintenanceDue{LastDate: &last, LastMiles: &base, CurrentDate: &old, CurrentMiles: &now, Schedule: &MaintenanceSchedule{Enabled: true, IntervalMiles: &interval}}
	calculateMaintenanceDue(&stale, today)
	if stale.Status != "missing_information" {
		t.Fatal(stale)
	}
	dayInterval := 1
	overdue := MaintenanceDue{LastDate: &last, Schedule: &MaintenanceSchedule{Enabled: true, IntervalMiles: &interval, IntervalDays: &dayInterval}}
	calculateMaintenanceDue(&overdue, today)
	if overdue.Status != "overdue" {
		t.Fatal(overdue)
	}
}

func TestMaintenanceDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
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
	migration, err := os.ReadFile("../../sql/096_maintenance.sql")
	if err != nil {
		t.Fatal(err)
	}
	entryMigration, err := os.ReadFile("../../sql/097_maintenance_entry.sql")
	if err != nil {
		t.Fatal(err)
	}
	preventativeMigration, err := os.ReadFile("../../sql/099_preventative_maintenance.sql")
	if err != nil {
		t.Fatal(err)
	}
	brandMigration, err := os.ReadFile("../../sql/100_maintenance_brands_history.sql")
	if err != nil {
		t.Fatal(err)
	}
	taskMigration, err := os.ReadFile("../../sql/101_maintenance_tasks_roles.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("maintenance_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + quoted + "; SET search_path TO " + quoted + ",public; GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app")
			defer func() { admin.Exec(ctx, "SET search_path TO public; DROP SCHEMA "+quoted+" CASCADE") }()
			sql := strings.ReplaceAll(string(source), "\r\n", "\n")
			if mode == "migration" {
				sql = strings.Replace(sql, strings.ReplaceAll(string(taskMigration), "\r\n", "\n"), "", 1)
				sql = strings.Replace(sql, strings.ReplaceAll(string(brandMigration), "\r\n", "\n"), "", 1)
				sql = strings.Replace(sql, strings.ReplaceAll(string(preventativeMigration), "\r\n", "\n"), "", 1)
				sql = strings.Replace(sql, strings.ReplaceAll(string(entryMigration), "\r\n", "\n"), "", 1)
				sql = strings.Replace(sql, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
			}
			exec(sql)
			if mode == "migration" {
				exec(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,cost_breakdown) SELECT id,name,'Oil change','2026-09-01',10,'[{"name":"Oil change","amount":"10.00"}]' FROM expense_settings WHERE kind='category' AND name='Maintenance'`)
				exec("UPDATE expense_settings SET name='Truck upkeep' WHERE kind='category' AND name='Maintenance'")
				exec(string(migration))
				exec(string(entryMigration))
				exec(string(preventativeMigration))
				exec(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,cost_breakdown,source_spreadsheet_id,source_sheet,source_row,unit_number) SELECT id,name,'PM Service','2026-09-02',20,'[{"name":"PM Service","amount":"20.00"}]','test-sheet','Maintenance_and_other_expenses',2,'Historical unit' FROM expense_settings WHERE kind='category' AND name='Truck upkeep'`)
				exec(string(brandMigration))
				var historicalStatus, historicalDate, tracking string
				if err := admin.QueryRow(ctx, `SELECT m.status,m.service_date::text,m.services->0->>'trackingCode' FROM maintenance_records m JOIN expenses e ON e.id=m.expense_id WHERE e.source_spreadsheet_id='test-sheet'`).Scan(&historicalStatus, &historicalDate, &tracking); err != nil || historicalStatus != "completed" || historicalDate != "2026-09-02" || tracking != "pm" {
					t.Fatal("historical correction", historicalStatus, historicalDate, tracking, err)
				}
				exec(string(taskMigration))
				var status string
				var date *time.Time
				var services []MaintenanceWork
				if err := admin.QueryRow(ctx, "SELECT m.status,m.service_date,m.services FROM maintenance_records m JOIN expenses e ON e.id=m.expense_id WHERE e.source_spreadsheet_id IS NULL").Scan(&status, &date, &services); err != nil || status != "needs_review" || date != nil || services[0].TrackingCode != "" {
					t.Fatal("migration invented completion", status, date, err)
				}
			}
			exec(`INSERT INTO maintenance_records(work_order,notes,services) VALUES('fleet-board:oil-change:row-999',E'Type of oil: 10w 30\nSource information: synthetic import','[{"typeCode":"oil","name":"Oil change","amount":null,"trackingCode":"oil"}]')`)
			exec(string(preventativeMigration))
			var importedGrade string
			if err := admin.QueryRow(ctx, "SELECT oil_type FROM maintenance_records WHERE work_order='fleet-board:oil-change:row-999'").Scan(&importedGrade); err != nil || importedGrade != "10W-30" {
				t.Fatal("historical explicit grade", importedGrade, err)
			}
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			// Fleet tables in historical test schemas predate explicit ownership migrations.
			exec("GRANT SELECT,INSERT,UPDATE,DELETE ON trucks,expenses,expense_settings,drivers,truck_driver_assignments,app_users TO mserp_app")
			testMaintenanceTaskLifecycle(t, ctx, admin, pool)
			repo := NewMaintenanceRepository(pool)
			category, err := repo.CategoryID(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var truck, id string
			if err := admin.QueryRow(ctx, "INSERT INTO trucks(unit_number,active) VALUES('MAINT-TEST',true) RETURNING id").Scan(&truck); err != nil {
				t.Fatal(err)
			}
			pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&id)
			vendor, err := repo.SaveVendor(ctx, MaintenanceVendor{Name: "Test shop", Address: "Original address", Active: true})
			if err != nil {
				t.Fatal(err)
			}
			date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
			odo := 100000
			amount := "120.30"
			costs := []ExpenseCostItem{{Name: "Oil change", Amount: "100.10"}, {Name: "Tire repair", Amount: "20.20"}}
			input := MaintenanceInput{OilType: "15W-40", Notes: "Synthetic last service note", ID: id, TruckID: &truck, VendorID: &vendor.ID, ServiceDate: &date, Odometer: &odo, Location: "shop", Status: "completed", Actor: "Test operator", Services: []MaintenanceWork{{TypeCode: "oil", Name: "Oil change", Amount: payTestString("100.10"), TrackingCode: "oil"}, {TypeCode: "tires", Name: "Tire repair", Amount: payTestString("20.20")}}, Expense: &ExpenseInput{CategoryID: category, ExpenseDate: date, TruckID: &truck, ExpenseType: payTestString("Oil change & tires"), Amount: amount, BaseAmount: &amount, PaymentFee: "0.00", CoveredBy: payTestString("Company"), CostBreakdown: &costs}}
			visit, err := repo.Save(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if visit.Expense == nil || *visit.Expense.Amount != "120.30" || visit.VendorAddress != "Original address" {
				t.Fatal(visit)
			}
			preventive, err := repo.Preventative(ctx, "oil", "MAINT-TEST", false, 1, 25)
			if err != nil || len(preventive.Items) != 1 {
				t.Fatal(preventive, err)
			}
			summary := preventive.Items[0]
			if summary.RecordID == nil || *summary.RecordID != visit.ID || summary.Cost == nil || *summary.Cost != "100.10" || summary.OilType != "15W-40" || summary.Notes != input.Notes || summary.VendorName != "Test shop" || summary.CurrentMiles == nil || *summary.CurrentMiles != odo {
				t.Fatal("incorrect preventative summary", summary)
			}
			pm, err := repo.Preventative(ctx, "pm", "MAINT-TEST", false, 1, 25)
			if err != nil || len(pm.Items) != 1 || pm.Items[0].RecordID != nil || pm.Items[0].Cost != nil {
				t.Fatal("oil must not complete PM", pm, err)
			}
			var defaults int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM maintenance_vendors WHERE superseded_by IS NULL AND name<>'Test shop'").Scan(&defaults); err != nil || defaults != 12 {
				t.Fatal("vendor catalog", defaults, err)
			}
			if _, err := repo.Save(ctx, input); !errors.Is(err, ErrMaintenanceConflict) {
				t.Fatal("duplicate identity accepted", err)
			}
			report, err := repo.Reports(ctx, MaintenanceQuery{TruckID: truck})
			if err != nil || report.Total != "120.30" || len(report.ServiceTypes) != 2 {
				t.Fatal(report, err)
			}
			readings, err := repo.Readings(ctx, truck)
			if err != nil || len(readings) != 1 || readings[0].Mileage != odo {
				t.Fatal(readings, err)
			}
			bad := input
			bad.ID = ""
			pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&bad.ID)
			bad.Expense = &ExpenseInput{CategoryID: category, ExpenseDate: date, TruckID: &truck, ExpenseType: payTestString("Bad costs"), Amount: "119.30", BaseAmount: payTestString("119.30"), CostBreakdown: &[]ExpenseCostItem{}}
			if _, err := repo.Save(ctx, bad); err == nil {
				t.Fatal("mismatched work costs accepted")
			}
			page, err := repo.List(ctx, MaintenanceQuery{TruckID: truck, PageSize: 25})
			if err != nil || page.Total != 1 {
				t.Fatal("partial financial transaction", page, err)
			}
			vendor.Address = "Renamed address"
			vendor, err = repo.SaveVendor(ctx, vendor)
			if err != nil {
				t.Fatal(err)
			}
			unchanged, err := repo.Get(ctx, id)
			if err != nil || unchanged.VendorAddress != "Original address" {
				t.Fatal("vendor snapshot changed", err)
			}
			mileage := 110000
			reading, err := repo.SaveReading(ctx, MaintenanceReading{TruckID: truck, Date: "2026-10-14", Mileage: mileage, ActorName: "Operator"})
			if err == nil {
				t.Fatal("same-day conflicting mileage accepted", reading)
			}
			reading, err = repo.SaveReading(ctx, MaintenanceReading{TruckID: truck, Date: "2026-10-15", Mileage: mileage, ActorName: "Operator"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.SaveReading(ctx, MaintenanceReading{TruckID: truck, Date: "2026-10-13", Mileage: 120000, ActorName: "Operator"}); err == nil {
				t.Fatal("decreasing historical mileage accepted")
			}
			if _, err := repo.SaveReading(ctx, MaintenanceReading{TruckID: truck, Date: "2026-10-16", Mileage: 100500, ActorName: "Operator"}); err == nil {
				t.Fatal("manual reading chronology was bypassed")
			}
			interval := 5000
			schedule, err := repo.SaveSchedule(ctx, MaintenanceSchedule{ServiceCode: "oil", IntervalMiles: &interval, Enabled: true, WarningMiles: 1000, WarningDays: 14})
			if err != nil {
				t.Fatal(err)
			}
			due, err := repo.Due(ctx, truck, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range due {
				if d.ServiceCode == "oil" && (d.Status != "overdue" || d.LastMiles == nil || *d.LastMiles != odo) {
					t.Fatal(d)
				}
				if d.ServiceCode == "pm" && d.LastDate != nil {
					t.Fatal("oil change reset PM")
				}
			}
			schedule.TruckID = &truck
			schedule.ID = ""
			schedule.Version = 0
			schedule.Enabled = false
			if _, err := repo.SaveSchedule(ctx, schedule); err != nil {
				t.Fatal(err)
			}
			due, err = repo.Due(ctx, truck, date)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range due {
				if d.ServiceCode == "oil" && d.Status != "disabled" {
					t.Fatal("override failed", d)
				}
			}
			input.Version = visit.Version
			input.Expense.UpdatedAt = &visit.Expense.UpdatedAt
			input.VendorName = visit.VendorName
			input.VendorAddress = visit.VendorAddress
			input.VendorPhone = visit.VendorPhone
			input.VendorContact = visit.VendorContact
			input.Notes = "Corrected notes"
			updated, err := repo.Save(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Save(ctx, input); !errors.Is(err, ErrMaintenanceConflict) {
				t.Fatal("stale edit accepted", err)
			}
			history, err := repo.History(ctx, id)
			if err != nil || len(history) != 2 {
				t.Fatal(history, err)
			}
			if _, err := pool.Exec(ctx, "UPDATE expenses SET truck_id=NULL WHERE id=$1", *updated.ExpenseID); err == nil {
				t.Fatal("ledger detached from completed work")
			}
			if err := repo.Void(ctx, id, updated.Version, "Duplicate test entry", "Operator"); err != nil {
				t.Fatal(err)
			}
			voided, err := repo.Get(ctx, id)
			if err != nil || voided.Status != "voided" || voided.ExpenseID != nil {
				t.Fatal(voided, err)
			}
			emptyOil, err := repo.Preventative(ctx, "oil", "MAINT-TEST", false, 1, 25)
			if err != nil || len(emptyOil.Items) != 1 || emptyOil.Items[0].RecordID != nil {
				t.Fatal("voided work reset service", emptyOil, err)
			}
			exec(`INSERT INTO maintenance_records(truck_id,status,service_date,services,vendor_name,notes) VALUES($1,'completed','2026-10-13','[{"typeCode":"oil","name":"Oil change","amount":null,"trackingCode":"oil"}]','Historical shop','Known work, cost unknown')`, truck)
			exec(`INSERT INTO maintenance_records(truck_id,status,service_date,services,vendor_name,notes) VALUES($1,'needs_review','2026-10-15','[{"typeCode":"oil","name":"Oil change","amount":null,"trackingCode":"oil"}]','Unconfirmed shop','Must not replace completed history')`, truck)
			historical, err := repo.Preventative(ctx, "oil", "MAINT-TEST", false, 1, 25)
			if err != nil || len(historical.Items) != 1 || historical.Items[0].Cost != nil || historical.Items[0].VendorName != "Historical shop" || historical.Items[0].OilType != "" || *historical.Items[0].LastDate != "2026-10-13" {
				t.Fatal("unknown historical cost or unconfirmed latest visit", historical, err)
			}
			if err := pool.QueryRow(ctx, "SELECT count(*)::text FROM maintenance_changes WHERE record_id=$1", id).Scan(&amount); err != nil || amount != "3" {
				t.Fatal("missing void audit", err)
			}
		})
	}
}

func TestMaintenanceOilType(t *testing.T) {
	for source, want := range map[string]string{"SAE 15W-40": "15W-40", "10w 30": "10W-30", "5 W - 40": "5W-40", " Custom oil ": "Custom oil", "": ""} {
		if actual := NormalizeMaintenanceOilType(source); actual != want {
			t.Fatal(source, actual, want)
		}
	}
}

func testMaintenanceTaskLifecycle(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var truck, baseline string
	if err := admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,active) VALUES('REMINDER-TEST',true) RETURNING id::text`).Scan(&truck); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO maintenance_records(truck_id,status,service_date,odometer,services) VALUES($1,'completed','2026-10-01',1000,'[{"typeCode":"preventive","name":"PM Service","amount":null,"trackingCode":"pm"}]') RETURNING id::text`, truck).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	repo := NewMaintenanceRepository(pool)
	miles := 100
	schedule, err := repo.SaveSchedule(ctx, MaintenanceSchedule{TruckID: &truck, ServiceCode: "pm", IntervalMiles: &miles, Enabled: true, WarningMiles: 10})
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	live := map[string]MaintenanceReading{truck: {Mileage: 1095, Date: "2026-10-15"}}
	refresh := func(readings map[string]MaintenanceReading) {
		t.Helper()
		if err := repo.RefreshMaintenanceTasks(ctx, today, readings); err != nil {
			t.Fatal(err)
		}
	}
	count := func(open, total int) {
		t.Helper()
		var a, b int
		if err := admin.QueryRow(ctx, `SELECT count(*) FILTER(WHERE completed_at IS NULL),count(*) FROM system_task_records WHERE maintenance_truck_id=$1`, truck).Scan(&a, &b); err != nil || a != open || b != total {
			t.Fatal("reminder counts", a, b, open, total, err)
		}
	}
	exec(`INSERT INTO maintenance_odometer_readings(truck_id,reading_date,mileage,source,actor_name) VALUES($1,'2026-10-15',1050,'manual','Tester')`, truck)
	refresh(live)
	refresh(live)
	count(1, 1)
	refresh(nil)
	count(1, 1) // a telemetry outage must not resolve a warning
	live[truck] = MaintenanceReading{Mileage: 1101, Date: "2026-10-15"}
	refresh(live)
	count(1, 1)
	var title string
	if err := admin.QueryRow(ctx, `SELECT title FROM system_task_records WHERE maintenance_truck_id=$1`, truck).Scan(&title); err != nil || !strings.Contains(title, "overdue") {
		t.Fatal(title, err)
	}
	exec(`INSERT INTO maintenance_records(truck_id,status,service_date,odometer,services) VALUES($1,'completed','2026-10-15',1101,'[{"typeCode":"preventive","name":"PM Service","amount":null,"trackingCode":"pm"}]')`, truck)
	refresh(live)
	count(0, 1)
	live[truck] = MaintenanceReading{Mileage: 1195, Date: "2026-10-15"}
	refresh(live)
	count(1, 2)
	schedule.Enabled = false
	if _, err = repo.SaveSchedule(ctx, schedule); err != nil {
		t.Fatal(err)
	}
	refresh(live)
	count(0, 2)
	// Keep this independent fixture out of the other maintenance assertions.
	exec(`UPDATE trucks SET active=false WHERE id=$1`, truck)
}
