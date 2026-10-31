package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollStatementReceiptsDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) SELECT md5('receipt-driver'||n)::uuid,'Receipt Driver '||n,'receipt driver '||n,'cpm',0.75 FROM generate_series(1,3)n;
   INSERT INTO investors(id,full_name) SELECT md5('receipt-owner'||n)::uuid,'Receipt Owner '||n FROM generate_series(1,3)n;
   INSERT INTO trucks(id,unit_number,owner_id) SELECT md5('receipt-truck'||n)::uuid,'RECEIPT-'||n,md5('receipt-owner'||n)::uuid FROM generate_series(1,3)n;
   UPDATE truck_ownership_history SET start_known=false;
   INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) SELECT md5('receipt-truck'||n)::uuid,md5('receipt-driver'||n)::uuid,'2026-01-01' FROM generate_series(1,3)n;
   INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) SELECT md5('receipt-truck'||n)::uuid,md5('receipt-owner'||n)::uuid,'2026-01-05',88 FROM generate_series(1,3)n;
   INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles) SELECT md5('receipt-driver'||n)::uuid,'2026-09-28','RECEIPT-'||n,1000,1200,400 FROM generate_series(1,3)n;`)
		exec(`INSERT INTO expenses(id,category_id,category,expense_date,expense_type,amount,charge_driver_id,driver_id,covered_by)
 SELECT md5('receipt-expense'||n)::uuid,(SELECT id FROM expense_settings WHERE kind='category' AND name='Other'),'Other','2026-01-05','Old unpaid charge',200,md5('receipt-driver'||n)::uuid,md5('receipt-driver'||n)::uuid,'Driver' FROM generate_series(1,3)n;
 INSERT INTO expense_payments(expense_id,week_start,amount) SELECT md5('receipt-expense'||n)::uuid,'2026-10-26',20 FROM generate_series(1,3)n;
 INSERT INTO expenses(category_id,category,expense_date,expense_type,amount,driver_id,payment_type,covered_by)
 SELECT (SELECT id FROM expense_settings WHERE kind='category' AND name='Other'),'Other','2026-09-28','Reimbursement',12.25,md5('receipt-driver'||n)::uuid,'Reimburse','Company' FROM generate_series(1,3)n;
 INSERT INTO expenses(category_id,category,expense_date,expense_type,amount,truck_id,owner_id,covered_by)
 SELECT (SELECT id FROM expense_settings WHERE kind='category' AND name='Other'),'Other','2026-01-05','Old truck balance',90,md5('receipt-truck'||n)::uuid,md5('receipt-owner'||n)::uuid,'Truck Owner' FROM generate_series(1,3)n;
 INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount,opening_paid)
 SELECT md5('receipt-driver'||n)::uuid,'Receipt Driver '||n,'2026-01-05',2500,500 FROM generate_series(1,3)n;
 DELETE FROM gross_board_entries WHERE driver_id=md5('receipt-driver3')::uuid;
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at) VALUES(md5('receipt-truck1')::uuid,md5('receipt-driver3')::uuid,'2025-01-01','2025-08-01');
 INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES(md5('receipt-driver2')::uuid,md5('receipt-driver3')::uuid,'2026-09-28');`)
		pay := NewDriverPayRepository(pool)
		week, _ := chargeWeek("2026-09-28")
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var first string
		if err = tx.QueryRow(ctx, `SELECT md5('receipt-driver1')::uuid::text`).Scan(&first); err != nil {
			t.Fatal(err)
		}
		scoped, err := selectPayrollContext(payrollWeekContext(ctx, "2026-09-28"), tx, "driver", first)
		if err != nil || len(payrollDriverIDs(scoped)) != 1 {
			t.Fatalf("retired assignment expanded unrelated drivers: %v %v", payrollDriverIDs(scoped), err)
		}
		_ = tx.Rollback(ctx)
		for _, kind := range []string{"driver", "investor"} {
			t.Run(kind, func(t *testing.T) {
				full := func() DriverPayWeek {
					t.Helper()
					var v DriverPayWeek
					var e error
					tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(ctx)
					authority := context.WithValue(ctx, payrollAuthoritativeKey{}, true)
					if kind == "driver" {
						v, e = readDriverPayWeek(authority, tx, week, "")
					} else {
						v, e = readInvestorPay(authority, tx, week)
						v = investorPayView(v, "")
					}
					if e != nil {
						t.Fatal(e)
					}
					return v
				}
				settle := pay.Settle
				if kind == "investor" {
					settle = pay.SettleInvestor
				}
				report := full()
				if !strings.HasPrefix(report.Revision, "inputs:") {
					t.Fatal("fixture did not exercise committed input revisions")
				}
				var target string
				for _, d := range report.Drivers {
					if !strings.HasPrefix(d.FullName, "Receipt") {
						continue
					}
					if target == "" {
						target = d.ID
					}
					detail, e := pay.PayrollStatement(ctx, kind, week, d.ID)
					if e != nil {
						t.Fatal(e)
					}
					a, _ := json.Marshal(d)
					b, _ := json.Marshal(detail.Drivers[0])
					if string(a) != string(b) || detail.Revision != report.Revision || detail.Pagination != nil || len(detail.Drivers) != 1 {
						t.Fatalf("detail differs from authoritative report: %s\n%s", a, b)
					}
				}
				// An expanded selection must use the same snapshot/revision as its summary.
				page, e := pay.PayrollPage(ctx, kind, week, PayPageQuery{Summary: true, Pagination: Pagination{Page: 1, PageSize: 100}, DetailIDs: map[string]bool{target: true}})
				if e != nil || page.Revision != report.Revision {
					t.Fatal("summary/detail snapshot mismatch", e)
				}
				var edits DriverPayEdits
				for _, d := range report.Drivers {
					if d.ID == target {
						edits = d.Edits
					}
				}
				edits.Notes = "Committed receipt save"
				var saved DriverPayEdits
				if kind == "driver" {
					saved, e = pay.Save(WithPayrollReceipt(ctx), edits, actor)
				} else {
					saved, e = pay.SaveInvestorPay(WithPayrollReceipt(ctx), edits, actor)
				}
				if e != nil {
					t.Fatal(e)
				}
				report = full()
				if saved.InputRevision != report.Revision || saved.Version != edits.Version+1 {
					t.Fatal("save did not return its committed version/revision")
				}
				if _, e = settle(WithPayrollReceipt(ctx), week, target, "stale", actor, "", false, true); !errors.Is(e, ErrDriverPayConflict) {
					t.Fatal("stale receipt finalized", e)
				}
				// Missing load evidence still requires explicit reviewed override.
				_, e = settle(WithPayrollReceipt(ctx), week, target, report.Revision, actor, "", false, false)
				var blocked *PayrollFinalizationBlockedError
				if !errors.As(e, &blocked) || blocked.Review == nil || blocked.Review.Revision != report.Revision || len(blocked.Review.Drivers) != 1 {
					t.Fatal("structured review missing", e)
				}
				// Bulk validation may stage collection writes before a later blocking
				// statement. The whole transaction, including audit rows, must roll back.
				beforeBlocked := full().Revision
				if _, e = settle(WithPayrollReceipt(ctx), week, "", beforeBlocked, actor, "", false, false); e == nil {
					t.Fatal("bulk finalization bypassed unresolved evidence")
				}
				if full().Revision != beforeBlocked {
					t.Fatal("blocked bulk finalization leaked writes or revisions")
				}
				receipt, e := settle(WithPayrollReceipt(ctx), week, target, report.Revision, actor, "", false, true)
				if e != nil {
					t.Fatal(e)
				}
				check := func(receipt DriverPayWeek, finalized bool) {
					t.Helper()
					authoritative := full()
					if !receipt.Receipt || len(receipt.Drivers) != 1 || receipt.Drivers[0].ID != target || receipt.Revision != authoritative.Revision {
						t.Fatal("invalid committed receipt")
					}
					for _, d := range authoritative.Drivers {
						if d.ID == target {
							a, _ := json.Marshal(d)
							b, _ := json.Marshal(receipt.Drivers[0])
							if string(a) != string(b) {
								t.Fatalf("receipt differs from committed statement:\n%s\n%s", a, b)
							}
							if (d.Settlement != nil && d.Settlement.Finalized) != finalized {
								t.Fatal("wrong frozen state")
							}
						}
					}
				}
				check(receipt, true)
				if _, e = settle(WithPayrollReceipt(ctx), week, target, receipt.Revision, actor, "", true, false); e == nil {
					t.Fatal("reopen accepted no reason")
				}
				receipt, e = settle(WithPayrollReceipt(ctx), week, target, receipt.Revision, actor, "Receipt test", true, false)
				if e != nil {
					t.Fatal(e)
				}
				check(receipt, false)
				report = full()
				receipt, e = settle(WithPayrollReceipt(ctx), week, "", report.Revision, actor, "", false, true)
				if e != nil {
					t.Fatal(e)
				}
				if !receipt.Receipt || receipt.Revision != full().Revision {
					t.Fatal("bulk receipt revision mismatch")
				}
				for _, d := range receipt.Drivers {
					if d.Settlement == nil || !d.Settlement.Finalized {
						t.Fatal("bulk receipt returned unfinished write")
					}
				}
			})
		}
	})
}
