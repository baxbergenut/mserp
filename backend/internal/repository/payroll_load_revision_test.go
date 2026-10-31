package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func TestPayrollLoadRevisionDatabase(t *testing.T) {
	withTruckChargeDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ string) {
		exec := func(sql string) {
			t.Helper()
			if _, e := pool.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
		}
		revision := func(q chargeQuery) int64 {
			t.Helper()
			var n int64
			if e := q.QueryRow(ctx, `SELECT revision FROM payroll_cache_revisions WHERE table_name='loads'`).Scan(&n); e != nil {
				t.Fatal(e)
			}
			return n
		}
		exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(18101,'REVISION-1','delivered',100,100,10,'{}'),(18102,'REVISION-2','delivered',100,100,10,'{}')`)
		initial := revision(pool)
		exec(`UPDATE loads SET synced_at=now() WHERE id IN(18101,18102)`)
		exec(`UPDATE loads SET total_pay=total_pay WHERE id=18101`)
		if revision(pool) != initial {
			t.Fatal("sync bookkeeping or no-op evicted payroll")
		}
		for _, sql := range []string{
			`UPDATE loads SET total_pay=total_pay+1 WHERE id=18101`,
			`UPDATE loads SET total_miles=total_miles+1 WHERE id=18101`,
			`UPDATE loads SET status='invoiced' WHERE id=18101`,
			`UPDATE loads SET pickup_time='2026-10-01' WHERE id=18101`,
			`UPDATE loads SET raw_payload='{"trips":[{"id":1}]}' WHERE id=18101`,
			`UPDATE loads SET synced_at=now(),total_pay=CASE WHEN id=18102 THEN total_pay+1 ELSE total_pay END WHERE id IN(18101,18102)`,
		} {
			exec(sql)
			if revision(pool) != initial+1 {
				t.Fatal("source change did not invalidate exactly once")
			}
			initial++
		}
		tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		pinned := revision(tx)
		exec(`UPDATE loads SET total_pay=total_pay+1 WHERE id=18101`)
		if revision(tx) != pinned || revision(pool) != pinned+1 {
			t.Fatal("commit violated snapshot invalidation")
		}
		initial = revision(pool)
		exec(`BEGIN;UPDATE loads SET total_pay=total_pay+1 WHERE id=18101;ROLLBACK`)
		if revision(pool) != initial {
			t.Fatal("rollback invalidated committed payroll")
		}
	})
}
