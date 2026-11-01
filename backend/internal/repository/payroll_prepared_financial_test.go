package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Run the same postings through the independent current calculator and prepared
// validation. Compare canonical money, reservations, confirmations and versions
// after every single/bulk finalize/reopen, not only the displayed payable total.
func TestPreparedPayrollFinancialParityDatabase(t *testing.T) {
	want := map[string]string{}
	for _, mode := range []string{"global", "dual", "resources"} {
		prepared := mode != "global"
		t.Run(mode, func(t *testing.T) {
			withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
				seedPayrollProjectionTest(t, ctx, pool)
				// Two trucks share an investor, with independently payable drivers.
				// Bulk operations must include both rows and roll back the first row's
				// allocations/audits if posting the second row fails.
				_, seedErr := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000001010','Second Driver','second driver','cpm',0.65,'2026-01-01');
 INSERT INTO trucks(id,unit_number,owner_id) VALUES('18200000-0000-0000-0000-000000001011','SECOND','18200000-0000-0000-0000-000000000003');
 UPDATE truck_ownership_history SET start_known=false;
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at)
 VALUES('18200000-0000-0000-0000-000000001011','18200000-0000-0000-0000-000000001010','2026-01-01');
 INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent)
 VALUES('18200000-0000-0000-0000-000000001011','18200000-0000-0000-0000-000000000003','2026-01-05',88);
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000001010','2026-10-05','SECOND-1',900,1100,300);
 UPDATE loads SET total_pay=2300,load_pay=1900,total_miles=700,raw_payload='{"trips":[
 {"id":101,"trip_id":"FIRST","driver__full_name":"Projection Driver","total_load_pay":1200,"mile":350,"empty_mile":50,"trip_begin_date":"2026-10-05","trip_end_date":"2026-10-05"},
 {"id":102,"trip_id":"SECOND","driver__full_name":"Second Driver","total_load_pay":1100,"mile":250,"empty_mile":50,"trip_begin_date":"2026-10-05","trip_end_date":"2026-10-05"}]}' WHERE id=18201;
 UPDATE gross_board_entries SET load_number='PROJECTION-1',load_record_id=18201,
 load_trip_id=CASE WHEN driver_id='18200000-0000-0000-0000-000000000001' THEN 101 ELSE 102 END;`)
				if seedErr != nil {
					t.Fatal(seedErr)
				}
				const actor = "18200000-0000-0000-0000-000000001000"
				_, err := pool.Exec(ctx, `INSERT INTO app_users(id,username,password_hash) VALUES('18200000-0000-0000-0000-000000001000','Financial parity',crypt('test-only',gen_salt('bf')));
 INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,truck_id,owner_id,covered_by,payment_type)
 SELECT v.id::uuid,s.id,s.name,v.name,'2026-10-05',v.amount,'18200000-0000-0000-0000-000000000001',
 CASE WHEN v.payer='Driver' THEN '18200000-0000-0000-0000-000000000001'::uuid END,
 CASE WHEN v.payer='Truck Owner' THEN '18200000-0000-0000-0000-000000000002'::uuid END,
 CASE WHEN v.payer='Truck Owner' THEN '18200000-0000-0000-0000-000000000003'::uuid END,v.payer,v.method
 FROM expense_settings s CROSS JOIN (VALUES
 ('18200000-0000-0000-0000-000000001001','Personal invoice',200,'Driver',''),
 ('18200000-0000-0000-0000-000000001002','Owner invoice',120,'Truck Owner',''),
 ('18200000-0000-0000-0000-000000001003','Reimbursement',25,'Company','Reimburse')) v(id,name,amount,payer,method)
 WHERE s.kind='category' AND s.name='Other';
 INSERT INTO expense_payments(expense_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000001001','2026-10-12',30);
 INSERT INTO expense_payments(expense_id,week_start,amount,investor_truck_id) VALUES('18200000-0000-0000-0000-000000001002','2026-10-12',15,'18200000-0000-0000-0000-000000000002');
 INSERT INTO expense_reimbursement_payments(expense_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000001003','2026-10-12',7);
 INSERT INTO driver_escrows(id,driver_id,driver_name,start_date,amount,opening_paid) VALUES('18200000-0000-0000-0000-000000001004','18200000-0000-0000-0000-000000000001','Projection Driver','2026-09-28',300,50);
 INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000001004','2026-10-12',50);
 INSERT INTO driver_charge_schedules(id,driver_id,kind,name,direction,start_week,eligibility,total) VALUES('18200000-0000-0000-0000-000000001005','18200000-0000-0000-0000-000000000001','installment','Advance','charge','2026-10-05','calendar',125);
 INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000001005','2026-10-05',40);
 INSERT INTO driver_charge_types(id,name,direction,amount,amounts) VALUES('18200000-0000-0000-0000-000000001006','Weekly truck fee','charge',20,ARRAY[20]::numeric[]);
 INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000001006','2026-10-05',20);
 INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES('18200000-0000-0000-0000-000000000002','2026-10-06','2026-10-06','','','','','PROJECTION','','','2026-10-06','12:00','',40,repeat('f',64));`)
				if err != nil {
					t.Fatal(err)
				}
				// Corrected debt and external settlements must feed the same final
				// reservations. Increase each remaining balance, then settle that
				// increase externally; original invoices stay unchanged and the
				// expected payroll allocations below remain 170 and 105.
				expenses := NewExpenseRepository(pool)
				for i, correction := range []struct{ id, remaining, external string }{
					{"18200000-0000-0000-0000-000000001001", "220.00", "50.00"},
					{"18200000-0000-0000-0000-000000001002", "135.00", "30.00"},
				} {
					invoice, err := expenses.GetExpense(ctx, correction.id)
					if err != nil {
						t.Fatal(err)
					}
					if err = expenses.CorrectExpenseBalance(ctx, correction.id, actor, ExpenseBalanceCorrection{ID: fmt.Sprintf("18200000-0000-0000-0000-%012d", 1700+i), Remaining: correction.remaining, Reason: "Prepared parity balance review", Version: invoice.BalanceVersion}); err != nil {
						t.Fatal(err)
					}
					invoice, err = expenses.GetExpense(ctx, correction.id)
					if err != nil {
						t.Fatal(err)
					}
					if err = expenses.RecordExternalSettlement(ctx, correction.id, actor, ExternalExpenseSettlement{ID: fmt.Sprintf("18200000-0000-0000-0000-%012d", 1710+i), Amount: correction.external, SettledOn: "2026-10-05", Reference: "Prepared parity external payment", Version: invoice.BalanceVersion}); err != nil {
						t.Fatal(err)
					}
				}
				repo := NewDriverPayRepository(pool)
				repo.ConfigurePreparedFinalization(prepared)
				repo.resourceLockAudit = prepared
				repo.resourceConcurrency = mode == "resources"
				if err = repo.EnablePayrollShadow(ctx); err != nil {
					t.Fatal(err)
				}
				if prepared {
					if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode=$1 WHERE singleton`, mode); err != nil {
						t.Fatal(err)
					}
				}
				if err = repo.EnablePayrollShadow(ctx); err != nil {
					t.Fatal(err)
				}
				week, _ := chargeWeek("2026-10-05")
				for _, date := range []string{"2026-10-05", "2026-10-12"} {
					w, _ := chargeWeek(date)
					if err = repo.RequestPayrollPreparation(ctx, w, false); err != nil {
						t.Fatal(err)
					}
					if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
						t.Fatal(err)
					}
				}
				seq := 1100
				for _, kind := range []string{"driver", "investor"} {
					for _, bulk := range []bool{false, true} {
						for _, reopen := range []bool{false, true} {
							seq++
							page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{}, true)
							if err != nil || len(page.Drivers) != 2 || page.Preparation.State != "ready" {
								t.Fatal("financial fixture statement", kind, err)
							}
							for _, row := range page.Drivers {
								trip, miles, gross := 102, int64(30000), int64(110000)
								if row.ID == projectionDriver || row.ID == projectionTruck {
									trip, miles, gross = 101, 40000, 120000
								}
								if len(row.Loads) != 1 || row.Loads[0].LoadTripID == nil || *row.Loads[0].LoadTripID != trip || row.Loads[0].TripReview {
									t.Fatal("prepared financial fixture lost selected split identity", kind, row.ID, row.Loads)
								}
								gotMiles, mileErr := chargeCents(row.Loads[0].TotalMiles)
								gotGross, grossErr := chargeCents(row.Loads[0].OriginalRate)
								if mileErr != nil || grossErr != nil || gotMiles != miles || gotGross != gross {
									t.Fatal("prepared financial fixture changed split amounts", kind, row.ID, row.Loads)
								}
							}
							id, version := projectionDriver, ""
							if kind == "investor" {
								id = projectionTruck
							}
							for _, row := range page.Drivers {
								if row.ID == id {
									version = row.StatementRevision
								}
							}
							if bulk {
								id, version = "", page.Preparation.Generation
							}
							command := PayrollSettlementCommand{RequestID: fmt.Sprintf("18200000-0000-0000-0000-%012d", seq), ExpectedStatementRevision: version}
							settle := func() error {
								if kind == "driver" {
									_, e := repo.Settle(WithPayrollReceipt(ctx), week, id, page.Revision, actor, "Financial parity reopen", reopen, !reopen, command)
									return e
								}
								_, e := repo.SettleInvestor(WithPayrollReceipt(ctx), week, id, page.Revision, actor, "Financial parity reopen", reopen, !reopen, command)
								return e
							}
							if bulk && prepared {
								table, field, failedID := "payroll_settlements", "driver_id", "18200000-0000-0000-0000-000000001010"
								if kind == "investor" {
									table, field, failedID = "investor_pay_weeks", "truck_id", "18200000-0000-0000-0000-000000001011"
								}
								var before, after string
								if err = pool.QueryRow(ctx, payrollFinancialRollbackSQL).Scan(&before); err != nil {
									t.Fatal(err)
								}
								if _, err = admin.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION fail_bulk_payroll() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.%s='%s'::uuid THEN RAISE EXCEPTION 'test second statement failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER fail_bulk_payroll BEFORE INSERT OR UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION fail_bulk_payroll()`, field, failedID, table)); err != nil {
									t.Fatal(err)
								}
								if err = settle(); err == nil {
									t.Fatal("bulk operation ignored second-statement failure")
								}
								if _, err = admin.Exec(ctx, fmt.Sprintf(`DROP TRIGGER fail_bulk_payroll ON %s; DROP FUNCTION fail_bulk_payroll()`, table)); err != nil {
									t.Fatal(err)
								}
								if err = pool.QueryRow(ctx, payrollFinancialRollbackSQL).Scan(&after); err != nil || before != after {
									t.Fatal("failed bulk left financial, audit, projection or receipt changes", err, before, after)
								}
							}
							err = settle()
							if err != nil {
								t.Fatal(kind, bulk, reopen, err)
							}
							var actual string
							if err = pool.QueryRow(ctx, payrollFinancialParitySQL).Scan(&actual); err != nil {
								t.Fatal(err)
							}
							parts := strings.Split(t.Name(), "/")
							key := fmt.Sprintf("%s/%s/%t/%t", parts[len(parts)-1], kind, bulk, reopen)
							if !prepared {
								want[key] = actual
							} else if actual != want[key] {
								t.Fatalf("prepared posting differs %s\nwant %s\ngot %s", key, want[key], actual)
							}
							if !reopen && kind == "driver" && !strings.Contains(actual, `"confirmed": true`) {
								t.Fatal("fixture did not exercise installment confirmation")
							}
							if !reopen {
								var posted bool
								check := `SELECT
 EXISTS(SELECT 1 FROM expense_payments WHERE expense_id='18200000-0000-0000-0000-000000001001' AND week_start='2026-10-05' AND amount=170) AND
 EXISTS(SELECT 1 FROM expense_reimbursement_payments WHERE expense_id='18200000-0000-0000-0000-000000001003' AND week_start='2026-10-05' AND amount=18) AND
 EXISTS(SELECT 1 FROM driver_escrow_payments WHERE escrow_id='18200000-0000-0000-0000-000000001004' AND week_start='2026-10-05' AND amount=200)`
								if kind == "investor" {
									check = `SELECT
 EXISTS(SELECT 1 FROM expense_payments WHERE expense_id='18200000-0000-0000-0000-000000001002' AND week_start='2026-10-05' AND amount=105) AND
 EXISTS(SELECT 1 FROM pay_source_collections WHERE source='investor_cost:18200000-0000-0000-0000-000000000002:toll' AND week_start='2026-10-05' AND amount=-40) AND
 EXISTS(SELECT 1 FROM pay_source_collections WHERE source='truck_charge:18200000-0000-0000-0000-000000000002:18200000-0000-0000-0000-000000001006' AND week_start='2026-10-05' AND amount=-20)`
								}
								if err = pool.QueryRow(ctx, check).Scan(&posted); err != nil || !posted {
									t.Fatal("expected financial postings or future reservation limits missing", kind, posted, err)
								}
							}
						}
					}
				}
			})
		})
	}
}

