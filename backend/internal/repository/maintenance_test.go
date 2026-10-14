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
				sql = strings.Replace(sql, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
			}
			exec(sql)
			if mode == "migration" {
				exec(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,cost_breakdown) SELECT id,name,'Oil change','2026-09-01',10,'[{"name":"Oil change","amount":"10.00"}]' FROM expense_settings WHERE kind='category' AND name='Maintenance'`)
				exec("UPDATE expense_settings SET name='Truck upkeep' WHERE kind='category' AND name='Maintenance'")
				exec(string(migration))
				var status string
				var date *time.Time
				var services []MaintenanceWork
				if err := admin.QueryRow(ctx, "SELECT status,service_date,services FROM maintenance_records").Scan(&status, &date, &services); err != nil || status != "needs_review" || date != nil || services[0].TrackingCode != "" {
					t.Fatal("migration invented completion", status, date, err)
				}
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
			input := MaintenanceInput{ID: id, TruckID: &truck, VendorID: &vendor.ID, ServiceDate: &date, Odometer: &odo, Location: "shop", Status: "completed", Actor: "Test operator", Services: []MaintenanceWork{{TypeCode: "preventive", Name: "Oil change", Amount: payTestString("100.10"), TrackingCode: "oil"}, {TypeCode: "tires", Name: "Tire repair", Amount: payTestString("20.20")}}, Expense: &ExpenseInput{CategoryID: category, ExpenseDate: date, TruckID: &truck, ExpenseType: payTestString("Oil change & tires"), Amount: amount, BaseAmount: &amount, PaymentFee: "0.00", CoveredBy: payTestString("Company"), CostBreakdown: &costs}}
			visit, err := repo.Save(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if visit.Expense == nil || *visit.Expense.Amount != "120.30" || visit.VendorAddress != "Original address" {
				t.Fatal(visit)
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
			if err := pool.QueryRow(ctx, "SELECT count(*)::text FROM maintenance_changes WHERE record_id=$1", id).Scan(&amount); err != nil || amount != "3" {
				t.Fatal("missing void audit", err)
			}
		})
	}
}
