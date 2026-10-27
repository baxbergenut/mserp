package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func TestDashboardEarnings(t *testing.T) {
	id := 1
	load := DriverPayLoad{LoadRecordID: &id, OriginalRate: "1000.00", Fee: "900.00"}
	driver := DriverPayDriver{Loads: []DriverPayLoad{load}, FuelTotal: "200.00", TollTotal: "20.00",
		Edits: DriverPayEdits{Adjustments: []DriverPayAdjustment{{Kind: "addition", Amount: "10.00"}, {Kind: "deduction", Amount: "5.00"}},
			SourceCharges: []PaySourceCharge{{Amount: "-200.00"}}, GeneratedCharges: []ChargeOccurrence{{Amount: "-100.00"}}},
		AutoCharges: []PayAutoCharge{{Source: "escrow_release:1", Amount: "500.00"}}, Layovers: []DriverPayLayover{{Date: "2026-10-19", Amount: "25.00"}}}
	report := DriverPayWeek{Drivers: []DriverPayDriver{driver}}
	gross := map[string]string{"1": "1000.00"}
	covered := map[string]bool{}
	n, review := dashboardEarnings(report, false, gross, covered)
	if n != 93000 || review != 0 || !covered["1"] {
		t.Fatalf("earned pay must exclude cost recoveries/carry/escrow: %d %d", n, review)
	}
	investor := DriverPayDriver{Loads: []DriverPayLoad{{LoadRecordID: &id, OriginalRate: "1000", Fee: "880"}}, AutoCharges: []PayAutoCharge{{Source: "driver:1", Amount: "-250"}, {Source: "truck_charge:1", Amount: "-100"}}}
	n, review = dashboardEarnings(DriverPayWeek{Drivers: []DriverPayDriver{investor}}, true, gross, map[string]bool{})
	if n != 63000 || review != 0 {
		t.Fatalf("investor share must subtract hired labor once: %d %d", n, review)
	}
	for _, bad := range []DriverPayLoad{{LoadRecordID: &id, OriginalRate: "999", Fee: "900"}, {LoadRecordID: &id, OriginalRate: "1000", Fee: ""}, {OriginalRate: "1000", Fee: "900"}} {
		n, review = dashboardEarnings(DriverPayWeek{Drivers: []DriverPayDriver{{Loads: []DriverPayLoad{bad}}}}, false, gross, map[string]bool{})
		if n != 0 || review == 0 {
			t.Fatal("unknown/stale earnings treated as complete")
		}
	}
}