const payrollFinancialParitySQL = `SELECT jsonb_build_object(
 'balance_corrections',(SELECT jsonb_agg(to_jsonb(c)-'created_at' ORDER BY id) FROM expense_balance_corrections c),
 'external_settlements',(SELECT jsonb_agg(to_jsonb(c)-'created_at' ORDER BY id) FROM expense_external_settlements c),
 'expenses',(SELECT jsonb_agg(to_jsonb(p)-'updated_at' ORDER BY expense_id,week_start) FROM expense_payments p),
 'reimbursements',(SELECT jsonb_agg(to_jsonb(p)-'updated_at' ORDER BY expense_id,week_start) FROM expense_reimbursement_payments p),
 'escrow',(SELECT jsonb_agg(to_jsonb(p)-'updated_at' ORDER BY escrow_id,week_start) FROM driver_escrow_payments p),
 'installments',(SELECT jsonb_agg((to_jsonb(c)-'confirmed_at')||jsonb_build_object('confirmed',confirmed_at IS NOT NULL) ORDER BY schedule_id,week_start) FROM driver_charge_occurrences c),
 'schedule_versions',(SELECT jsonb_agg(jsonb_build_object('id',id,'version',version) ORDER BY id) FROM driver_charge_schedules),
 'reopened_charges',(SELECT jsonb_agg(jsonb_build_object('schedule',schedule_id,'details',details) ORDER BY schedule_id,details::text) FROM driver_charge_events WHERE action='payroll_reopened'),
 'reopened_drivers',(SELECT jsonb_agg(jsonb_build_object('driver',driver_id,'week',week_start,'version',version,'reason',reason) ORDER BY driver_id,week_start,version) FROM payroll_settlement_events WHERE action='reopened'),
 'reopened_investors',(SELECT jsonb_agg(jsonb_build_object('truck',truck_id,'week',week_start,'reason',details->'reason','override',details->'override') ORDER BY truck_id,week_start) FROM truck_settlement_events WHERE action='reopened'),
 'costs',(SELECT jsonb_agg(to_jsonb(c)-'updated_at' ORDER BY driver_id,week_start) FROM driver_pay_cost_collections c),
 'sources',(SELECT jsonb_agg(to_jsonb(c)-ARRAY['updated_at','statement'] ORDER BY source,owner_id,week_start) FROM pay_source_collections c),
 'driver_settlements',(SELECT jsonb_agg(jsonb_build_object('driver',driver_id,'week',week_start,'version',version,'finalized',finalized,'confirmed',confirmed_schedules) ORDER BY driver_id,week_start) FROM payroll_settlements),
 'investor_settlements',(SELECT jsonb_agg(jsonb_build_object('truck',truck_id,'week',week_start,'version',version,'finalized',finalized) ORDER BY truck_id,week_start) FROM investor_pay_weeks)
 )::text`

const payrollFinancialRollbackSQL = `SELECT jsonb_build_object(
 'financial',(` + payrollFinancialParitySQL + `),
 'driver_audit',(SELECT count(*) FROM payroll_settlement_events),
 'investor_audit',(SELECT count(*) FROM truck_settlement_events),
 'charge_audit',(SELECT count(*) FROM driver_charge_events),
 'receipts',(SELECT count(*) FROM payroll_command_receipts),
 'changes',(SELECT count(*) FROM payroll_projection_changes),
 'versions',(SELECT count(*) FROM payroll_statement_versions),
 'state',(SELECT jsonb_agg(to_jsonb(s) ORDER BY kind,statement_id,week_start) FROM payroll_statement_state s)
 )::text`
