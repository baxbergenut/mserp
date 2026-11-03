package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
	"testing"
	"time"
)

func TestPreparedZeroCostFinalizationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Zero cost finalization',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedFinalization(true)
		repo.resourceConcurrency = true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		page, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{ID: projectionDriver}, true)
		if err != nil || len(page.Drivers) != 1 || page.Preparation.State != "ready" {
			t.Fatal("prepared", err)
		}
		// Zero current inputs must still clear a previously nonzero canonical posting.
		checkTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer checkTx.Rollback(ctx)
		if err = EnterPayrollSourceProtocol(ctx, checkTx); err != nil {
			t.Fatal(err)
		}
		if _, err = checkTx.Exec(ctx, `INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount) VALUES($1,$2,10,0,10,0)`, projectionDriver, week); err != nil {
			t.Fatal(err)
		}
		needed, err := payrollDriverCostsNeedPosting(ctx, checkTx, page.Drivers[0])
		if err != nil || !needed {
			t.Fatal("existing collection skipped", needed, err)
		}
		_ = checkTx.Rollback(ctx)
		measured, metrics := db.WithMetrics(WithPayrollReceipt(ctx))
		started := time.Now()
		command := PayrollSettlementCommand{RequestID: "18200000-0000-0000-0000-000000009910", ExpectedStatementRevision: page.Drivers[0].StatementRevision}
		result, err := repo.Settle(measured, week, projectionDriver, page.Revision, actor, "", false, true, command)
		if err != nil {
			t.Fatal(err)
		}
		if metrics.Events()["payroll_calculation"] != 0 {
			t.Fatal("empty first finalization recalculated", metrics.Events())
		}
		t.Logf("first zero-cost finalization: %s, %d queries, zero calculations", time.Since(started), metrics.Queries.Load())
		var costs int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM driver_pay_cost_collections`).Scan(&costs); err != nil || costs != 0 {
			t.Fatal("unnecessary zero ledger posting", costs, err)
		}
		if len(result.Drivers) != 1 || result.Drivers[0].Settlement == nil || !result.Drivers[0].Settlement.Finalized {
			t.Fatal("not finalized")
		}
		replay, err := repo.Settle(WithPayrollReceipt(ctx), week, projectionDriver, page.Revision, actor, "", false, true, command)
		a, _ := json.Marshal(result)
		b, _ := json.Marshal(replay)
		if err != nil || string(a) != string(b) {
			t.Fatal("retry receipt", err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		reference, err := readPayrollProjectionReference(ctx, tx, week, true)
		_ = tx.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			got, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(reference[i], PayPageQuery{}))
			b, _ := json.Marshal(got.Report)
			if err != nil || got.State != "ready" || string(a) != string(b) {
				t.Fatalf("%s parity: %v\n%s\n%s", kind, err, a, b)
			}
		}

	})
}