// Runs in TestCompanyDashboardSchema's isolated full-schema database.
func testDashboardWaterfall(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES
 ('10000000-0000-0000-0000-000000000001','Flow company','flow company','gross_percentage',25),
 ('10000000-0000-0000-0000-000000000002','Flow owner','flow owner','gross_percentage',90),
 ('10000000-0000-0000-0000-000000000003','Flow hired','flow hired','gross_percentage',25);
 INSERT INTO investors(id,full_name,driver_id) VALUES('20000000-0000-0000-0000-000000000002','Flow owner','10000000-0000-0000-0000-000000000002'),('20000000-0000-0000-0000-000000000003','Flow investor',null);
 INSERT INTO trucks(id,unit_number,owner_id) VALUES
 ('30000000-0000-0000-0000-000000000001','FLOW-1','00000000-0000-0000-0000-000000000001'),
 ('30000000-0000-0000-0000-000000000002','FLOW-2','20000000-0000-0000-0000-000000000002'),
 ('30000000-0000-0000-0000-000000000003','FLOW-3','20000000-0000-0000-0000-000000000003');
 UPDATE truck_ownership_history SET assigned_at='2026-01-01',start_known=true WHERE truck_unit LIKE 'FLOW-%';
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at)
 SELECT ('30000000-0000-0000-0000-00000000000'||n)::uuid,('10000000-0000-0000-0000-00000000000'||n)::uuid,'2026-01-01' FROM generate_series(1,3) n;
 INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES('30000000-0000-0000-0000-000000000003','20000000-0000-0000-0000-000000000003','2026-01-05',88);
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,pickup_time,raw_payload)
 SELECT 900+n,'FLOW-'||n,'delivered',1000,1000,1000,'FLOW-'||n,'2026-10-19 12:00+00','{}' FROM generate_series(1,3) n;
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate)
 SELECT ('10000000-0000-0000-0000-00000000000'||n)::uuid,'2026-10-19','FLOW-'||n,900+n,1000 FROM generate_series(1,3) n;`)
	exec(`WITH fixtures(label,unit,amount,env,purchase_day,kind,category) AS (VALUES
 ('company','FLOW-1',100.00,'production','2026-10-19','fuel','diesel'),
 ('def','FLOW-1',10.00,'production','2026-10-19','fuel','def'),
 ('refund','FLOW-1',-5.00,'production','2026-10-20','fuel','diesel'),
 ('owner','FLOW-2',200.00,'production','2026-10-19','fuel','diesel'),
 ('investor','FLOW-3',300.00,'production','2026-10-19','fuel','diesel'),
 ('staging','FLOW-1',999.00,'staging','2026-10-19','fuel','diesel'),
 ('outside','FLOW-1',999.00,'production','2026-10-18','fuel','diesel'),
 ('product','FLOW-1',999.00,'production','2026-10-19','product','other')),
 inserted AS (INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,raw_payload,prompts)
 SELECT env,label,('10000000-0000-0000-0000-00000000000'||right(unit,1))::uuid,'identity',(purchase_day||' 12:00+00')::timestamptz,amount,amount,0,false,'USD','','','','','','','','','','',0,0,'US/Eastern','{}',jsonb_build_array(jsonb_build_object('label','Truck #','value',unit)) FROM fixtures RETURNING id,relay_transaction_id)
 INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid) SELECT i.id,0,f.kind,f.category,f.amount FROM inserted i JOIN fixtures f ON i.relay_transaction_id=f.label;
 INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint,prepass_environment)
 SELECT ('30000000-0000-0000-0000-00000000000'||n)::uuid,'2026-10-20','2026-10-20','','','','','','','','2026-10-19','12:00','',n*10,md5(n::text)||md5(n::text),'production' FROM generate_series(1,3) n;`)
	p, _ := ResolveDashboardPeriod("week", "2026-10-19", "", time.Now())
	repo := NewDashboardRepository(pool)
	got, err := repo.Waterfall(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Gross != "3000.00" || got.Fuel != "105.00" || got.Tolls != "10.00" || got.DriverPay != "1400.00" || got.InvestorPay != "630.00" || got.Profit == nil || *got.Profit != "855.00" {
		t.Fatalf("whole-fleet margin: %+v", got)
	}
	// Imported baselines establish an owner even when the historical start is
	// unknown and the record was created after the reporting week. No payroll
	// finalization is required for these source costs to appear.
	exec(`UPDATE truck_ownership_history SET assigned_at='2026-10-26',start_known=false WHERE truck_unit LIKE 'FLOW-%';`)
	got, err = repo.Waterfall(ctx, p)
	if err != nil || got.Fuel != "105.00" || got.Tolls != "10.00" || got.Profit == nil || *got.Profit != "855.00" || len(got.Review) != 0 {
		t.Fatalf("baseline ownership must count before finalization: %+v %v", got, err)
	}
	exec(`UPDATE truck_ownership_history SET assigned_at='2026-01-01' WHERE truck_unit LIKE 'FLOW-%';`)
	// Ownership changes are resolved on transaction day, not from today's owner.
	exec(`UPDATE truck_ownership_history SET unassigned_at='2026-10-20 00:00-04' WHERE truck_unit='FLOW-1';
 INSERT INTO truck_ownership_history(truck_id,truck_unit,owner_id,assigned_at,start_known) VALUES('30000000-0000-0000-0000-000000000001','FLOW-1','20000000-0000-0000-0000-000000000002','2026-10-20 00:00-04',true);`)
	rows, err := pool.Query(ctx, dashboardCompanyCostsSQL, p.DateFrom, p.DateTo)
	if err != nil {
		t.Fatal(err)
	}
	costs := map[string]string{}
	for rows.Next() {
		var kind, amount string
		var review int
		if err = rows.Scan(&kind, &amount, &review); err != nil {
			t.Fatal(err)
		}
		if review != 0 {
			t.Fatal("known dated owner unresolved")
		}
		costs[kind] = amount
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if costs["fuel"] != "110.00" || costs["tolls"] != "10.00" {
		t.Fatalf("transaction-day ownership: %v", costs)
	}
	// Accepting a baseline must not silently choose between conflicting owners.
	exec(`INSERT INTO truck_ownership_history(id,truck_id,truck_unit,owner_id,assigned_at,unassigned_at,start_known) VALUES('40000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000001','FLOW-1','20000000-0000-0000-0000-000000000002','2026-10-18 00:00-04','2026-10-20 00:00-04',true);`)
	got, err = repo.Waterfall(ctx, p)
	fuel, fuelErr := chargeCents(got.Fuel)
	tolls, tollsErr := chargeCents(got.Tolls)
	if err != nil || fuelErr != nil || tollsErr != nil || fuel != 0 || tolls != 0 || got.Profit != nil || len(got.Review) == 0 {
		t.Fatalf("conflicting ownership must stay unresolved: %+v %v", got, err)
	}
	tollReview, err := repo.DashboardCostReview(ctx, p, "tolls", Pagination{Page: 1, PageSize: 25})
	if err != nil || tollReview.Total != 1 || len(tollReview.Items) != 1 || tollReview.Items[0].Reason != "Conflicting owners on the transaction date" || tollReview.Items[0].Date != "2026-10-20" || tollReview.Items[0].OwnershipDate == nil || *tollReview.Items[0].OwnershipDate != "2026-10-19" {
		t.Fatalf("toll review attribution: %+v %v", tollReview, err)
	}
	exec(`DELETE FROM truck_ownership_history WHERE id='40000000-0000-0000-0000-000000000001';`)
	exec(`UPDATE fuel_transactions SET prompts='[]' WHERE relay_transaction_id='company';`)
	got, err = repo.Waterfall(ctx, p)
	if err != nil || got.Profit != nil || len(got.Review) == 0 {
		t.Fatalf("unresolved ownership must block profit: %+v %v", got, err)
	}
	fuelReview, err := repo.DashboardCostReview(ctx, p, "fuel", Pagination{Page: 1, PageSize: 25})
	if err != nil || fuelReview.Total != 1 || len(fuelReview.Items) != 1 || fuelReview.Items[0].Reference != "company" || fuelReview.Items[0].TruckID != "" || fuelReview.Items[0].Reason != "Reported truck number is missing or not uniquely linked" {
		t.Fatalf("missing truck review: %+v %v", fuelReview, err)
	}
	if fuelReview.Items[0].DriverName != "Flow company" || len(fuelReview.Items[0].AssignedUnits) != 1 || fuelReview.Items[0].AssignedUnits[0] != "FLOW-1" {
		t.Fatalf("linked driver and dated assignment context: %+v", fuelReview.Items[0])
	}
	exec(`UPDATE fuel_transactions SET prompts='[]' WHERE relay_transaction_id='def';`)
	first, err := repo.DashboardCostReview(ctx, p, "fuel", Pagination{Page: 1, PageSize: 1})
	if err != nil || first.Total != 2 || first.TotalPages != 2 || len(first.Items) != 1 {
		t.Fatalf("review pagination: %+v %v", first, err)
	}
	second, err := repo.DashboardCostReview(ctx, p, "fuel", Pagination{Page: 2, PageSize: 1})
	if err != nil || second.Total != 2 || len(second.Items) != 1 || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("review second page: %+v %v", second, err)
	}
	exec(`DELETE FROM truck_ownership_history WHERE truck_unit='FLOW-3';`)
	tollReview, err = repo.DashboardCostReview(ctx, p, "tolls", Pagination{Page: 1, PageSize: 25})
	if err != nil || tollReview.Total != 1 || tollReview.Items[0].TruckUnit != "FLOW-3" || tollReview.Items[0].Reason != "No ownership recorded for the transaction date" {
		t.Fatalf("missing dated ownership review: %+v %v", tollReview, err)
	}
}
