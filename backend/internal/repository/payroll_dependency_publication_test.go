package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollPublicationDependenciesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		ctx = payrollPublicationReadContext(ctx, tx)
		shared := make([]string, 5600)
		for i := range shared {
			shared[i] = fmt.Sprintf("expense:18600000-0000-0000-0000-%012d", i)
		}
		// Another statement can be a real legacy cost dependency; only remove self.
		shared = append(shared, "statement:driver:18600000-0000-0000-0000-000000000999:2026-10-12")
		for _, week := range []string{"2026-10-19", "2026-10-26"} {
			resources := map[string][]string{}
			for i := 0; i < 100; i++ {
				identity := fmt.Sprintf("driver:18600000-0000-0000-0000-%012d", i)
				resources[identity] = append(append([]string{}, shared...), "statement:"+identity+":"+week)
			}
			packed, err := payrollPublicationDependencies(ctx, tx, week, resources)
			if err != nil {
				t.Fatal(err)
			}
			for identity, keys := range resources {
				var exact bool
				if err = tx.QueryRow(ctx, `SELECT payroll_expand_resource_dependencies($1)=payroll_resource_snapshot($2::text[])`, packed[identity], keys).Scan(&exact); err != nil || !exact {
					t.Fatal("batch changed exact dependency map", err)
				}
			}
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM payroll_resource_dependency_sets`).Scan(&count); err != nil || count != 1 {
			t.Fatal("identical cross-period component did not share snapshot", count, err)
		}
		if len(ctx.Value(payrollPublicationReadKey{}).(*payrollPublicationReadState).dependencySnapshots) != 1 {
			t.Fatal("component snapshot recomputed")
		}
	})
}
