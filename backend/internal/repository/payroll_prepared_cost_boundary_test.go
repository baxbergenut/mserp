package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPreparedPayrollCostBoundaryDatabase(t *testing.T) {
	for _, kind := range []string{"driver", "investor"} {
		t.Run(kind, func(t *testing.T) {
			withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
				seedPayrollProjectionTest(t, ctx, pool)
				exec := func(sql string, args ...any) {
					t.Helper()
					if _, err := pool.Exec(ctx, sql, args...); err != nil {
						t.Fatal(err)
					}
				}
				id := func(sql string, args ...any) string {
					t.Helper()
					var v string
					if err := pool.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
						t.Fatal(err)
					}
					return v
				}
				actor := id(`INSERT INTO app_users(username,password_hash) VALUES('Prepared cost boundary',crypt('test-only',gen_salt('bf'))) RETURNING id::text`)
				rowID := projectionTruck
				if kind == "driver" {
					rowID = projectionDriver
					exec(`DELETE FROM truck_settlement_terms;
 UPDATE investors SET driver_id='18200000-0000-0000-0000-000000000001' WHERE id='18200000-0000-0000-0000-000000000003';
 UPDATE drivers SET pay_type='gross_percentage',pay_rate=87 WHERE id='18200000-0000-0000-0000-000000000001'`)
				}
				exec(`WITH f AS (
 INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,prompts,raw_payload)
 SELECT env,label,$1::uuid,'identity',stamp::timestamptz,amount,amount,0,false,'USD','','Pilot','','','','','','City','OH','',0,0,'US/Eastern','[{"label":"Truck #","value":"PROJECTION"}]','{}'
 FROM (VALUES ('old','2026-09-22 12:00+00',90.21,'production'),('diesel','2026-10-20 12:00+00',100.00,'production'),('def','2026-10-21 12:00+00',10.00,'production'),('credit','2026-10-22 12:00+00',-5.00,'production'),('staging','2026-10-20 12:00+00',999.00,'staging')) v(label,stamp,amount,env)
 RETURNING id,relay_transaction_id,total_amount_paid
 ) INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid)
 SELECT id,0,'fuel',CASE WHEN relay_transaction_id='def' THEN 'def' ELSE 'diesel' END,total_amount_paid FROM f`, projectionDriver)
				exec(`INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint,prepass_environment)
 VALUES($1,'2026-10-21','2026-10-21','','','','','PROJECTION','Agency','Plaza','2026-10-18','12:00','',12.25,repeat('b',64),'production')`, projectionTruck)
				exec(`INSERT INTO driver_pay_weeks(driver_id,week_start,fuel_override) VALUES($1,'2026-09-21',0)`, projectionDriver)
				exec(`INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount) VALUES($1,'2026-09-21',90.21,0,0,0)`, projectionDriver)
				exec(`INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement)
 VALUES('investor_cost:'||$1::uuid::text||':fuel','18200000-0000-0000-0000-000000000003','2026-09-21','investor',$1,$1,'Fuel',-90.21,0,jsonb_build_object('id',$1::uuid::text,'truckId',$1::uuid::text,'investorId','18200000-0000-0000-0000-000000000003'))`, projectionTruck)
				exec(`INSERT INTO payroll_cost_start(week_start,reason) VALUES('2026-10-19','Prepared boundary fixture')`)
				repo := NewDriverPayRepository(pool)
				repo.ConfigurePreparedReads(true)
				repo.ConfigurePreparedFinalization(true)
				if err := repo.EnablePayrollShadow(ctx); err != nil {
					t.Fatal(err)
				}
				week, _ := chargeWeek("2026-10-19")
				next := week.AddDate(0, 0, 7)
				refresh := func() {
					t.Helper()
					for _, w := range []time.Time{week, next} {
						if err := repo.RequestPayrollPreparation(ctx, w, true); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
						t.Fatal(err)
					}
					for {
						worked, err := repo.PrepareNextPayrollWeek(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if !worked {
							break
						}
					}
				}
				parity := func() {
					t.Helper()
					for _, w := range []time.Time{week, next} {
						tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
						if err != nil {
							t.Fatal(err)
						}
						full, err := readPayrollProjectionReference(ctx, tx, w, true)
						_ = tx.Rollback(ctx)
						if err != nil {
							t.Fatal(err)
						}
						for i, pageKind := range []string{"driver", "investor"} {
							page, err := repo.ReadPreparedPayrollPage(ctx, pageKind, w, PayPageQuery{})
							a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
							b, _ := json.Marshal(page.Report)
							if err != nil || page.State != "ready" || string(a) != string(b) {
								t.Fatalf("cost boundary parity %s %s: %s %v\n%s\n%s", pageKind, w, page.State, err, a, b)
							}
						}
					}
				}
				refresh()
				parity()
				page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: rowID}, true)
				if err != nil || len(page.Drivers) != 1 {
					t.Fatal("cost boundary row", page, err)
				}
				fuel, err := repo.PreparedCostTransactions(ctx, kind, rowID, week, "fuel", page.Drivers[0].StatementRevision)
				if err != nil || fuel.SourceTotal != "105.00" || fuel.Carry != "0.00" || len(fuel.Transactions) != 3 {
					t.Fatal("first-live fuel evidence", fuel, err)
				}
				toll, err := repo.PreparedCostTransactions(ctx, kind, rowID, week, "toll")
				if err != nil || toll.SourceTotal != "12.25" || len(toll.Transactions) != 1 || toll.Transactions[0].Date != "2026-10-18" {
					t.Fatal("first-live posting/crossing evidence", toll, err)
				}
				edits := page.Drivers[0].Edits
				fuelPaid, tollPaid := "-102.00", "-10.00"
				edits.FuelOverride, edits.TollOverride = &fuelPaid, &tollPaid
				edits.RequestID = id(`SELECT gen_random_uuid()::text`)
				if kind == "driver" {
					_, err = repo.Save(ctx, edits, actor)
				} else {
					_, err = repo.SaveInvestorPay(ctx, edits, actor)
				}
				if err != nil {
					t.Fatal(err)
				}
				parity()
				carry, err := repo.PreparedCostTransactions(ctx, kind, rowID, next, "fuel")
				if err != nil || carry.Carry != "3.00" || len(carry.Transactions) != 1 || carry.Transactions[0].Remaining != "3.00" {
					t.Fatal("prepared partial fuel carry", carry, err)
				}
				if _, err = settlePayrollFixture(t, ctx, repo, kind, week, rowID, "", actor, "", false, true, true); err != nil {
					t.Fatal(err)
				}
				parity()
				frozenSQL := `SELECT report::text FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2`
				if kind == "investor" {
					frozenSQL = `SELECT report::text FROM investor_pay_weeks WHERE truck_id=$1 AND week_start=$2`
				}
				frozen := id(frozenSQL, rowID, week)
				exec(`UPDATE fuel_transaction_items SET total_amount_paid=101 WHERE fuel_transaction_id=(SELECT id FROM fuel_transactions WHERE relay_transaction_id='diesel')`)
				refresh()
				parity()
				if got := id(frozenSQL, rowID, week); got != frozen {
					t.Fatal("source correction rewrote frozen financial JSON")
				}
				if _, err = settlePayrollFixture(t, ctx, repo, kind, week, rowID, "", actor, "Review corrected fuel", true, false, true); err != nil {
					t.Fatal(err)
				}
				parity()
			})
		})
	}
}
