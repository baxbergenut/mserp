package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPaySourceCollectionsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		id := func(q string, args ...any) string {
			t.Helper()
			var v string
			if err := pool.QueryRow(ctx, q, args...).Scan(&v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		pr := NewDriverPayRepository(pool)
		cr := NewDriverChargeRepository(pool)
		for _, kind := range []string{"driver", "investor"} {
			t.Run(kind, func(t *testing.T) {
				driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'gross_percentage',88) RETURNING id::text`, kind+" carry owner")
				owner := id(`INSERT INTO investors(full_name,driver_id) VALUES($1,$2) RETURNING id::text`, kind+" carry owner", driver)
				truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES($1,$2) RETURNING id::text`, kind+"-CARRY", owner)
				exec(`UPDATE truck_ownership_history SET start_known=false WHERE truck_id=$1`, truck)
				if kind == "driver" {
					exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01')`, truck, driver)
				} else {
					if err := cr.SaveTruckTerm(ctx, TruckTerm{TruckID: truck, OwnerID: owner, WeekStart: "2026-09-28", SharePercent: "88"}, actor); err != nil {
						t.Fatal(err)
					}
				}
				typ, err := cr.SaveType(ctx, ChargeType{Name: kind + " recurring", Direction: "charge", Amounts: []string{"100"}, Eligibility: "calendar"}, actor)
				if err != nil {
					t.Fatal(err)
				}
				if err = cr.SaveTruckCharge(ctx, TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: "2026-09-28", Amount: "100", Included: true, TypeVersion: typ.Version}, actor); err != nil {
					t.Fatal(err)
				}
				fee := "truck_charge:" + truck + ":" + typ.ID
				read := func(week string) DriverPayDriver {
					t.Helper()
					w, _ := chargeWeek(week)
					var r DriverPayWeek
					var e error
					if kind == "driver" {
						r, e = pr.Get(ctx, w)
					} else {
						r, e = pr.InvestorPay(ctx, w, truck)
					}
					if e != nil {
						t.Fatal(e)
					}
					for _, d := range r.Drivers {
						if d.ID == driver && kind == "driver" || d.ID == truck && kind == "investor" {
							return d
						}
					}
					t.Fatalf("missing %s %s", kind, week)
					return DriverPayDriver{}
				}
				save := func(e DriverPayEdits) (DriverPayEdits, error) {
					if kind == "driver" {
						return pr.Save(ctx, e, actor)
					}
					return pr.SaveInvestorPay(ctx, e, actor)
				}
				clone := func(e DriverPayEdits) DriverPayEdits {
					b, _ := json.Marshal(e)
					var c DriverPayEdits
					_ = json.Unmarshal(b, &c)
					return c
				}
				source := func(d DriverPayDriver) PaySourceCharge {
					t.Helper()
					for _, r := range d.Edits.SourceCharges {
						if r.Source == fee {
							return r
						}
					}
					t.Fatal("missing fee")
					return PaySourceCharge{}
				}
				editFee := func(e *DriverPayEdits, amount string) {
					for i := range e.SourceCharges {
						if e.SourceCharges[i].Source == fee {
							e.SourceCharges[i].Amount = amount
						}
					}
				}
				before := id(`SELECT count(*)::text FROM pay_source_collections`)
				first := read("2026-09-28")
				_ = read("2026-10-05")
				if id(`SELECT count(*)::text FROM pay_source_collections`) != before {
					t.Fatal("GET collected money")
				}
				for _, amount := range []string{"1.00", "-100.01"} {
					e := clone(first.Edits)
					editFee(&e, amount)
					for i := range e.SourceCharges {
						e.SourceCharges[i].Due = "-999999.00"
					}
					if _, err = save(e); err == nil {
						t.Fatal("accepted bonus/overcollection", amount)
					}
				}
				e := clone(first.Edits)
				editFee(&e, "-25.00")
				if _, err = save(e); err != nil {
					t.Fatal(err)
				}
				next := read("2026-10-05")
				if r := source(next); r.Carry != "-75.00" || r.Amount != "-175.00" {
					t.Fatalf("partial carry: %+v", r)
				}
				stale := clone(next.Edits)
				first = read("2026-09-28")
				e = clone(first.Edits)
				editFee(&e, "-40.00")
				if _, err = save(e); err != nil {
					t.Fatal(err)
				}
				if _, err = save(stale); err != ErrDriverPayConflict {
					t.Fatal("stale cross-week save accepted", err)
				}
				next = read("2026-10-05")
				e = clone(next.Edits)
				editFee(&e, "0.00")
				if _, err = save(e); err != nil {
					t.Fatal(err)
				}
				if err = cr.SaveTruckCharge(ctx, TruckChargePhase{TruckID: truck, TypeID: typ.ID, WeekStart: "2026-10-12", Amount: "100", Included: false, TypeVersion: typ.Version}, actor); err != nil {
					t.Fatal(err)
				}
				third := read("2026-10-12")
				if r := source(third); r.Base != "0.00" || r.Due != "-160.00" {
					t.Fatalf("excluded fee lost carry: %+v", r)
				}
				if _, err = save(clone(third.Edits)); err != nil {
					t.Fatal(err)
				}
				first = read("2026-09-28")
				e = clone(first.Edits)
				editFee(&e, "-50.00")
				if _, err = save(e); err == nil {
					t.Fatal("earlier change invalidated later collection")
				}
				// The same source remains truck-linked when the owner hires a driver.
				if kind == "driver" {
					if err = cr.SaveTruckTerm(ctx, TruckTerm{TruckID: truck, OwnerID: owner, WeekStart: "2026-10-05", SharePercent: "88"}, actor); err != nil {
						t.Fatal(err)
					}
					exec(`UPDATE truck_driver_assignments SET unassigned_at='2026-10-05 04:00+00' WHERE truck_id=$1`, truck)
					w, _ := chargeWeek("2026-10-05")
					r, e := pr.InvestorPay(ctx, w, truck)
					if e != nil {
						t.Fatal(e)
					}
					for _, d := range r.Drivers {
						if d.ID == truck {
							for _, s := range d.Edits.SourceCharges {
								if s.Source == fee && s.Carry != "-60.00" {
									t.Fatal("routing lost carry", s)
								}
							}
						}
					}
				}
			})
		}
		// Investor operating costs use the same zero/partial/carry rules, including
		// pre-migration overrides whose source must remain recoverable.
		t.Run("investor_costs", func(t *testing.T) {
			driver := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Cost operator','cost operator','cpm',0.5) RETURNING id::text`)
			owner := id(`INSERT INTO investors(full_name) VALUES('Cost investor') RETURNING id::text`)
			truck := id(`INSERT INTO trucks(unit_number,owner_id) VALUES('COST-CARRY',$1) RETURNING id::text`, owner)
			exec(`UPDATE truck_ownership_history SET start_known=false WHERE truck_id=$1`, truck)
			if err := cr.SaveTruckTerm(ctx, TruckTerm{TruckID: truck, OwnerID: owner, WeekStart: "2026-09-28", SharePercent: "88"}, actor); err != nil {
				t.Fatal(err)
			}
			fuel := id(`INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,prompts,raw_payload) VALUES('production','cost-carry',$1,'relay-test','2026-09-28 12:00+00',100,100,0,false,'USD','','','','','','','','','','',0,0,'America/New_York','[{"label":"Truck #","value":"COST-CARRY"}]','{}') RETURNING id::text`, driver)
			exec(`INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid) VALUES($1,0,'fuel','diesel',100)`, fuel)
			exec(`INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint) VALUES($1,'2026-09-29','2026-09-29','','','','','COST-CARRY','','','2026-09-29','12:00','',40,repeat('c',64))`, truck)
			// An old saved Fuel override must bring its 40 remainder forward.
			exec(`INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits) VALUES($1,$2,'2026-09-28',jsonb_build_object('driverId',$1::uuid::text,'weekStart','2026-09-28','fuelOverride','-60.00','tollOverride','-10.00','comments','{}'::jsonb,'adjustments','[]'::jsonb))`, truck, owner)
			read := func(week string) (DriverPayWeek, DriverPayDriver) {
				t.Helper()
				w, _ := chargeWeek(week)
				r, err := pr.InvestorPay(ctx, w, truck)
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range r.Drivers {
					if d.ID == truck {
						return r, d
					}
				}
				t.Fatal("missing investor")
				return r, DriverPayDriver{}
			}
			_, prior := read("2026-09-28")
			changedFuel := "-50.00"
			prior.Edits.FuelOverride = &changedFuel
			if _, err := pr.SaveInvestorPay(ctx, prior.Edits, actor); err != nil {
				t.Fatal(err)
			}
			_, mixed := read("2026-10-05")
			if mixed.Edits.Costs.FuelDue != "50.00" || mixed.Edits.Costs.TollDue != "30.00" {
				t.Fatal("migrating Fuel lost the separate legacy Toll balance", mixed.Edits.Costs)
			}
			_, prior = read("2026-09-28")
			originalFuel := "-60.00"
			prior.Edits.FuelOverride = &originalFuel
			if _, err := pr.SaveInvestorPay(ctx, prior.Edits, actor); err != nil {
				t.Fatal(err)
			}
			_, d := read("2026-10-05")
			if d.Edits.Costs.FuelDue != "40.00" || d.Edits.Costs.TollDue != "30.00" {
				t.Fatal("legacy carry lost", d.Edits.Costs)
			}
			for _, amount := range []string{"1.00", "-40.01"} {
				e := d.Edits
				e.FuelOverride = &amount
				if _, err := pr.SaveInvestorPay(ctx, e, actor); err == nil {
					t.Fatal("invalid investor fuel", amount)
				}
			}
			partial, zero := "-15.00", "0.00"
			e := d.Edits
			e.FuelOverride = &partial
			e.TollOverride = &zero
			if _, err := pr.SaveInvestorPay(ctx, e, actor); err != nil {
				t.Fatal(err)
			}
			r, d := read("2026-10-12")
			if d.Edits.Costs.FuelDue != "25.00" || d.Edits.Costs.TollDue != "30.00" {
				t.Fatal("repeated investor carry lost", d.Edits.Costs)
			}
			if payPageCosts(d) != -5500 {
				t.Fatal("summary ignores carry", payPageCosts(d))
			}
			w, _ := chargeWeek("2026-10-12")
			if _, err := pr.SettleInvestor(ctx, w, truck, r.Revision, actor, "", false); err != nil {
				t.Fatal(err)
			}
			r, d = read("2026-10-12")
			if d.Settlement == nil || !d.Settlement.Finalized || d.Edits.Costs.FuelDue != "25.00" {
				t.Fatal("frozen totals lost")
			}
			if _, err := pr.SaveInvestorPay(ctx, d.Edits, actor); err == nil {
				t.Fatal("edited finalized costs")
			}
			if _, err := pr.SettleInvestor(ctx, w, truck, r.Revision, actor, "check carry", true); err != nil {
				t.Fatal(err)
			}
			_, d = read("2026-10-19")
			if d.Edits.Costs.FuelDue != "0.00" || d.Edits.Costs.TollDue != "0.00" {
				t.Fatal("reopening recollected costs", d.Edits.Costs)
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pay_source_collections WHERE truck_id=$1 AND week_start='2026-09-28'`, truck).Scan(&count); err != nil || count != 2 {
				t.Fatal("legacy principal not pinned", count, err)
			}
			// Credits keep their source direction too.
			exec(`UPDATE tolls SET amount=-20,posting_date='2026-10-19' WHERE truck_id=$1`, truck)
			_, d = read("2026-10-19")
			bad := "-1.00"
			d.Edits.TollOverride = &bad
			if _, err := pr.SaveInvestorPay(ctx, d.Edits, actor); err == nil || !strings.Contains(err.Error(), "between zero") {
				t.Fatal("credit flipped to charge", err)
			}
		})
	})
}
