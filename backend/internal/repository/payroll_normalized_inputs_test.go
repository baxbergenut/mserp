package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func TestPayrollNormalizedInputsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO driver_pay_cost_collections(driver_id,week_start,fuel_base,toll_base,fuel_amount,toll_amount)
SELECT '18200000-0000-0000-0000-000000000001',d,100,20,-30,-5 FROM unnest(ARRAY['2026-08-03'::date,'2026-08-10','2026-09-07','2026-09-14','2026-10-05','2026-11-02']) d;
INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement)
SELECT source,'18200000-0000-0000-0000-000000000003',d,'investor','18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000000002','Test source',-100,-30,'{}'
FROM unnest(ARRAY['2026-08-03'::date,'2026-08-10','2026-09-07','2026-09-14','2026-10-05','2026-11-02']) d
CROSS JOIN unnest(ARRAY['investor_cost:18200000-0000-0000-0000-000000000002:fuel','truck:test']) source`)
		parity := func() {
			t.Helper()
			for _, week := range []string{"2026-08-03", "2026-09-07", "2026-10-05", "2026-11-02"} {
				tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				if err != nil {
					t.Fatal(err)
				}
				read := payrollWeekContext(ctx, week)
				fast := context.WithValue(read, payrollNormalizedFactsKey{}, true)
				wantCost, err := readPayCostRecords(read, tx)
				if err != nil {
					t.Fatal(err)
				}
				gotCost, err := readPayCostRecords(fast, tx)
				if err != nil {
					t.Fatal(err)
				}
				wantSource, err := readPaySources(read, tx)
				if err != nil {
					t.Fatal(err)
				}
				gotSource, err := readPaySources(fast, tx)
				if err != nil {
					t.Fatal(err)
				}
				a, _ := json.Marshal(wantCost)
				b, _ := json.Marshal(gotCost)
				if string(a) != string(b) {
					t.Fatalf("cost buckets %s\n%s\n%s", week, a, b)
				}
				a, _ = json.Marshal(wantSource)
				b, _ = json.Marshal(gotSource)
				if string(a) != string(b) {
					t.Fatalf("source buckets %s\n%s\n%s", week, a, b)
				}
				_ = tx.Rollback(ctx)
			}
		}
		parity()
		exec(`INSERT INTO payroll_cost_start(week_start,reason) VALUES('2026-09-14','Test boundary inside a balance month')`)
		parity()
		exec(`UPDATE driver_pay_cost_collections SET fuel_amount=-50 WHERE week_start='2026-08-03';UPDATE pay_source_collections SET amount=-40,version=version+1 WHERE week_start='2026-08-03'`)
		parity()
		exec(`BEGIN; UPDATE driver_pay_cost_collections SET fuel_base=900;UPDATE pay_source_collections SET base=-900;ROLLBACK`)
		parity()
		exec(`DELETE FROM driver_pay_cost_collections WHERE week_start='2026-08-03';DELETE FROM pay_source_collections WHERE week_start='2026-08-03'`)
		parity()
		prepare := func() {
			t.Helper()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if err = preparePayrollLoadFacts(ctx, tx); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		count := func() int {
			t.Helper()
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM payroll_load_facts`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		prepare()
		prepare()
		if count() != 1 {
			t.Fatal("same source version normalized twice")
		}
		exec(`UPDATE loads SET synced_at=clock_timestamp()`)
		prepare()
		if count() != 1 {
			t.Fatal("timestamp-only sync normalized again")
		}
		exec(`UPDATE loads SET total_pay=total_pay+1`)
		prepare()
		if count() != 2 {
			t.Fatal("source correction reused obsolete fact")
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_load_facts SET trip_count=999`); err == nil {
			t.Fatal("normalized history mutable")
		}
		exec(`TRUNCATE driver_pay_cost_collections,pay_source_collections`)
		parity()
	})
}
