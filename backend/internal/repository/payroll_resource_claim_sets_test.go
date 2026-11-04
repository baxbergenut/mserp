package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollResourceClaimSetsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources',capture_enabled=true`); err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 2000)
		for i := range keys {
			keys[i] = fmt.Sprintf("expense:19800000-0000-0000-0000-%012d", i)
		}
		for _, tc := range []string{"complete", "missing claim", "missing lock", "missing gate", "stale epoch", "empty", "null key"} {
			t.Run(tc, func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				claimed, locked, required := keys, keys, keys
				if tc == "missing claim" {
					claimed = keys[1:]
				}
				if tc == "missing lock" {
					locked = keys[1:]
				}
				if tc == "empty" {
					required = nil
				}
				if tc != "missing gate" {
					if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(key,182736281940)) FROM unnest($1::text[]) key ORDER BY hashtextextended(key,182736281940)`, locked); err != nil {
					t.Fatal(err)
				}
				body, _ := json.Marshal(claimed)
				delta := 0
				if tc == "stale epoch" {
					delta = 1
				}
				if _, err = tx.Exec(ctx, `SELECT set_config('mserp.payroll.resources',$1,true),set_config('mserp.payroll.protocol_epoch',(SELECT (protocol_epoch+$2)::text FROM payroll_projection_control),true)`, string(body), delta); err != nil {
					t.Fatal(err)
				}
				if tc == "null key" {
					_, err = tx.Exec(ctx, `SELECT payroll_assert_resources(ARRAY[NULL]::text[])`)
				} else {
					_, err = tx.Exec(ctx, `SELECT payroll_assert_resources($1::text[])`, required)
				}
				if tc == "complete" {
					if err != nil {
						t.Fatal("complete lock set rejected", err)
					}
				} else if pgerr, ok := err.(*pgconn.PgError); !ok || pgerr.Code != "40001" {
					t.Fatal("incomplete or obsolete lock set accepted", err)
				}
			})
		}
	})
}
