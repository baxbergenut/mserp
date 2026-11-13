package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReportsClosedWeekDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		_, err := admin.Exec(ctx, `
  INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES
  ('61000000-0000-0000-0000-000000000001','Company Driver','company driver','cpm',0.75,'2026-01-01'),
  ('61000000-0000-0000-0000-000000000002','Owner Driver','owner driver','gross_percentage',80,'2026-01-01');
  INSERT INTO investors(id,full_name,driver_id) VALUES
  ('61000000-0000-0000-0000-000000000003','Owner Driver','61000000-0000-0000-0000-000000000002');
  INSERT INTO trucks(id,unit_number,owner_id) VALUES
  ('61000000-0000-0000-0000-000000000004','COMPANY-RPT',(SELECT id FROM investors WHERE is_company)),
  ('61000000-0000-0000-0000-000000000005','OWNER-RPT','61000000-0000-0000-0000-000000000003');
  UPDATE truck_ownership_history SET start_known=false;
  INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES
  ('61000000-0000-0000-0000-000000000004','61000000-0000-0000-0000-000000000001','2026-01-01'),
  ('61000000-0000-0000-0000-000000000005','61000000-0000-0000-0000-000000000002','2026-01-01');
  DELETE FROM driver_dispatcher_assignments WHERE driver_id='61000000-0000-0000-0000-000000000001';
 INSERT INTO driver_dispatcher_assignments(driver_id,dispatcher_name,assigned_at) VALUES
  ('61000000-0000-0000-0000-000000000001','Former Dispatcher','2026-01-01');
  INSERT INTO loads(id,load_id,driver_id,status,load_pay,total_pay,total_miles,pickup_time,delivery_time,pickup_appointment_time,delivery_appointment_time,raw_payload) VALUES
  (61001,'REPORT-1','61000000-0000-0000-0000-000000000001','delivered',1000,1100,400,'2026-10-12 15:00Z','2026-10-13 15:00Z','2026-10-12 16:00Z','2026-10-13 14:00Z','{"trip":{"mile":"360","empty_mile":"40"}}'),
  (61002,'REPORT-2','61000000-0000-0000-0000-000000000002','completed',1700,1800,600,'2026-10-17 15:00Z','2026-10-18 15:00Z',NULL,NULL,'{}');
  INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate,entered_original_rate,entered_miles) VALUES
  ('61000000-0000-0000-0000-000000000001','2026-10-12','REPORT-1',61001,1000,999,300),
  ('61000000-0000-0000-0000-000000000002','2026-10-18','REPORT-2',61002,1700,1999,500),
  ('61000000-0000-0000-0000-000000000001','2026-10-14','MANUAL',NULL,100,200,100);
  INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id,driver_rate,original_rate,miles)
  VALUES('61000000-0000-0000-0000-000000000001','2026-10-12',1,'REPORT-1',61001,1000,1300,500);
  INSERT INTO gross_board_transfers(driver_id,source_date,source_slot,dispatch_date,dispatch_slot,settlement_date,dispatcher_id,dispatcher_name,truck_id)
  VALUES('61000000-0000-0000-0000-000000000002','2026-10-18',0,'2026-10-19',0,'2026-10-18','','Original Dispatcher','61000000-0000-0000-0000-000000000005');
  INSERT INTO expenses(truck_id,category,category_id,expense_date,amount,expense_type,payment_type,covered_by)
  SELECT '61000000-0000-0000-0000-000000000004'::uuid,name,id,'2026-10-13'::date,80,'Repair','Card','Company' FROM expense_settings WHERE kind='category' AND name='Maintenance'
  UNION ALL SELECT '61000000-0000-0000-0000-000000000005'::uuid,name,id,'2026-10-13'::date,50,'Truck Lease','Card','Company' FROM expense_settings WHERE kind='category' AND name='Other';
  INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES
  ('61000000-0000-0000-0000-000000000001','2026-10-12','{"id":"61000000-0000-0000-0000-000000000001","fullName":"Company Driver","payType":"cpm","loads":[{"loadRecordId":61001,"loadNumber":"REPORT-1","date":"2026-10-12","fee":"300.00"}],"edits":{"weekStart":"2026-10-12"}}'),
  ('61000000-0000-0000-0000-000000000002','2026-10-12','{"id":"61000000-0000-0000-0000-000000000002","fullName":"Owner Driver","isOwnerOperator":true,"payType":"gross_percentage","loads":[{"loadRecordId":61002,"loadNumber":"REPORT-2","date":"2026-10-18","fee":"1440.00"}],"edits":{"weekStart":"2026-10-12","expenseDeductions":[{"source":"reimburse","amount":"25.00"},{"source":"escrow","amount":"20.00"},{"source":"expense","amount":"50.00","escrowAmount":"10.00"}]},"autoCharges":[{"source":"escrow-release:receipt","amount":"5.00"}]}');
  `)
		if err != nil {
			t.Fatal(err)
		}
		repo := NewReportsRepository(pool)
		read := func(view, from, to string) ReportsResult {
			t.Helper()
			period, err := ResolveReportsPeriod(from, to, "", "", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			result, err := repo.Read(ctx, ReportsQuery{View: view, Period: period})
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
		check := func(row ReportRow, want map[string]float64) {
			t.Helper()
			for key, value := range want {
				if got, ok := row.Metrics[key]; !ok || got != value {
					t.Errorf("%s %s got %v (%v), want %v; all %v", row.Name, key, got, ok, value, row.Metrics)
				}
			}
		}
		company := read(ReportCompany, "2026-10-12", "2026-10-18")
		if len(company.Current) != 1 {
			t.Fatalf("company: %+v", company.Current)
		}
		check(company.Current[0], map[string]float64{"gross": 110000, "miles": 400, "earnedPay": 30000, "maintenance": 8000, "contribution": 72000, "completedLoads": 1, "provisionalGross": 20000})
		operator := read(ReportOperator, "2026-10-12", "2026-10-18")
		if len(operator.Current) != 1 {
			t.Fatalf("operator: %+v", operator.Current)
		}
		check(operator.Current[0], map[string]float64{"gross": 180000, "carrierFees": 36000, "carrierCosts": 5000, "contribution": 31000,
			"settlementEarnings": 144000, "reimbursements": 2500, "escrowWithheld": 2000, "escrowReturned": 500, "escrowApplied": 1000, "deductions": 5000, "netPayable": 140000})
		performance := read(ReportDriver, "2026-10-12", "2026-10-18")
		if len(performance.Current) != 2 {
			t.Fatalf("driver: %+v", performance.Current)
		}
		check(performance.Current[0], map[string]float64{"activeDays": 2, "pickupOnTime": 100, "pickupCoverage": 100, "deliveryOnTime": 0, "deliveryCoverage": 100})
		check(performance.Current[1], map[string]float64{"pickupCoverage": 0, "deliveryCoverage": 0})
		if _, ok := performance.Current[1].Metrics["pickupOnTime"]; ok {
			t.Error("unknown on-time rate displayed")
		}
		period := performance.Period
		filtered, err := repo.Read(ctx, ReportsQuery{View: ReportDriver, Period: period, Truck: "61000000-0000-0000-0000-000000000004", Dispatcher: "historical:Former Dispatcher"})
		if err != nil || len(filtered.Current) != 1 || filtered.Current[0].Name != "Company Driver" {
			t.Fatal("dated filters", filtered, err)
		}
		dispatch := read(ReportDispatcher, "2026-10-19", "2026-10-25")
		if len(dispatch.Current) != 1 || dispatch.Current[0].Name != "Original Dispatcher" {
			t.Fatalf("transferred dispatch: %+v", dispatch.Current)
		}
		check(dispatch.Current[0], map[string]float64{"gross": 180000, "truckWeeks": 1, "grossPerTruckWeek": 180000})
	})
}

func TestReportsCutoff(t *testing.T) {
	now := time.Date(2026, 11, 9, 4, 59, 59, 0, time.UTC)
	period, err := ResolveReportsPeriod("", "", "", "", now)
	if err != nil || period.DateFrom != "2026-10-26" || period.DateTo != "2026-11-01" {
		t.Fatal(period, err)
	}
	period, err = ResolveReportsPeriod("", "", "", "", now.Add(time.Second))
	if err != nil || period.DateFrom != "2026-11-02" || period.CompareFrom != "2026-10-26" {
		t.Fatal(period, err)
	}
}
