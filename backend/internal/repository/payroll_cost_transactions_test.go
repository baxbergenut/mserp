package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCostCarryTransactionAllocation(t *testing.T) {
	rows := []PayCostTransaction{{ID: "diesel", Amount: "100.00"}, {ID: "def", Amount: "10.00"}, {ID: "refund", Amount: "-5.00"}}
	carried := allocateCostCarry(nil, rows, 10500, 10200, "2026-10-19")
	if len(carried) != 1 || carried[0].ID != "def" || carried[0].Remaining != "3.00" || !carried[0].Carried {
		t.Fatalf("partial transaction carry: %+v", carried)
	}
	carried = allocateCostCarry(carried, []PayCostTransaction{{ID: "new", Amount: "20.00"}}, 2000, 100, "2026-10-26")
	if len(carried) != 2 || carried[0].Remaining != "2.00" || carried[1].Remaining != "20.00" {
		t.Fatalf("FIFO across weeks: %+v", carried)
	}
	carried = allocateCostCarry(carried, nil, 0, 2500, "2026-11-02")
	if len(carried) != 1 || carried[0].Remaining != "-3.00" {
		t.Fatalf("credit lost: %+v", carried)
	}
	carried = allocateCostCarry(carried, []PayCostTransaction{{ID: "later", Amount: "5.00"}}, 500, 0, "2026-11-09")
	if len(carried) != 1 || carried[0].ID != "later" || carried[0].Remaining != "2.00" {
		t.Fatalf("credit offset: %+v", carried)
	}
	changed := allocateCostCarry(nil, nil, 500, 100, "2026-10-19")
	if len(changed) != 1 || !changed[0].BalanceOnly || changed[0].Remaining != "4.00" {
		t.Fatalf("missing evidence fabricated: %+v", changed)
	}
}

func TestPayrollCostStartDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_COST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable cost test database required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	for _, migrated := range []bool{false, true} {
		t.Run(fmt.Sprint("migrated=", migrated), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			admin, e := pgx.Connect(ctx, dsn)
			if e != nil {
				t.Fatal(e)
			}
			defer admin.Close(ctx)
			schema := fmt.Sprintf("cost_start_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec("CREATE SCHEMA " + quoted + ";SET search_path TO " + quoted + ",public")
			defer admin.Exec(context.Background(), "SET search_path TO public;DROP SCHEMA "+quoted+" CASCADE")
			source, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			migration, e := os.ReadFile("../../sql/173_payroll_cost_start.sql")
			if e != nil {
				t.Fatal(e)
			}
			body := strings.ReplaceAll(string(source), "\r\n", "\n")
			if migrated {
				body = strings.Replace(body, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1)
			}
			exec(body)
			if migrated {
				exec(string(migration))
			}
			exec("GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app;GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO mserp_app")
			appCfg := cfg.Copy()
			appCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			appCfg.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, appCfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			var driver, owner, truck, actor string
			if e = admin.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('cost-test',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); e != nil {
				t.Fatal(e)
			}
			if e = admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Cost Owner','cost owner','gross_percentage',87) RETURNING id::text`).Scan(&driver); e != nil {
				t.Fatal(e)
			}
			if e = admin.QueryRow(ctx, `INSERT INTO investors(full_name,driver_id) VALUES('Cost Owner',$1) RETURNING id::text`, driver).Scan(&owner); e != nil {
				t.Fatal(e)
			}
			if e = admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,owner_id) VALUES('COST-OWNER',$1) RETURNING id::text`, owner).Scan(&truck); e != nil {
				t.Fatal(e)
			}
			exec(`UPDATE truck_ownership_history SET start_known=false WHERE truck_id=$1`, truck)
			exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-09-01 04:00+00')`, truck, driver)
			exec(`WITH f AS (
 INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,prompts,raw_payload)
 SELECT env,label,$1::uuid,'identity',stamp::timestamptz,amount,amount,0,false,'USD','','Pilot','','','','','','City','OH','',0,0,'US/Eastern','[{"label":"Truck #","value":"COST-OWNER"}]','{}'
 FROM (VALUES ('old','2026-09-22 12:00+00',90.21,'production'),('diesel','2026-10-20 12:00+00',100.00,'production'),('def','2026-10-21 12:00+00',10.00,'production'),('credit','2026-10-22 12:00+00',-5.00,'production'),('staging','2026-10-20 12:00+00',999.00,'staging')) v(label,stamp,amount,env)
 RETURNING id,relay_transaction_id,total_amount_paid
 ) INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid)
 SELECT id,0,'fuel',CASE WHEN relay_transaction_id='def' THEN 'def' ELSE 'diesel' END,total_amount_paid FROM f`, driver)
			exec(`UPDATE fuel_transactions SET reported_truck_units=ARRAY['COST-OWNER']`)
			exec(`INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint,prepass_environment)
 VALUES($1,'2026-10-21','2026-10-21','','','','','COST-OWNER','Agency','Plaza','2026-10-18','12:00','',12.25,repeat('a',64),'production')`, truck)
			exec(`INSERT INTO driver_pay_weeks(driver_id,week_start,fuel_override) VALUES($1,'2026-09-21',0)`, driver)
			pay := NewDriverPayRepository(pool)
			read := func(week string) DriverPayDriver {
				t.Helper()
				w, _ := chargeWeek(week)
				r, e := pay.GetDriverWeek(ctx, w, driver)
				if e != nil || len(r.Drivers) != 1 {
					t.Fatalf("read %s %+v %v", week, r, e)
				}
				return r.Drivers[0]
			}
			before := read("2026-10-19")
			if before.Edits.Costs.FuelCarry != "90.21" {
				t.Fatalf("legacy carry %+v", before.Edits.Costs)
			}
			// Both pinned records and legacy overrides must be excluded by the boundary.
			exec(`INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount) VALUES($1,'2026-09-21',90.21,0,0,0)`, driver)
			exec(`INSERT INTO payroll_cost_start(week_start,reason) VALUES('2026-10-19','Start using Clustie; Fuel/Toll only')`)
			current := read("2026-10-19")
			if current.Edits.Costs.FuelCarry != "0.00" || current.Edits.Costs.FuelDue != "105.00" || current.Edits.Costs.TollDue != "12.25" {
				t.Fatalf("baseline %+v", current.Edits.Costs)
			}
			w, _ := chargeWeek("2026-10-19")
			fuel, e := pay.CostTransactions(ctx, "driver", driver, w, "fuel")
			if e != nil {
				t.Fatal(e)
			}
			if fuel.SourceTotal != "105.00" || fuel.Carry != "0.00" || len(fuel.Transactions) != 3 || fuel.Notice != "" {
				t.Fatalf("fuel detail %+v", fuel)
			}
			toll, e := pay.CostTransactions(ctx, "driver", driver, w, "toll")
			if e != nil {
				t.Fatal(e)
			}
			if len(toll.Transactions) != 1 || toll.Transactions[0].Date != "2026-10-18" || toll.Transactions[0].PostedDate != "2026-10-21" || toll.SourceTotal != "12.25" {
				t.Fatalf("posted vs crossing %+v", toll)
			}
			edits := current.Edits
			f, tollAmount := "-102.00", "-10.00"
			edits.FuelOverride = &f
			edits.TollOverride = &tollAmount
			if _, e = pay.Save(ctx, edits, actor); e != nil {
				t.Fatal(e)
			}
			next := read("2026-10-26")
			if next.Edits.Costs.FuelCarry != "3.00" || next.Edits.Costs.TollCarry != "2.25" {
				t.Fatalf("new carry lost %+v", next.Edits.Costs)
			}
			w2, _ := chargeWeek("2026-10-26")
			carried, e := pay.CostTransactions(ctx, "driver", driver, w2, "fuel")
			if e != nil {
				t.Fatal(e)
			}
			if len(carried.Transactions) != 1 || !carried.Transactions[0].Carried || carried.Transactions[0].Remaining != "3.00" || !strings.Contains(carried.Transactions[0].Description, "DEF") {
				t.Fatalf("carried DEF %+v", carried)
			}
			// Frozen collection stays untouched; only obsolete opening metadata is hidden.
			frozen := read("2026-10-19")
			frozen.Edits.Costs.FuelCarry = "90.21"
			frozen.Edits.Costs.FuelDue = "195.21"
			b, _ := json.Marshal(frozen)
			exec(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES($1,'2026-10-19',$2)`, driver, b)
			after := read("2026-10-19")
			if after.Edits.Costs.FuelCarry != "0.00" || *after.Edits.FuelOverride != "-102.00" {
				t.Fatalf("frozen amount changed %+v", after.Edits)
			}
			// Raw company-driver fuel is evidence, not a personal obligation.
			company := frozen
			company.IsOwnerOperator = false
			company.Settlement = &PayrollSettlement{Finalized: true}
			company.Edits.Costs = &DriverPayCosts{FuelDue: "90.21", FuelCarry: "90.21", TollDue: "0.00", TollCarry: "0.00"}
			companyReport := DriverPayWeek{WeekStart: "2026-10-19", Drivers: []DriverPayDriver{company}}
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = applyCostStartDisplay(ctx, tx, &companyReport); e != nil {
				t.Fatal(e)
			}
			tx.Rollback(ctx)
			if companyReport.Drivers[0].Edits.Costs.FuelDue != "0.00" {
				t.Fatal("company fuel became a personal deduction")
			}
			var stored string
			if e = admin.QueryRow(ctx, `SELECT report->'edits'->'costs'->>'fuelCarry' FROM payroll_settlements WHERE driver_id=$1`, driver).Scan(&stored); e != nil || stored != "90.21" {
				t.Fatalf("frozen JSON changed %s %v", stored, e)
			}
			if _, e = pool.Exec(ctx, `UPDATE payroll_cost_start SET week_start='2026-10-26'`); e == nil {
				t.Fatal("boundary mutable")
			}
			if _, e = pool.Exec(ctx, `TRUNCATE payroll_cost_start`); e == nil {
				t.Fatal("boundary truncatable")
			}
			// Investor costs use the same boundary and item routing. A saved investor
			// destination remains a truck statement, including owner-operated weeks.
			exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-09-21',87)`, truck, owner)
			exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits) VALUES($1,$2,'2026-10-19',jsonb_build_object('driverId',$1::uuid::text,'weekStart','2026-10-19','comments','{}'::jsonb,'adjustments','[]'::jsonb))`, truck, owner)
			exec(`INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement)
 VALUES('investor_cost:'||$1::uuid::text||':fuel',$2,'2026-09-21','investor',$1,$1,'Fuel',-90.21,0,jsonb_build_object('id',$1::uuid::text,'truckId',$1::uuid::text,'investorId',$2::uuid::text))`, truck, owner)
			investorFuel, e := pay.CostTransactions(ctx, "investor", truck, w, "fuel")
			if e != nil {
				t.Fatal(e)
			}
			if investorFuel.Carry != "0.00" || investorFuel.SourceTotal != "105.00" || len(investorFuel.Transactions) != 3 || investorFuel.Notice != "" {
				t.Fatalf("investor fuel detail %+v", investorFuel)
			}
			investorToll, e := pay.CostTransactions(ctx, "investor", truck, w, "toll")
			if e != nil {
				t.Fatal(e)
			}
			if investorToll.SourceTotal != "12.25" || len(investorToll.Transactions) != 1 {
				t.Fatalf("investor toll detail %+v", investorToll)
			}
		})
	}
}
