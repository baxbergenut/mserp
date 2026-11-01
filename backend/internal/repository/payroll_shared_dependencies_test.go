package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollSharedDependenciesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		keys := make([]string, 1000)
		for i := range keys {
			keys[i] = fmt.Sprintf("expense:18500000-0000-0000-0000-%012d", i)
		}
		packed := make([]json.RawMessage, 2)
		local := []string{"statement:driver:18500000-0000-0000-0000-000000000001:2026-10-05", "statement:driver:18500000-0000-0000-0000-000000000002:2026-10-05"}
		for i := range packed {
			all := append(append([]string{}, keys...), local[i])
			if err := pool.QueryRow(ctx, `SELECT payroll_pack_resource_snapshot($1::text[],$2)`, all, local[i]).Scan(&packed[i]); err != nil {
				t.Fatal(err)
			}
			var exact, current bool
			if err := pool.QueryRow(ctx, `SELECT payroll_expand_resource_dependencies($1)=payroll_resource_snapshot($2::text[]),payroll_resources_current($1)`, packed[i], all).Scan(&exact, &current); err != nil || !exact || !current {
				t.Fatal("decoded dependencies changed", err, exact, current)
			}
			if len(packed[i]) > 300 {
				t.Fatal("shared map remained inline", len(packed[i]))
			}
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM payroll_resource_dependency_sets`).Scan(&count); err != nil || count != 1 {
			t.Fatal("component snapshots were not shared", count, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO payroll_resource_revisions(resource_key,revision) VALUES($1,1)`, local[0]); err != nil {
			t.Fatal(err)
		}
		var first, second bool
		if err := pool.QueryRow(ctx, `SELECT payroll_resources_current($1),payroll_resources_current($2)`, packed[0], packed[1]).Scan(&first, &second); err != nil || first || !second {
			t.Fatal("statement-local fence lost", first, second, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO payroll_resource_revisions(resource_key,revision) VALUES($1,1)`, keys[0]); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT payroll_resources_current($1),payroll_resources_current($2)`, packed[0], packed[1]).Scan(&first, &second); err != nil || first || second {
			t.Fatal("shared financial fence lost", first, second, err)
		}
		if err := pool.QueryRow(ctx, `SELECT payroll_resources_current(jsonb_build_object('$shared',repeat('0',64))),payroll_resources_current('{"legacy-resource":0}'::jsonb)`).Scan(&first, &second); err != nil || first || !second {
			t.Fatal("missing or legacy snapshot handling", first, second, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_resource_dependency_sets SET dependencies='{}'`); err == nil {
			t.Fatal("immutable shared dependencies were editable")
		}
	})
}
