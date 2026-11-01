package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollLoadCohortDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		count := func(sql string) int {
			t.Helper()
			var n int
			if err := pool.QueryRow(ctx, sql).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		prepare := func(ids ...[]int64) {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if err = preparePayrollLoadFacts(ctx, tx, ids...); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		// Partial and empty preparations cannot certify all source records.
		prepare([]int64{})
		prepare([]int64{18201})
		if count("SELECT count(*) FROM payroll_routing_facts WHERE kind='load_cohort'") != 0 {
			t.Fatal("partial preparation certified a full cohort")
		}
		exec("INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(18202,'COHORT-2','delivered',500,500,200,'{}')")
		prepare()
		if count("SELECT count(*) FROM payroll_load_facts") != 2 {
			t.Fatal("full preparation skipped an unprepared source")
		}
		if count("SELECT count(*) FROM payroll_routing_facts WHERE kind='load_cohort'") != 1 {
			t.Fatal("missing complete cohort")
		}
		// Check the actual plan, rather than timing a tiny fixture.
		var plan []byte
		if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE,FORMAT JSON) "+payrollPrepareLoadFactsSQL, payrollCalculatorVersion, []int64(nil)).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		var nodes []map[string]any
		if err := json.Unmarshal(plan, &nodes); err != nil {
			t.Fatal(err)
		}
		var visit func(any)
		visit = func(v any) {
			switch n := v.(type) {
			case map[string]any:
				if n["Relation Name"] == "loads" && n["Actual Loops"] != float64(0) {
					t.Fatalf("complete cohort rescanned loads: %s", plan)
				}
				for _, v := range n {
					visit(v)
				}
			case []any:
				for _, v := range n {
					visit(v)
				}
			}
		}
		visit(nodes[0])
		exec("UPDATE loads SET synced_at=clock_timestamp()")
		prepare()
		if count("SELECT count(*) FROM payroll_routing_facts WHERE kind='load_cohort'") != 1 {
			t.Fatal("timestamp-only sync invalidated cohort")
		}
		exec("UPDATE loads SET total_pay=total_pay+1 WHERE id=18201")
		prepare([]int64{18202})
		if count("SELECT count(*) FROM payroll_load_facts") != 2 {
			t.Fatal("unexpected partial normalization")
		}
		prepare()
		if count("SELECT count(*) FROM payroll_load_facts") != 3 {
			t.Fatal("partial preparation hid a source correction")
		}
		// Marker and facts roll back together.
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "UPDATE loads SET total_miles=total_miles+1 WHERE id=18201"); err != nil {
			t.Fatal(err)
		}
		if err = preparePayrollLoadFacts(ctx, tx); err != nil {
			t.Fatal(err)
		}
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if count("SELECT count(*) FROM payroll_load_facts") != 3 || count("SELECT count(*) FROM payroll_routing_facts WHERE kind='load_cohort'") != 2 {
			t.Fatal("rollback retained derived facts or marker")
		}
		exec("DELETE FROM loads WHERE id=18202")
		prepare()
		exec("INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(18202,'COHORT-2','delivered',900,900,300,'{}')")
		prepare()
		if count("SELECT count(*) FROM payroll_load_facts") != 4 {
			t.Fatal("reinsert reused obsolete fact")
		}
		n := count("SELECT count(*) FROM payroll_routing_facts WHERE kind='load_cohort'")
		exec("UPDATE gross_board_entries SET entered_miles=entered_miles+1")
		prepare()
		if count("SELECT count(*) FROM payroll_routing_facts WHERE kind='load_cohort'") != n {
			t.Fatal("board edit invalidated load normalization")
		}
	})
}
