package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollPostWriteInputsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		week, _ := chargeWeek("2026-10-05")
		read, err := selectPayrollContext(payrollWeekContext(ctx, "2026-10-05"), tx, "driver", projectionDriver)
		if err != nil {
			t.Fatal(err)
		}
		// Pre-write memos must not be carried forward, even in the same Tx.
		read = payrollReadContext(read, tx)
		old := payrollReadMemo(read, tx)
		read = beginPayrollPostWriteRead(read, tx)
		if payrollReadMemo(read, tx) == old {
			t.Fatal("post-write phase retained old inputs")
		}
		report, err := readDriverPayWeek(read, tx, week, "")
		if err != nil {
			t.Fatal(err)
		}
		read, err = retainPayrollPostWriteResult(read, tx, "driver", report)
		if err != nil {
			t.Fatal(err)
		}
		known := read.Value(payrollPostWriteResultKey{}).(*payrollPostWriteResult)
		read = context.WithValue(read, payrollSnapshotKey{}, payrollSnapshot{tx, known.snapshot})
		if value := payrollRetainedPostWrite(read, tx, week); value == nil || value.read != payrollReadMemo(read, tx) {
			t.Fatal("exact post-write inputs unavailable")
		}
		actual, err := readPayrollProjectionReference(read, tx, week, false)
		if err != nil {
			t.Fatal(err)
		}
		authority, err := readPayrollProjectionReference(read, tx, week, true)
		if err != nil || !samePayrollProjection(actual, authority) {
			t.Fatal("post-write input reuse changed complete report", err)
		}
		for name, invalid := range map[string]context.Context{
			"source":    context.WithValue(read, payrollSnapshotKey{}, payrollSnapshot{tx, known.snapshot + "changed"}),
			"component": context.WithValue(read, payrollSelectionKey{}, (*payrollSelection)(nil)),
			"authority": context.WithValue(read, payrollAuthoritativeKey{}, true),
		} {
			if payrollRetainedPostWrite(invalid, tx, week) != nil {
				t.Fatal("post-write inputs bypassed fence", name)
			}
		}
		if payrollRetainedPostWrite(read, tx, week.AddDate(0, 0, 7)) != nil || payrollRetainedPostWrite(read, nil, week) != nil {
			t.Fatal("post-write inputs crossed period or transaction")
		}
	})
}
