package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollResourceSourceFenceDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		var previous string
		for phase := 0; phase < 2; phase++ {
			tx, closeWrite, err := repo.beginPayrollWriteProtocol(ctx, true, "", "", "driver:"+projectionDriver)
			if err != nil {
				t.Fatal(err)
			}
			closed := false
			defer func() {
				if !closed {
					closeWrite()
				}
			}()
			measured, metrics := db.WithQueryMetrics(ctx)
			source, err := payrollProjectionFence(measured, tx)
			if err != nil || metrics.Queries.Load() == 0 || source == previous {
				t.Fatal("new resource transaction failed to validate current sources", err, metrics.Queries.Load())
			}
			before, err := payrollFinancialSnapshot(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO driver_pay_weeks(driver_id,week_start,notes) VALUES($1,'2026-10-05','First allocation')`, projectionDriver); err != nil {
				t.Fatal(err)
			}
			cached, metrics := db.WithQueryMetrics(ctx)
			afterSource, err := payrollCacheSnapshot(cached, tx)
			afterFence, fenceErr := payrollProjectionFence(cached, tx)
			if err != nil || fenceErr != nil || source != afterSource || source != afterFence || metrics.Queries.Load() != 0 {
				t.Fatal("fenced source phase was not reused", err, fenceErr, metrics.Queries.Load())
			}
			after, err := payrollFinancialSnapshot(ctx, tx)
			if err != nil || before == after {
				t.Fatal("source memo concealed changed financial resource epoch", err)
			}
			// Compare a fresh source query in the same snapshot, then verify
			// that this command cannot write a source table behind the memo.
			raw := tx.(*payrollResourceTransaction).Tx
			fresh, err := payrollProjectionFence(ctx, raw)
			if err != nil || fresh != source {
				t.Fatal("source revisions changed during resource command", err)
			}
			if _, err = tx.Exec(ctx, `UPDATE drivers SET active=false WHERE id=$1`, projectionDriver); err == nil {
				t.Fatal("resource command changed a source table")
			}
			previous = source
			closeWrite()
			closed = true
			if _, err := pool.Exec(ctx, `UPDATE drivers SET full_name=full_name||' changed' WHERE id=$1`, projectionDriver); err != nil {
				t.Fatal(err)
			}
		}
	})
}
