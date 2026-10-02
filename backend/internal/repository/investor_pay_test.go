package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTruckRecurringCharges(t *testing.T) {
	data := TruckChargeData{Phases: []TruckChargePhase{{TruckID: "truck", TypeID: "admin", WeekStart: "2026-01-05", Amount: "100.00", Included: true}, {TruckID: "truck", TypeID: "admin", WeekStart: "2026-10-05", Amount: "200.00", Included: true}}}
	types := []ChargeType{{ID: "admin", Name: "Admin", Direction: "charge", Eligibility: "loads", Rules: []ChargeTypeRule{{WeekStart: "2026-01-05", Eligibility: "calendar"}, {WeekStart: "2026-10-05", Eligibility: "loads"}}}}
	rows := truckRecurringCharges(data, types, "truck", "2026-09-28", false)
	if len(rows) != 1 || rows[0].Amount != "-100.00" {
		t.Fatalf("historical rule: %+v", rows)
	}
	if rows = truckRecurringCharges(data, types, "truck", "2026-10-05", false); len(rows) != 0 {
		t.Fatal("no-load week should not charge")
	}
	if rows = truckRecurringCharges(data, types, "truck", "2026-10-05", true); len(rows) != 1 || rows[0].Amount != "-200.00" {
		t.Fatalf("dated amount: %+v", rows)
	}
	if rows = truckRecurringCharges(data, types, "other", "2026-10-05", true); len(rows) != 0 {
		t.Fatal("fee crossed trucks")
	}
}

func TestInvestorPayDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_INVESTOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_INVESTOR_TEST_DATABASE_URL to a disposable _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "_test") {
		t.Fatal("only a disposable _test database is allowed")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("investor_pay_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, err = admin.Exec(ctx, `CREATE SCHEMA `+quoted+`; SET search_path TO `+quoted+`,public`); err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			init, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			migration, e := os.ReadFile("../../sql/041_truck_settlements.sql")
			if e != nil {
				t.Fatal(e)
			}
			sql := strings.ReplaceAll(string(init), "\r\n", "\n")
			m := strings.ReplaceAll(string(migration), "\r\n", "\n")
			if mode == "migration" {
				sql = strings.Replace(sql, m, "", 1)
				if strings.Contains(sql, "CREATE TABLE truck_settlement_terms") {
					t.Fatal("failed to isolate migration")
				}
			}
			if _, err = admin.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
			if mode == "migration" {
				if _, err = admin.Exec(ctx, m); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = admin.Exec(ctx, `GRANT USAGE ON SCHEMA `+quoted+` TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,dispatchers,trucks,truck_driver_assignments,loads,gross_board_entries,gross_board_extra_entries,driver_pay_weeks,app_users,fuel_transactions,fuel_transaction_items,tolls TO mserp_app`); err != nil {
				t.Fatal(err)
			}
			cfg, e := pgxpool.ParseConfig(dsn)
			if e != nil {
				t.Fatal(e)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			cfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			var owner, driver, investor, truck, actor string
			mustID := func(query string, args ...any) string {
				t.Helper()
				var id string
				if e := pool.QueryRow(ctx, query, args...).Scan(&id); e != nil {
					t.Fatal(e)
				}
				return id
			}
			owner = mustID(`INSERT INTO drivers(full_name,normalized_name,is_owner_operator,pay_type,pay_rate) VALUES('Hector','hector',true,'gross_percentage',88) RETURNING id`)
			driver = mustID(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Hired driver','hired driver','gross_percentage',25) RETURNING id`)
			investor = mustID(`INSERT INTO investors(full_name,driver_id) VALUES('Hector',$1) RETURNING id`, owner)
			truck = mustID(`INSERT INTO trucks(unit_number,owner_id) VALUES('TRUCK-A',$1) RETURNING id`, investor)
			actor = mustID(`INSERT INTO app_users(username,password_hash) VALUES('accountant','$2test') RETURNING id`)
			if _, e = pool.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01')`, truck, driver); e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,pickup_time,raw_payload) VALUES(991,'LOAD-1','delivered',10000,10000,1000,'TRUCK-A','2026-09-28 00:01:00+00','{"trip":{"mile":900,"empty_mile":100},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"A","state":"TX"}},{"stop_type":"delivery","ordering":2,"location":{"city":"B","state":"OH"}}]}')`); e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,'2026-09-28','LOAD-1',991,10000)`, driver); e != nil {
				t.Fatal(e)
			}
			cr := NewDriverChargeRepository(pool)
			pr := NewDriverPayRepository(pool)
			term := TruckTerm{TruckID: truck, OwnerID: investor, WeekStart: "2026-01-05", SharePercent: "88"}
			if e = cr.SaveTruckTerm(ctx, term, actor); e == nil {
				t.Fatal("single-truck driver owner must use Driver charges")
			}
			ownTruck := mustID(`INSERT INTO trucks(unit_number,owner_id) VALUES('OWNER-TRUCK',$1) RETURNING id`, investor)
			if _, e = pool.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01')`, ownTruck, owner); e != nil {
				t.Fatal(e)
			}
			ownTerm := term
			ownTerm.TruckID = ownTruck
			if e = cr.SaveTruckTerm(ctx, ownTerm, actor); e == nil {
				t.Fatal("self-operated truck must use Driver charges")
			}
			management, e := cr.TruckCharges(ctx)
			if e != nil || len(management.EligibleTruckIDs) != 1 || management.EligibleTruckIDs[0] != truck {
				t.Fatalf("investor management eligibility: %+v %v", management, e)
			}
			if _, e = pool.Exec(ctx, `UPDATE truck_driver_assignments SET unassigned_at='2026-01-02' WHERE truck_id=$1`, ownTruck); e != nil {
				t.Fatal(e)
			}
			if e = cr.SaveTruckTerm(ctx, term, actor); e != nil {
				t.Fatal(e)
			}
			if e = cr.SaveTruckTerm(ctx, term, actor); e != ErrChargeConflict {
				t.Fatalf("stale term: %v", e)
			}
			typ, e := cr.SaveType(ctx, ChargeType{Name: "Admin", Direction: "charge", Amount: "100.00", Amounts: []string{"100.00"}, Eligibility: "calendar"}, actor)
			if e != nil {
				t.Fatal(e)
			}
			phase := TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: "2026-01-05", Amount: "100.00", Included: true, TypeVersion: typ.Version}
			if e = cr.SaveTruckCharge(ctx, phase, actor); e != nil {
				t.Fatal(e)
			}
			week, _ := time.Parse(time.DateOnly, "2026-09-28")
			report, e := pr.InvestorPay(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if len(report.Drivers) != 1 {
				t.Fatalf("expected investor truck: %+v", report)
			}
			card := report.Drivers[0]
			if len(card.Loads) != 1 || card.Loads[0].Fee != "8800.00" || card.Loads[0].DriverFee != "2500.00" {
				t.Fatalf("wrong load calculation: %+v", card)
			}
			if len(card.AutoCharges) != 2 || card.AutoCharges[0].Amount != "-100.00" || card.AutoCharges[1].Amount != "-2500.00" {
				t.Fatalf("wrong charges: %+v", card.AutoCharges)
			}
			payroll, e := pr.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if len(payroll.Drivers) != 1 || payroll.Drivers[0].Loads[0].Fee != "2500.00" || len(payroll.Drivers[0].AutoCharges) != 0 {
				t.Fatalf("driver charged truck fees: %+v", payroll)
			}

			expense := mustID(`INSERT INTO expenses(company,category,expense_type,truck_id,driver_id,owner_id,covered_by,expense_date,amount) VALUES('MS Express','Maintenance','Repair',$1,$2,$3,'Truck Owner','2026-09-28',300) RETURNING id`, truck, driver, investor)
			fuelID := mustID(`INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,prompts,raw_payload) VALUES('production','pay-test',$1,'relay-test','2026-09-28 12:00+00',1500,1500,0,false,'USD','','','','','','','','','','',0,0,'America/New_York','[{"label":"Truck #","value":"TRUCK-A"}]','{}') RETURNING id`, driver)
			if _, e = pool.Exec(ctx, `INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid) VALUES($1,0,'fuel','diesel',1500),($1,1,'fuel','def',50)`, fuelID); e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint) VALUES($1,'2026-09-29','2026-09-29','','','','','TRUCK-A','','','2026-09-28','12:00','',200,repeat('a',64))`, truck); e != nil {
				t.Fatal(e)
			}
			report, e = pr.InvestorPay(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			card = report.Drivers[0]
			if card.FuelTotal != "1500.00" || card.TollTotal != "200.00" || len(card.Edits.ExpenseDeductions) != 1 || card.Edits.ExpenseDeductions[0].ExpenseID != expense {
				t.Fatalf("truck costs: %+v", card)
			}
			payroll, e = pr.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			for _, d := range payroll.Drivers {
				if d.FuelTotal != "0.00" || d.TollTotal != "0.00" || len(d.Edits.ExpenseDeductions) > 0 {
					t.Fatalf("cost deducted twice: %+v", d)
				}
			}
			card.Edits.ExpenseDeductions[0].Apply = true
			saved, e := pr.SaveInvestorPay(ctx, card.Edits, actor)
			if e != nil {
				t.Fatal(e)
			}
			if len(saved.ExpenseDeductions) != 1 || !saved.ExpenseDeductions[0].Saved {
				t.Fatal("expense payment not saved")
			}
			if saved.Version != 1 {
				t.Fatal("missing saved version")
			}
			if _, e = pr.SaveInvestorPay(ctx, card.Edits, actor); e != ErrDriverPayConflict {
				t.Fatalf("stale pay accepted: %v", e)
			}
			report, e = pr.InvestorPay(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			final, e := pr.SettleInvestor(ctx, week, truck, report.Revision, actor, "", false)
			if e != nil {
				t.Fatal(e)
			}
			if !final.Drivers[0].Settlement.Finalized {
				t.Fatal("not finalized")
			}
			if _, e = pr.SaveInvestorPay(ctx, saved, actor); e == nil {
				t.Fatal("edited finalized pay")
			}
			if e = cr.SaveTruckCharge(ctx, TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: "2026-09-28", Amount: "100.00", Included: false, TypeVersion: typ.Version}, actor); e == nil {
				t.Fatal("changed finalized truck charge")
			}
			if _, e = pool.Exec(ctx, `UPDATE drivers SET pay_rate=30 WHERE id=$1`, driver); e != nil {
				t.Fatal(e)
			}
			frozen, e := pr.InvestorPay(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if frozen.Drivers[0].Loads[0].DriverFee != "2500.00" {
				t.Fatal("finalized history changed")
			}
			if _, e = pr.SettleInvestor(ctx, week, truck, frozen.Revision, actor, "Correction", true); e != nil {
				t.Fatal(e)
			}
			// Corrections retain the expense payment's original destination.
			if _, e = pool.Exec(ctx, `UPDATE expense_payments SET investor_truck_id=NULL WHERE expense_id=$1`, expense); e == nil {
				t.Fatal("payment destination changed")
			}
			checkInvestorChargeHandoff(t, ctx, pool, actor)
			// Owner-only work in another week uses Driver Pay once, with the 88% tariff.
			if _, e = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) SELECT $1,service_date+7,load_number,load_record_id,driver_rate FROM gross_board_entries WHERE driver_id=$2`, owner, driver); e != nil {
				t.Fatal(e)
			}
			ownerWeek := week.AddDate(0, 0, 7)
			ownerPay, e := pr.Get(ctx, ownerWeek)
			if e != nil {
				t.Fatal(e)
			}
			var found bool
			for _, d := range ownerPay.Drivers {
				if d.ID == owner {
					found = true
					if d.Loads[0].Fee != "8800.00" || len(d.AutoCharges) != 1 || d.AutoCharges[0].Amount != "-100.00" {
						t.Fatalf("owner calculation: %+v", d)
					}
				}
			}
			if !found {
				t.Fatal("owner absent")
			}
			ownerInvestor, e := pr.InvestorPay(ctx, ownerWeek)
			if e != nil {
				t.Fatal(e)
			}
			if len(ownerInvestor.Drivers) != 0 {
				t.Fatal("duplicate investor statement for owner driving")
			}
			// A mixed week has one investor settlement and hired labor only.
			if _, e = pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,pickup_time,raw_payload) SELECT 992,'LOAD-2',status,1000,1000,total_miles,truck_unit,pickup_time,raw_payload FROM loads WHERE id=991`); e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,'2026-10-06','LOAD-2',992,1000)`, driver); e != nil {
				t.Fatal(e)
			}
			mixed, e := pr.InvestorPay(ctx, ownerWeek)
			if e != nil {
				t.Fatal(e)
			}
			if len(mixed.Drivers) != 1 || len(mixed.Drivers[0].Loads) != 2 || len(mixed.Drivers[0].AutoCharges) != 2 || mixed.Drivers[0].AutoCharges[1].Amount != "-300.00" {
				t.Fatalf("mixed-week allocation: %+v", mixed)
			}
			mixedDrivers, e := pr.Get(ctx, ownerWeek)
			if e != nil {
				t.Fatal(e)
			}
			for _, d := range mixedDrivers.Drivers {
				if d.ID == owner && len(d.Loads) > 0 {
					t.Fatal("owner earnings counted twice in mixed week")
				}
			}
			if e = cr.SaveCell(ctx, ChargeCell{DriverID: driver, TypeID: typ.ID, WeekStart: "2026-10-05", Included: true, Amount: "100.00", TypeVersion: typ.Version}, actor); e != nil {
				t.Fatal(e)
			}
			assignments, e := cr.List(ctx, driver)
			if e != nil {
				t.Fatal(e)
			}
			if len(assignments.Schedules) != 1 {
				t.Fatal("missing driver fee")
			}
			move := TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: "2026-10-05", Amount: "100.00", Included: true, TypeVersion: typ.Version, MoveScheduleID: assignments.Schedules[0].ID, MoveScheduleVersion: assignments.Schedules[0].Version}
			if e = cr.SaveTruckCharge(ctx, move, actor); e != nil {
				t.Fatal(e)
			}
			moved, e := pr.Get(ctx, ownerWeek)
			if e != nil {
				t.Fatal(e)
			}
			for _, d := range moved.Drivers {
				if len(d.Edits.GeneratedCharges) > 0 {
					t.Fatal("moved fee still charged to driver")
				}
			}
			if e = cr.SaveTruckCharge(ctx, move, actor); e != ErrChargeConflict {
				t.Fatalf("stale move accepted: %v", e)
			}
			emptyWeek, e := pr.InvestorPay(ctx, ownerWeek.AddDate(0, 0, 7))
			if e != nil {
				t.Fatal(e)
			}
			if len(emptyWeek.Drivers) != 1 || len(emptyWeek.Drivers[0].Loads) != 0 || len(emptyWeek.Drivers[0].AutoCharges) != 1 {
				t.Fatalf("calendar fees in no-load week: %+v", emptyWeek)
			}
		})
	}
}
