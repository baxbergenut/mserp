package repository

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpenseMaintenanceDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable local MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
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
	migration, err := os.ReadFile("../../sql/147_maintenance_expenses.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("maintenance_expense_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + quoted + "; SET search_path TO " + quoted + ",public")
			defer admin.Exec(ctx, "SET search_path TO public; DROP SCHEMA "+quoted+" CASCADE")
			source := strings.ReplaceAll(string(init), "\r\n", "\n")
			var legacyExpense, legacyRecord, missingRecord, truck, category string
			if mode == "migration" {
				source = strings.Replace(source, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
			}
			exec(source)
			scalar := func(sql string, args ...any) string {
				t.Helper()
				var value string
				if err := admin.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			truck = scalar("INSERT INTO trucks(unit_number,active) VALUES('MAINT-EXPENSE',true) RETURNING id::text")
			category = scalar("SELECT category_id::text FROM maintenance_configuration")
			if mode == "migration" {
				legacyExpense = scalar(`INSERT INTO expenses(category_id,category,truck_id,expense_date,expense_type,amount,base_amount,payment_fee,payment_type,paid_by,covered_by,cost_breakdown) VALUES($1,'Work orders',$2,'2026-10-01','PM Service',103,100,3,'EFS','Original payer','Company','[{"name":"Old invoice detail","amount":"100.00"}]') RETURNING id::text`, category, truck)
				legacyRecord = scalar(`UPDATE maintenance_records SET service_date='2026-10-01',status='completed',work_status='closed',odometer=99000,vendor_name='Original shop',notes='Original repair',services='[{"typeCode":"preventive","name":"PM Service","amount":"100.00","trackingCode":"pm"}]',payment_confirmation=jsonb_build_object('expenseId',$1::text,'amount','103.00','method','EFS','reference','','paidBy','Original payer') WHERE expense_id=$1 RETURNING id::text`, legacyExpense)
				missingRecord = scalar(`INSERT INTO maintenance_records(truck_id,dispatch_status,notes,services) VALUES($1,'shop','Unknown invoice','[{"typeCode":"other","name":"Other maintenance","amount":null,"trackingCode":""}]') RETURNING id::text`, truck)
				exec(`INSERT INTO maintenance_documents(record_id,file_name,content_type,size_bytes,sha256,data,uploaded_by) VALUES($1,'invoice.txt','text/plain',3,'test-hash','abc','Tester')`, legacyRecord)
				exec(`UPDATE expense_sheet_export_config SET enabled=true`)
				exec(string(migration))
				if got := scalar(`SELECT concat(amount,':',base_amount,':',payment_fee,':',payment_type,':',paid_by,':',jsonb_array_length(cost_breakdown)) FROM expenses WHERE id=$1`, legacyExpense); got != "103.00:100.00:3.00:EFS:Original payer:1" {
					t.Fatal("migration changed invoice", got)
				}
				if got := scalar(`SELECT count(*)::text FROM maintenance_records m JOIN expenses e ON e.id=m.expense_id WHERE m.id=$1 AND e.amount IS NULL AND NOT e.driver_settled AND e.covered_by IS NULL AND e.expense_date IS NULL`, missingRecord); got != "1" {
					t.Fatal("missing amount/payer/date invented", got)
				}
				if got := scalar(`SELECT fleet_truck_operating_status($1)`, truck); got != "shop" {
					t.Fatal("hold released", got)
				}
				if got := scalar(`SELECT count(*)::text FROM maintenance_records WHERE dispatch_status<>''`); got != "0" {
					t.Fatal("retired holds remain", got)
				}
				if got := scalar(`SELECT count(*)::text FROM expense_documents WHERE expense_id=$1`, legacyExpense); got != "1" {
					t.Fatal("receipt not copied", got)
				}
				if got := scalar(`SELECT count(*)::text FROM expense_sheet_exports`); got != "0" {
					t.Fatal("historical export queued", got)
				}
			}
			exec("GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + quoted + " TO mserp_app")
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
			repo := NewExpenseRepository(pool)
			amount, title, covered, description := "200.00", "Oil change", "Company", "Maintenance in one expense"
			miles := 100000
			input := ExpenseInput{CategoryID: category, ExpenseDate: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), TruckID: &truck, Amount: amount, BaseAmount: &amount, ExpenseType: &title, CoveredBy: &covered, Description: &description, CreatedByName: "Tester", Maintenance: &ExpenseMaintenance{Odometer: &miles, VendorName: "Test shop", VendorAddress: "123 Main St", VendorPhone: "5551234567", VendorContact: "Shop manager"}}
			saved, err := repo.CreateExpense(ctx, input)
			if err != nil {
				t.Fatal("create maintenance expense", err)
			}
			if saved.Maintenance == nil || saved.Maintenance.VendorName != "Test shop" || saved.Maintenance.Odometer == nil || *saved.Maintenance.Odometer != miles {
				t.Fatal("details not persisted", saved)
			}
			if got := scalar(`SELECT status||':'||service_date::text FROM maintenance_records WHERE expense_id=$1`, saved.ID); got != "completed:2026-10-22" {
				t.Fatal("service date", got)
			}
			if got := scalar(`SELECT count(*)::text FROM maintenance_records WHERE expense_id=$1`, saved.ID); got != "1" {
				t.Fatal("duplicate", got)
			}
			if got := scalar(`SELECT cardinality(program_ids)::text FROM maintenance_records WHERE expense_id=$1`, saved.ID); got != "1" {
				t.Fatal("PM program missing", got)
			}
			maintenance := NewMaintenanceRepository(pool)
			if members, err := maintenance.ProgramTrucks(ctx, OilChangeProgramID, truck); err != nil || len(members) != 0 {
				t.Fatal("expense must not enroll an excluded truck", members, err)
			}
			intervalMiles, intervalDays := 20000, 30
			if err := maintenance.SaveProgramTrucks(ctx, OilChangeProgramID, []ProgramTruckInput{{TruckID: truck, Active: true, OverrideInterval: true, IntervalMiles: &intervalMiles, IntervalDays: &intervalDays}}, ""); err != nil {
				t.Fatal(err)
			}
			assertDue := func() {
				t.Helper()
				members, err := maintenance.ProgramTrucks(ctx, OilChangeProgramID, truck)
				if err != nil || len(members) != 1 {
					t.Fatal("oil-change expense service history", members, err)
				}
				cycle := members[0]
				if cycle.LastRecordID == nil || *cycle.LastRecordID != saved.Maintenance.RecordID || cycle.LastDate == nil || *cycle.LastDate != "2026-10-22" || cycle.LastMiles == nil || *cycle.LastMiles != 100000 || cycle.NextMiles == nil || *cycle.NextMiles != 120000 || cycle.NextDate == nil || *cycle.NextDate != "2026-11-21" {
					t.Fatalf("oil-change expense did not drive mileage/date due limits: %+v", cycle)
				}
			}
			assertDue()
			input.UpdatedAt = &saved.UpdatedAt
			input.Maintenance = saved.Maintenance
			input.Maintenance.VendorAddress = "Corrected shop address"
			input.Amount = "250.00"
			input.BaseAmount = &input.Amount
			updated, err := repo.UpdateExpense(ctx, saved.ID, input)
			if err != nil {
				t.Fatal("edit expense", err)
			}
			if updated.Maintenance.VendorAddress != "Corrected shop address" || *updated.Amount != "250.00" {
				t.Fatal(updated)
			}
			assertDue()
			if _, err = repo.UpdateExpense(ctx, saved.ID, input); err == nil {
				t.Fatal("stale expense edit accepted")
			}
			input.UpdatedAt = &updated.UpdatedAt
			input.Maintenance = updated.Maintenance
			input.Maintenance.Version--
			if _, err = repo.UpdateExpense(ctx, saved.ID, input); err == nil {
				t.Fatal("stale maintenance edit accepted")
			}
			exec(`UPDATE expense_settings SET name='Oil service renamed',version=version+1 WHERE id=(SELECT expense_type_id FROM maintenance_types WHERE code='preventive')`)
			renamedTitle := "Oil service renamed"
			renamed := input
			renamed.UpdatedAt = nil
			renamed.ExpenseType = &renamedTitle
			renamed.Maintenance = &ExpenseMaintenance{Odometer: &miles, VendorName: "Renamed-type shop"}
			renamedExpense, err := repo.CreateExpense(ctx, renamed)
			if err != nil {
				t.Fatal("renamed expense type", err)
			}
			if got := scalar(`SELECT cardinality(program_ids)::text FROM maintenance_records WHERE expense_id=$1`, renamedExpense.ID); got != "1" {
				t.Fatal("expense type rename lost program link", got)
			}
			if err = repo.DeleteExpense(ctx, renamedExpense.ID, "Tester"); err != nil {
				t.Fatal(err)
			}
			// Moving an ordinary expense into Maintenance resolves its type/program
			// even if its already-entered expense type text stays the same.
			otherCategory := scalar(`SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`)
			moved := renamed
			moved.CategoryID = otherCategory
			moved.Maintenance = nil
			ordinary, err := repo.CreateExpense(ctx, moved)
			if err != nil {
				t.Fatal(err)
			}
			moved.UpdatedAt = &ordinary.UpdatedAt
			moved.CategoryID = category
			moved.Maintenance = &ExpenseMaintenance{VendorName: "Moved expense shop"}
			converted, err := repo.UpdateExpense(ctx, ordinary.ID, moved)
			if err != nil {
				t.Fatal("category conversion", err)
			}
			if got := scalar(`SELECT cardinality(program_ids)::text FROM maintenance_records WHERE expense_id=$1`, converted.ID); got != "1" {
				t.Fatal("category conversion lost program link", got)
			}
			if err = repo.DeleteExpense(ctx, converted.ID, "Tester"); err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				legacy, err := repo.GetExpense(ctx, legacyExpense)
				if err != nil {
					t.Fatal(err)
				}
				base := "150.00"
				method, payer := "EFS", "Original payer"
				empty := []ExpenseCostItem{}
				_, err = repo.UpdateExpense(ctx, legacyExpense, ExpenseInput{CategoryID: category, TruckID: &truck, ExpenseDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: &legacy.UpdatedAt, Amount: "153.00", BaseAmount: &base, PaymentFee: "3.00", PaymentType: &method, PaidBy: &payer, ExpenseType: &title, CoveredBy: &covered, CostBreakdown: &empty, Description: legacy.Description, Maintenance: legacy.Maintenance, CreatedByName: "Corrector"})
				if err != nil {
					t.Fatal("legacy paid edit", err)
				}
				if got := scalar(`SELECT payment_confirmation->>'amount'||':'||(payment_confirmation->'invoiceCorrections'->-1->>'amount') FROM maintenance_records WHERE id=$1`, legacyRecord); got != "103.00:153.00" {
					t.Fatal("payment evidence lost", got)
				}
			}
			if err = repo.DeleteExpense(ctx, saved.ID, "Tester"); err != nil {
				t.Fatal("delete", err)
			}
			if got := scalar(`SELECT status FROM maintenance_records WHERE id=$1`, saved.Maintenance.RecordID); got != "voided" {
				t.Fatal("audit not retained", got)
			}
			if got := scalar(`SELECT count(*)::text FROM maintenance_odometer_readings WHERE record_id=$1 AND active`, saved.Maintenance.RecordID); got != "0" {
				t.Fatal("deleted service still active", got)
			}
		})
	}
}
