package repository

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
)

func TestPayrollExactCertificatesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`UPDATE payroll_projection_control SET protocol_mode='resources'`)
		exec(`INSERT INTO payroll_resource_revisions(resource_key,revision) VALUES('certificate:a',2),('certificate:b',3) ON CONFLICT(resource_key) DO UPDATE SET revision=excluded.revision`)
		shared := func(body string) string {
			t.Helper()
			var fingerprint string
			err := pool.QueryRow(ctx, `SELECT encode(sha256(convert_to($1::jsonb::text,'UTF8')),'hex')`, body).Scan(&fingerprint)
			if err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO payroll_resource_dependency_sets(fingerprint,dependencies) VALUES($1,$2::jsonb) ON CONFLICT DO NOTHING`, fingerprint, body)
			return fingerprint
		}
		exactShared := shared(`{"certificate:a":2,"certificate:b":3}`)
		staleShared := shared(`{"certificate:a":1,"certificate:b":3}`)
		emptyShared := shared(`{}`)
		packed := func(fp, local string) string { return fmt.Sprintf(`{"$shared":%q%s}`, fp, local) }
		cases := []struct {
			name, deps, deltas, roots, mode string
			exact, current, sources         bool
		}{
			{"plain exact", `{"certificate:a":2}`, `{}`, `[]`, "resources", true, true, true},
			{"plain explained delta", `{"certificate:a":1}`, `{"certificate:a":1}`, `[]`, "resources", false, true, true},
			{"plain unexplained", `{"certificate:a":1}`, `{}`, `[]`, "resources", false, false, true},
			{"shared exact", packed(exactShared, ""), `{}`, `[]`, "resources", true, true, true},
			{"shared explained delta", packed(staleShared, ""), `{"certificate:a":1}`, `[]`, "resources", false, true, true},
			{"shared stale", packed(staleShared, ""), `{}`, `[]`, "resources", false, false, true},
			{"local override masks stale shared", packed(staleShared, `,"certificate:a":2`), `{}`, `[]`, "resources", true, true, true},
			{"local override stale", packed(exactShared, `,"certificate:a":1`), `{}`, `[]`, "resources", false, false, true},
			{"local override explained", packed(exactShared, `,"certificate:a":1`), `{"certificate:a":1}`, `[]`, "resources", false, true, true},
			{"missing shared", packed(strings.Repeat("0", 64), ""), `{}`, `[]`, "resources", false, false, true},
			{"source changed rejects exact", packed(exactShared, ""), `{}`, `["certificate:a"]`, "resources", true, true, false},
			{"empty resource", `{}`, `{}`, `[]`, "resources", true, false, true},
			{"empty global", `{}`, `{}`, `[]`, "global", true, true, true},
			{"empty shared global", packed(emptyShared, ""), `{}`, `[]`, "global", true, true, true},
		}
		// Substitute only version fixture input; production proof/resource logic is unchanged.
		pos := strings.Index(payrollStatementFreshnessSQL, " ), proofs AS MATERIALIZED (")
		if pos < 0 {
			t.Fatal("freshness fixture marker missing")
		}
		query := `WITH versions AS MATERIALIZED (SELECT '18200000-0000-0000-0000-000000009001'::uuid id,
 '{}'::jsonb summary,'18200000-0000-0000-0000-000000009002'::uuid statement_id,
 '18200000-0000-0000-0000-000000009003'::uuid generation_id,$1::jsonb resource_dependencies
 WHERE $2::text='' AND $3::text=''` + payrollStatementFreshnessSQL[pos:] + `
 SELECT resources_exact,resources_current,sources_current,
 CASE WHEN sources_current AND resources_current THEN
 payroll_expand_resource_dependencies(CASE WHEN resources_exact THEN resource_dependencies ELSE
 payroll_pack_resource_snapshot(ARRAY(SELECT jsonb_object_keys(payroll_expand_resource_dependencies(resource_dependencies))),'statement:test') END)
 =payroll_expand_resource_dependencies(payroll_pack_resource_snapshot(ARRAY(SELECT jsonb_object_keys(payroll_expand_resource_dependencies(resource_dependencies))),'statement:test'))
 ELSE true END FROM freshness`
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				exec(`UPDATE payroll_projection_control SET protocol_mode=$1`, tc.mode)
				proof := fmt.Sprintf(`{"18200000-0000-0000-0000-000000009003":{"complete":true,"roots":%s,"deltas":%s}}`, tc.roots, tc.deltas)
				var exact, current, sources, parity bool
				if err := pool.QueryRow(ctx, query, tc.deps, "", "", proof).Scan(&exact, &current, &sources, &parity); err != nil {
					t.Fatal(err)
				}
				if exact != tc.exact || current != tc.current || sources != tc.sources || !parity {
					t.Fatalf("got exact/current/sources/parity %v/%v/%v/%v; want %v/%v/%v/true", exact, current, sources, parity, tc.exact, tc.current, tc.sources)
				}
			})
		}
	})
}
