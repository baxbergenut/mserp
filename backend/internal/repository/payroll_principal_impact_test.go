package repository

import (
	"context"
	"encoding/json"
	"mserp/internal/relay"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollPrincipalImpactDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const independent = "18200000-0000-0000-0000-000000001501"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES
 ('18200000-0000-0000-0000-000000001500','New payer','new payer','cpm',0.65,'2026-01-01'),
 ('18200000-0000-0000-0000-000000001501','Independent payer','independent payer','cpm',0.65,'2026-01-01'),
 ('18200000-0000-0000-0000-000000001506','Team payer','team payer','cpm',0.65,'2026-01-01');
 INSERT INTO investors(id,full_name) VALUES('18200000-0000-0000-0000-000000001507','New dated owner');
 INSERT INTO trucks(id,unit_number,owner_id) VALUES('18200000-0000-0000-0000-000000001509','OTHER-IMPACT','18200000-0000-0000-0000-000000001507');
 INSERT INTO driver_charge_types(id,name,direction,amount,amounts) VALUES('18200000-0000-0000-0000-000000001508','Relationship fee','charge',20,ARRAY[20]::numeric[]);
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000001501','2026-10-05','INDEPENDENT',200,300,120)`); err != nil {
			t.Fatal(err)
		}
		fuelWeek, _ := chargeWeek("2026-10-05")
		fuel := NewFuelRepository(pool)
		purchase := relayTestPurchase(t, "principal-fuel", "principal-relay", fuelWeek.Add(12*time.Hour), relay.Transaction{})
		if err := fuel.UpsertDay(ctx, "production", fuelWeek, []relay.Transaction{purchase}, fuelWeek, true); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE fuel_transactions SET driver_id=$1,prompts='[{"label":"Truck #","value":"PROJECTION"}]' WHERE relay_transaction_id='principal-fuel'`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		weeks := []time.Time{}
		versions := map[string]string{}
		for _, date := range []string{"2026-10-05", "2026-10-12", "2026-10-19"} {
			week, _ := chargeWeek(date)
			weeks = append(weeks, week)
			if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
			page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
			if err != nil {
				t.Fatal(err)
			}
			versions[date] = page.Statements[independent].Revision
		}
		steps := []struct {
			name, sql string
			narrow    bool
		}{
			{"changed driver name", `UPDATE drivers SET full_name='Corrected Projection',normalized_name='corrected projection' WHERE id='18200000-0000-0000-0000-000000000001'`, true},
			{"changed driver rate", `UPDATE drivers SET pay_rate=0.80 WHERE id='18200000-0000-0000-0000-000000000001'`, true},
			{"changed investor name", `UPDATE investors SET full_name='Corrected Investor' WHERE id='18200000-0000-0000-0000-000000000003'`, true},
			{"new invoice", `INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT '18200000-0000-0000-0000-000000001502',id,name,'Principal test','2026-10-05',200,'18200000-0000-0000-0000-000000000001','Driver'
 FROM expense_settings WHERE kind='category' AND name='Other'`, true},
			{"changed amount", `UPDATE expenses SET amount=180 WHERE id='18200000-0000-0000-0000-000000001502'`, true},
			{"future reservation", `INSERT INTO expense_payments(expense_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000001502','2026-10-19',30)`, true},
			{"reassigned principal", `DELETE FROM expense_payments WHERE expense_id='18200000-0000-0000-0000-000000001502';
 UPDATE expenses SET driver_id='18200000-0000-0000-0000-000000001500',charge_driver_id='18200000-0000-0000-0000-000000001500' WHERE id='18200000-0000-0000-0000-000000001502'`, true},
			{"new reservation", `INSERT INTO expense_payments(expense_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000001502','2026-10-19',25)`, true},
			{"deleted principal and allocation", `DELETE FROM expense_payments WHERE expense_id='18200000-0000-0000-0000-000000001502'; DELETE FROM expenses WHERE id='18200000-0000-0000-0000-000000001502'`, true},
			{"new escrow principal", `INSERT INTO driver_escrows(id,driver_id,driver_name,start_date,amount,opening_paid)
 VALUES('18200000-0000-0000-0000-000000001504','18200000-0000-0000-0000-000000001500','New payer','2026-10-05',300,50)`, true},
			{"future escrow reservation", `INSERT INTO driver_escrow_payments(escrow_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000001504','2026-10-19',40)`, true},
			{"deleted escrow and allocation", `DELETE FROM driver_escrow_payments WHERE escrow_id='18200000-0000-0000-0000-000000001504'; DELETE FROM driver_escrows WHERE id='18200000-0000-0000-0000-000000001504'`, true},
			{"new installment principal", `INSERT INTO driver_charge_schedules(id,driver_id,kind,name,direction,start_week,eligibility,total)
 VALUES('18200000-0000-0000-0000-000000001505','18200000-0000-0000-0000-000000001500','installment','Principal advance','charge','2026-10-05','calendar',125);
 INSERT INTO driver_charge_phases(schedule_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000001505','2026-10-05',40)`, true},
			{"changed installment phase", `UPDATE driver_charge_phases SET amount=35 WHERE schedule_id='18200000-0000-0000-0000-000000001505'`, true},
			{"deleted installment principal", `DELETE FROM driver_charge_phases WHERE schedule_id='18200000-0000-0000-0000-000000001505'; DELETE FROM driver_charge_schedules WHERE id='18200000-0000-0000-0000-000000001505'`, true},
			{"corrected fuel line", `UPDATE fuel_transaction_items SET total_amount_paid=120 WHERE fuel_transaction_id=(SELECT id FROM fuel_transactions WHERE relay_transaction_id='principal-fuel')`, true},
			{"changed fuel driver", `UPDATE fuel_transactions SET driver_id='18200000-0000-0000-0000-000000001500' WHERE relay_transaction_id='principal-fuel'`, true},
			{"changed fuel truck", `UPDATE fuel_transactions SET prompts='[{"label":"Truck #","value":"OTHER-IMPACT"}]' WHERE relay_transaction_id='principal-fuel'`, true},
			{"changed fuel date", `UPDATE fuel_transactions SET purchased_at='2026-10-13 12:00-04' WHERE relay_transaction_id='principal-fuel'`, true},
			{"deleted fuel and lines", `DELETE FROM fuel_transactions WHERE relay_transaction_id='principal-fuel'`, true},
			{"new linked toll", `INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
 VALUES('18200000-0000-0000-0000-000000000002','2026-10-06','2026-10-06','','','','','PROJECTION','','','2026-10-06','12:00','',40,repeat('i',64))`, true},
			{"corrected toll amount", `UPDATE tolls SET amount=45 WHERE row_fingerprint=repeat('i',64)`, true},
			{"changed toll truck", `UPDATE tolls SET truck_id='18200000-0000-0000-0000-000000001509' WHERE row_fingerprint=repeat('i',64)`, true},
			{"changed toll posting week", `UPDATE tolls SET posting_date='2026-10-13' WHERE row_fingerprint=repeat('i',64)`, true},
			{"deleted toll", `DELETE FROM tolls WHERE row_fingerprint=repeat('i',64)`, true},
			{"changed dispatch gross", `UPDATE gross_board_entries SET driver_rate=975 WHERE driver_id='18200000-0000-0000-0000-000000000001'`, true},
			{"new dispatch slot", `INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,driver_rate,entered_original_rate,entered_miles) VALUES('18200000-0000-0000-0000-000000001500','2026-10-06',1,'NEW-IMPACT',400,500,200)`, true},
			{"moved dispatch slot", `UPDATE gross_board_extra_entries SET driver_id='18200000-0000-0000-0000-000000000001',service_date='2026-10-13' WHERE load_number='NEW-IMPACT'`, true},
			{"deleted dispatch slot", `DELETE FROM gross_board_extra_entries WHERE load_number='NEW-IMPACT'`, true},
			{"corrected source gross and miles", `UPDATE loads SET total_pay=1300,total_miles=450 WHERE id=18201`, true},
			{"renamed source", `UPDATE loads SET load_id='RENAMED-IMPACT' WHERE id=18201`, true},
			{"new matching source", `INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(18202,'PROJECTION-1','delivered',800,900,350,'{}')`, true},
			{"reused source number", `UPDATE loads SET load_id='PROJECTION-1' WHERE id=18201`, true},
			{"removed duplicate source", `DELETE FROM loads WHERE id=18202`, true},
			{"corrected source trips", `UPDATE loads SET raw_payload='{"trips":[{"id":1,"total_pay":600,"loaded_miles":200,"empty_miles":50},{"id":2,"total_pay":700,"loaded_miles":200,"empty_miles":0}]}' WHERE id=18201`, true},
			{"restored unsplit source", `UPDATE loads SET raw_payload='{}' WHERE id=18201`, true},
			{"changed settlement agreement", `UPDATE truck_settlement_terms SET share_percent=85 WHERE truck_id='18200000-0000-0000-0000-000000000002'`, true},
			{"new truck fee", `INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included) VALUES('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000001508','2026-10-05',20,true)`, true},
			{"changed truck fee", `UPDATE truck_charge_phases SET amount=25 WHERE type_id='18200000-0000-0000-0000-000000001508'`, true},
			{"deleted truck fee", `DELETE FROM truck_charge_phases WHERE type_id='18200000-0000-0000-0000-000000001508'`, true},
			{"dated replacement assignment", `UPDATE truck_driver_assignments SET unassigned_at='2026-10-12 00:00-04' WHERE truck_id='18200000-0000-0000-0000-000000000002';
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000001500','2026-10-12 00:00-04')`, true},
			{"dated new team", `INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES('18200000-0000-0000-0000-000000001500','18200000-0000-0000-0000-000000001506','2026-10-12')`, true},
			{"dated team ending", `UPDATE driver_teams SET end_week='2026-10-19' WHERE team_driver_id='18200000-0000-0000-0000-000000001506'`, true},
			{"deleted team retains both destinations", `DELETE FROM driver_teams WHERE team_driver_id='18200000-0000-0000-0000-000000001506'`, true},
			{"changed recorded owner", `UPDATE truck_ownership_history SET owner_id='18200000-0000-0000-0000-000000001507' WHERE truck_id='18200000-0000-0000-0000-000000000002'`, true},
			{"broker follows unrelated load", `INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,covered_by,broker_load_number)
 SELECT '18200000-0000-0000-0000-000000001503',id,name,'Broker test','2026-10-05',45,'18200000-0000-0000-0000-000000001500','Broker','INDEPENDENT'
 FROM expense_settings WHERE kind='category' AND name='Other'`, true},
			{"broker deletion retains old destination", `DELETE FROM expenses WHERE id='18200000-0000-0000-0000-000000001503'`, true},
		}
		for _, step := range steps {
			t.Run(step.name, func(t *testing.T) {
				write := WithPayrollPublication(ctx)
				tx, err := beginPayrollMutation(write, pool)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				if _, err = tx.Exec(ctx, step.sql); err != nil {
					t.Fatal(err)
				}
				impact, err := payrollMutationImpact(ctx, tx, &payrollCommand{name: "native", beforeSnapshot: tx.(*payrollMutationTransaction).beforeSnapshot}, "", "")
				if err != nil || (impact != nil) != step.narrow {
					var tables []string
					_ = tx.QueryRow(ctx, `SELECT array_agg(DISTINCT table_name ORDER BY table_name) FROM payroll_projection_changes WHERE source_xid=pg_current_xact_id()`).Scan(&tables)
					t.Fatal("unexpected impact classification", impact, err, tables)
				}
				if err = commitPayrollMutation(write, tx); err != nil {
					t.Fatal(err)
				}
				for _, week := range weeks {
					read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
					if err != nil {
						t.Fatal(err)
					}
					full, err := readPayrollProjectionReference(ctx, read, week, true)
					_ = read.Rollback(ctx)
					if err != nil {
						t.Fatal(err)
					}
					for i, kind := range []string{"driver", "investor"} {
						page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
						a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
						b, _ := json.Marshal(page.Report)
						if err != nil || page.State != "ready" || string(a) != string(b) {
							t.Fatalf("principal publication differs %s %s: %v\n%s\n%s", kind, week, err, a, b)
						}
						// These last two reminders intentionally target the otherwise
						// independent driver's load number.
						if step.narrow && !strings.HasPrefix(step.name, "broker ") && kind == "driver" && versions[week.Format(time.DateOnly)] != page.Statements[independent].Revision {
							t.Fatal("principal edit reconstructed unrelated statement")
						}
					}
				}
			})
			if t.Failed() {
				return
			}
		}
	})
}
