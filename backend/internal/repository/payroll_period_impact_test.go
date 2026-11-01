package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollSettlementPeriodDatabase(t *testing.T) {
	for _, mode := range []string{"dual", "resources"} {
		t.Run(mode, func(t *testing.T) { testPayrollSettlementPeriod(t, mode) })
	}
}

func testPayrollSettlementPeriod(t *testing.T, mode string) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'Shared dependency fixture','2026-10-05',1,'18200000-0000-0000-0000-000000000001','Company'
 FROM expense_settings CROSS JOIN generate_series(1,180) WHERE kind='category' AND name='Other'`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start)
 SELECT '18200000-0000-0000-0000-000000000001',week::date FROM (VALUES('2026-09-28'),('2026-10-12')) w(week);
 INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits)
 SELECT '18200000-0000-0000-0000-000000000002',week::date,'18200000-0000-0000-0000-000000000003','{}'::jsonb
 FROM (VALUES('2026-09-28'),('2026-10-12')) w(week)`); err != nil {
			t.Fatal(err)
		}
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Period impact',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(true)
		repo.ConfigurePreparedFinalization(true)
		repo.resourceLockAudit = true
		repo.resourceConcurrency = mode == "resources"
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode=$1 WHERE singleton`, mode); err != nil {
			t.Fatal(err)
		}
		weeks := []time.Time{}
		for _, date := range []string{"2026-09-28", "2026-10-05", "2026-10-12"} {
			week, _ := chargeWeek(date)
			weeks = append(weeks, week)
			if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
		}
		week := weeks[1]
		seq := 1300
		for _, kind := range []string{"driver", "investor"} {
			id := projectionDriver
			if kind == "investor" {
				id = projectionTruck
			}
			page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
			if err != nil || len(page.Drivers) != 1 {
				t.Fatal(page, err)
			}
			seq++
			edits := page.Drivers[0].Edits
			edits.ExpectedStatementRevision = page.Drivers[0].StatementRevision
			edits.RequestID = fmt.Sprintf("18200000-0000-0000-0000-%012d", seq)
			if kind == "driver" {
				_, err = repo.Save(ctx, edits, actor)
			} else {
				_, err = repo.SaveInvestorPay(ctx, edits, actor)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, reopen := range []bool{false, true} {
				var before string
				var otherBefore string
				if err = pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY statement_id),'[]')::text FROM payroll_statement_state s WHERE week_start=$1 AND kind<>$2`, week, kind).Scan(&otherBefore); err != nil {
					t.Fatal(err)
				}
				if err = pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY kind,week_start,statement_id),'[]')::text FROM payroll_statement_state s WHERE week_start<>$1`, week).Scan(&before); err != nil {
					t.Fatal(err)
				}
				page, err = repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
				if err != nil {
					t.Fatal(err)
				}
				seq++
				command := PayrollSettlementCommand{RequestID: fmt.Sprintf("18200000-0000-0000-0000-%012d", seq), ExpectedStatementRevision: page.Drivers[0].StatementRevision}
				var result DriverPayWeek
				if kind == "driver" {
					result, err = repo.Settle(WithPayrollReceipt(ctx), week, id, page.Revision, actor, "Period reopen", reopen, !reopen, command)
				} else {
					result, err = repo.SettleInvestor(WithPayrollReceipt(ctx), week, id, page.Revision, actor, "Period reopen", reopen, !reopen, command)
				}
				if err != nil {
					t.Fatal(kind, reopen, err)
				}
				if !reopen {
					var otherAfter string
					if err = pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY statement_id),'[]')::text FROM payroll_statement_state s WHERE week_start=$1 AND kind<>$2`, week, kind).Scan(&otherAfter); err != nil || otherBefore != otherAfter {
						t.Fatal("metadata-only finalization republished the other page", kind, err)
					}
				}
				for _, affected := range result.AffectedStatements {
					if affected.WeekStart != week.Format(time.DateOnly) {
						t.Fatal("metadata-only settlement republished another period", affected)
					}
				}
				var after string
				if err = pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY kind,week_start,statement_id),'[]')::text FROM payroll_statement_state s WHERE week_start<>$1`, week).Scan(&after); err != nil || before != after {
					t.Fatal("unaffected period versions changed", err, before, after)
				}
				for _, w := range weeks {
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
						current, err := repo.ReadPreparedPayrollPage(ctx, pageKind, w, PayPageQuery{})
						a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
						b, _ := json.Marshal(current.Report)
						if err != nil || current.State != "ready" || string(a) != string(b) {
							t.Fatalf("period rebase differs %s %s: %v\n%s\n%s", pageKind, w, err, a, b)
						}
					}
				}
			}
		}
		// An old saved override without a canonical cost row still reconstructs
		// its base from settlement status. It must retain cross-period publication.
		if _, err := pool.Exec(ctx, `DELETE FROM driver_pay_cost_collections WHERE driver_id=$1`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE driver_pay_weeks SET fuel_override=-5 WHERE driver_id=$1`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		snapshot, err := payrollCacheSnapshot(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `UPDATE payroll_settlements SET finalized=true WHERE driver_id=$1`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		impact, err := payrollMutationImpact(ctx, tx, &payrollCommand{name: "driver.finalize", beforeSnapshot: snapshot}, "driver", projectionDriver)
		if err != nil || impact == nil || impact.weeks != nil {
			t.Fatal("legacy override incorrectly limited to one period", impact, err)
		}
	})
}
