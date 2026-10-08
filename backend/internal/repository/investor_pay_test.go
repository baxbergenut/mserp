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
			identityMigration, e := os.ReadFile("../../sql/064_load_truck_identity.sql")
			if e != nil {
				t.Fatal(e)
			}
			identitySQL := strings.ReplaceAll(string(identityMigration), "\r\n", "\n")
			identityInit := strings.Replace(identitySQL, "BEGIN;\n", "", 1)
			identityInit = strings.TrimSpace(strings.TrimSuffix(identityInit, "COMMIT;\n"))
			sql := strings.ReplaceAll(string(init), "\r\n", "\n")
			m := strings.ReplaceAll(string(migration), "\r\n", "\n")
			if mode == "migration" {
				sql = strings.Replace(sql, m, "", 1)
				sql = strings.Replace(sql, identityInit, "", 1)
				if strings.Contains(sql, "CREATE TABLE truck_unit_aliases") {
					t.Fatal("failed to isolate identity migration")
				}
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
			if mode == "migration" {
				if _, err = admin.Exec(ctx, `INSERT INTO trucks(unit_number) VALUES('LEGACY'); INSERT INTO loads(id,load_id,status,load_pay,total_pay,truck_unit) VALUES(990,'LEGACY-LOAD','delivered',100,100,'LEGACY'); UPDATE trucks SET unit_number='RENAMED-LEGACY' WHERE unit_number='LEGACY';`); err != nil {
					t.Fatal(err)
				}
				if _, err = admin.Exec(ctx, identitySQL); err != nil {
					t.Fatal(err)
				}
				var bound bool
				if err = admin.QueryRow(ctx, `SELECT l.truck_id=t.id FROM loads l CROSS JOIN trucks t WHERE l.id=990 AND t.unit_number='RENAMED-LEGACY'`).Scan(&bound); err != nil || !bound {
					t.Fatalf("legacy alias backfill: %v %v", bound, err)
				}
				if _, err = admin.Exec(ctx, `DELETE FROM loads WHERE id=990; DELETE FROM trucks WHERE unit_number='RENAMED-LEGACY'`); err != nil {
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
			testInvestorAttribution(t, ctx, pool, pr, week, truck, owner, investor, actor)
			payroll, e := pr.Get(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			if len(payroll.Drivers) != 1 || payroll.Drivers[0].Loads[0].Fee != "2500.00" || len(payroll.Drivers[0].AutoCharges) != 0 {
				t.Fatalf("driver charged truck fees: %+v", payroll)
			}

			expense := mustID(`INSERT INTO expenses(company,category_id,category,expense_type,truck_id,driver_id,owner_id,covered_by,expense_date,amount) SELECT 'MS Express',id,name,'Repair',$1,$2,$3,'Truck Owner','2026-09-28',300 FROM expense_settings WHERE kind='category' AND name='Maintenance' RETURNING id`, truck, driver, investor)
			fuelID := mustID(`INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,prompts,raw_payload) VALUES('production','pay-test',$1,'relay-test','2026-09-28 12:00+00',1500,1500,0,false,'USD','','','','','','','','','','',0,0,'America/New_York','[{"label":"Truck #","value":"TRUCK-A"}]','{}') RETURNING id`, driver)
			if _, e = pool.Exec(ctx, `INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid) VALUES($1,0,'fuel','diesel',1500),($1,1,'fuel','def',50)`, fuelID); e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint) VALUES($1,'2026-09-29','2026-09-29','','','','','TRUCK-A','','','2026-09-27','12:00','',200,repeat('a',64))`, truck); e != nil {
				t.Fatal(e)
			}
			testTruckCostAliases(t, ctx, pool, truck, ownTruck, fuelID)
			report, e = pr.InvestorPay(ctx, week)
			if e != nil {
				t.Fatal(e)
			}
			card = report.Drivers[0]
			if card.FuelTotal != "1550.00" || card.TollTotal != "200.00" || len(card.Edits.ExpenseDeductions) != 1 || card.Edits.ExpenseDeductions[0].ExpenseID != expense {
				t.Fatalf("truck costs: %+v", card)
			}
			for _, ownerOperator := range []bool{false, true} {
				for _, payType := range []string{"gross_percentage", "cpm"} {
					if _, e = pool.Exec(ctx, `UPDATE drivers SET is_owner_operator=$2,pay_type=$3 WHERE id=$1`, driver, ownerOperator, payType); e != nil {
						t.Fatal(e)
					}
					costReport, costErr := pr.InvestorPay(ctx, week)
					if costErr != nil {
						t.Fatal(costErr)
					}
					if len(costReport.Drivers) != 1 || costReport.Drivers[0].FuelTotal != "1550.00" || costReport.Drivers[0].TollTotal != "200.00" {
						t.Fatalf("investor costs depend on driver classification (%v, %s): %+v", ownerOperator, payType, costReport)
					}
					statement := costReport.Drivers[0]
					if statement.InvestorID != investor || !statement.IsOwnerOperator || statement.PayType != "gross_percentage" || statement.Edits.Costs != nil {
						t.Fatalf("investor statement inherited personal driver cost eligibility: %+v", statement)
					}
				}
			}
			if _, e = pool.Exec(ctx, `UPDATE drivers SET is_owner_operator=false,pay_type='gross_percentage' WHERE id=$1`, driver); e != nil {
				t.Fatal(e)
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
			history, historyErr := pr.InvestorHistory(ctx, investor, Pagination{Page: 1, PageSize: 100})
			if historyErr != nil {
				t.Fatal(historyErr)
			}
			foundHistory := false
			for _, h := range history.Items {
				if h.WeekStart == "2026-09-28" {
					for _, d := range h.Trucks {
						if d.ID == truck {
							foundHistory = true
							if d.Settlement == nil || !d.Settlement.Finalized || d.Loads[0].DriverFee != "2500.00" {
								t.Fatal("profile history must use frozen investor report")
							}
						}
					}
				}
			}
			if !foundHistory {
				t.Fatal("finalized statement missing from investor history")
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

func testInvestorAttribution(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pr *DriverPayRepository, week time.Time, truck, owner, investor, actor string) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	read := func(target ...string) DriverPayWeek {
		t.Helper()
		r, err := pr.InvestorPay(ctx, week, target...)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	expectLoad := func() {
		t.Helper()
		r := read()
		if len(r.Drivers) != 1 || len(r.Drivers[0].Loads) != 1 || r.Drivers[0].Loads[0].Fee != "8800.00" || len(r.Issues) > 0 || len(r.Drivers[0].Issues) > 0 {
			t.Fatalf("attribution changed: %+v", r)
		}
	}
	exec(`UPDATE drivers SET active=false WHERE id=$1`, owner)
	expectLoad()
	var active bool
	if err := pool.QueryRow(ctx, `SELECT active FROM investors WHERE id=$1`, investor).Scan(&active); err != nil || !active {
		t.Fatalf("driver status changed investor: %v %v", active, err)
	}
	payroll, err := pr.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range payroll.Drivers {
		if d.ID == owner {
			t.Fatal("inactive non-driving owner appeared in Driver Pay")
		}
	}
	exec(`UPDATE drivers SET active=true WHERE id=$1`, owner)
	exec(`UPDATE trucks SET unit_number='RENAMED-A' WHERE id=$1`, truck)
	expectLoad()
	exec(`UPDATE loads SET truck_unit='' WHERE id=991`)
	expectLoad()
	exec(`UPDATE loads SET truck_unit='UNKNOWN' WHERE id=991`)
	r := read()
	if len(r.Issues) != 0 || len(r.Drivers) != 1 || len(r.Drivers[0].Issues) != 1 {
		t.Fatalf("assignment issue not scoped to truck: %+v", r)
	}
	if _, err = pr.SettleInvestor(ctx, week, truck, r.Revision, actor, "", false); err == nil {
		t.Fatal("finalized unresolved truck")
	}
	exec(`UPDATE loads SET truck_unit='TRUCK-A' WHERE id=991`)
	// Existing ID bindings survive reuse; a newly supplied ambiguous label does not resolve.
	exec(`INSERT INTO trucks(unit_number) VALUES('TRUCK-A')`)
	exec(`UPDATE loads SET truck_unit='TRUCK-A' WHERE id=991`)
	expectLoad()
	exec(`UPDATE loads SET truck_unit='UNKNOWN' WHERE id=991`)
	exec(`UPDATE loads SET truck_unit='TRUCK-A' WHERE id=991`)
	r = read()
	if len(r.Drivers[0].Loads) != 0 || len(r.Drivers[0].Issues) == 0 {
		t.Fatal("reused unit name was guessed")
	}
	exec(`DELETE FROM trucks WHERE unit_number='TRUCK-A'`)
	exec(`UPDATE loads SET truck_unit='TRUCK-A' WHERE id=991`)
	expectLoad()
	exec(`UPDATE trucks SET unit_number='TRUCK-A' WHERE id=$1`, truck)
	// An unrelated company driver cannot block every investor's statements.
	var unrelated string
	if err = pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Unrelated','unrelated','gross_percentage',25) RETURNING id`).Scan(&unrelated); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,raw_payload) VALUES(992,'UNRELATED-LOAD','delivered',1000,1000,100,'UNKNOWN','{}')`)
	exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES($1,'2026-09-28','UNRELATED-LOAD',992,1000)`, unrelated)
	expectLoad()
	exec(`DELETE FROM gross_board_entries WHERE driver_id=$1`, unrelated)
	exec(`DELETE FROM loads WHERE id=992`)
	exec(`DELETE FROM drivers WHERE id=$1`, unrelated)
	exec(`UPDATE trucks SET active=false WHERE id=$1`, truck)
	r = read()
	if len(r.Drivers) != 0 {
		t.Fatal("inactive truck shown in default pay view")
	}
	explicit := read(truck)
	if len(explicit.Drivers) != 1 || !explicit.Drivers[0].TruckInactive || len(explicit.Drivers[0].Loads) != 1 || explicit.Revision != r.Revision {
		t.Fatal("inactive historical statement or revision lost")
	}
	if _, err = pr.SettleInvestor(ctx, week, "", r.Revision, actor, "", false); err == nil {
		t.Fatal("whole week finalized hidden inactive truck")
	}
	exec(`UPDATE trucks SET active=true WHERE id=$1`, truck)
	exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,truck_unit) VALUES(993,'LATE-TRUCK-LOAD','delivered',100,100,'LATE-UNIT')`)
	exec(`INSERT INTO trucks(unit_number) VALUES('LATE-UNIT')`)
	var linked bool
	if err = pool.QueryRow(ctx, `SELECT l.truck_id=t.id FROM loads l CROSS JOIN trucks t WHERE l.id=993 AND t.unit_number='LATE-UNIT'`).Scan(&linked); err != nil || !linked {
		t.Fatalf("late truck not linked: %v %v", linked, err)
	}
	exec(`UPDATE trucks SET unit_number='LATE-RENAMED' WHERE unit_number='LATE-UNIT'`)
	exec(`UPDATE trucks SET unit_number='LATE-RENAMED-AGAIN' WHERE unit_number='LATE-RENAMED'`)
	exec(`UPDATE loads SET truck_unit='LATE-RENAMED' WHERE id=993`)
	if err = pool.QueryRow(ctx, `SELECT l.truck_id=t.id FROM loads l CROSS JOIN trucks t WHERE l.id=993 AND t.unit_number='LATE-RENAMED-AGAIN'`).Scan(&linked); err != nil || !linked {
		t.Fatalf("intermediate alias lost: %v %v", linked, err)
	}
	exec(`DELETE FROM trucks WHERE unit_number='LATE-RENAMED-AGAIN'`)
	if err = pool.QueryRow(ctx, `SELECT truck_id IS NULL FROM loads WHERE id=993`).Scan(&linked); err != nil || !linked {
		t.Fatalf("deleted truck link retained: %v %v", linked, err)
	}
	exec(`DELETE FROM loads WHERE id=993`)
}
