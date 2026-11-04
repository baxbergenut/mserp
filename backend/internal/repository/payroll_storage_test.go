package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollCurrentStorageDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Storage Test',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		prepare := func() {
			t.Helper()
			if err := repo.RequestPayrollPreparation(ctx, week, true); err != nil {
				t.Fatal(err)
			}
			if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
				t.Fatal(worked, err)
			}
		}
		count := func(query string) int {
			t.Helper()
			var n int
			if err := pool.QueryRow(ctx, query).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		prepare()
		original, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil {
			t.Fatal(err)
		}
		old := original.Drivers[0].StatementRevision
		for i := 0; i < 4; i++ {
			prepare()
		}
		if count(`SELECT count(*) FROM payroll_statement_current`) != 2 || count(`SELECT count(*) FROM payroll_statement_references`) != 0 {
			t.Fatal("repeated preparation retained history")
		}
		current, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil || current.Drivers[0].StatementRevision != old {
			t.Fatal("identical preparation changed statement revision", err)
		}
		edits := current.Drivers[0].Edits
		edits.RequestID = "18900000-0000-0000-0000-000000000001"
		edits.Notes = "keep this committed receipt"
		saved, err := repo.Save(ctx, edits, actor)
		if err != nil {
			t.Fatal(err)
		}
		if count(`SELECT count(*) FROM payroll_statement_current`) != 2 || count(`SELECT count(*) FROM payroll_statement_references`) < 2 {
			t.Fatal("command input/output evidence missing")
		}
		if _, err = repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{ID: projectionDriver, StatementRevision: old}); !errors.Is(err, ErrPayrollVersionExpired) {
			t.Fatal("old revision exposed as a current read", err)
		}
		if _, err = repo.PreparedCostTransactions(ctx, "driver", projectionDriver, week, "fuel", old); !errors.Is(err, ErrPayrollVersionExpired) {
			t.Fatal("old cost revision exposed as current", err)
		}
		before := count(`SELECT count(*) FROM payroll_statement_references`)
		if _, err = pool.Exec(ctx, `UPDATE loads SET total_miles=401 WHERE id=18201`); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.DispatchPayrollChanges(ctx); err != nil {
			t.Fatal(err)
		}
		prepare()
		if count(`SELECT count(*) FROM payroll_statement_references`) != before {
			t.Fatal("background recalculation created statement references")
		}
		if err = repo.SeedPayrollPreparation(ctx); err != nil {
			t.Fatal(err)
		}
		replayed, err := NewDriverPayRepository(pool).Save(ctx, edits, actor)
		if err != nil || replayed.StatementRevision != saved.StatementRevision || replayed.Notes != saved.Notes {
			t.Fatal("cleanup broke exact receipt recovery", err)
		}
		if count(`SELECT count(*) FROM payroll_statement_current c LEFT JOIN payroll_statement_state s ON s.version_id=c.id WHERE s.version_id IS NULL`) != 0 {
			t.Fatal("orphan current row")
		}
		if count(`SELECT count(*) FROM payroll_projection_generations g WHERE NOT EXISTS(SELECT 1 FROM payroll_projection_weeks w WHERE w.generation_id=g.id) AND NOT EXISTS(SELECT 1 FROM payroll_statement_versions v WHERE v.generation_id=g.id)`) != 0 {
			t.Fatal("orphan generations retained")
		}
		for _, query := range []string{`UPDATE payroll_statement_references SET detail_hash='changed'`, `TRUNCATE payroll_statement_references`, `DELETE FROM payroll_storage_cutovers`} {
			if _, err = pool.Exec(ctx, query); err == nil {
				t.Fatal("audit mutation accepted", query)
			}
		}
	})
}

// Exercise a populated upgrade, including foreign keys and JSON-only references.
// The normal fixture's incremental branch upgrades empty projection tables.
func TestPayrollCurrentStoragePopulatedUpgrade(t *testing.T) {
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable test database required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.Database, "_test") || (cfg.Host != "localhost" && cfg.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := pgx.Identifier{fmt.Sprintf("payroll_storage_upgrade_%d", time.Now().UnixNano())}.Sanitize()
	exec := func(sql string) {
		t.Helper()
		if _, err = admin.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE SCHEMA ` + schema + `; SET search_path TO ` + schema + `,public`)
	defer admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+schema+` CASCADE`)
	init, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/189_payroll_current_storage.sql")
	if err != nil {
		t.Fatal(err)
	}
	expenseMigration, err := os.ReadFile("../../sql/198_payroll_expense_drafts.sql")
	if err != nil {
		t.Fatal(err)
	}
	prior := strings.Replace(strings.ReplaceAll(string(init), "\r\n", "\n"), strings.ReplaceAll(string(expenseMigration), "\r\n", "\n"), "", 1)
	exec(strings.Replace(prior, strings.ReplaceAll(string(migration), "\r\n", "\n"), "", 1))
	exec(`INSERT INTO app_users(id,username,password_hash) VALUES('18900000-0000-0000-0000-000000000001','Upgrade Storage',crypt('test-only',gen_salt('bf')));
 INSERT INTO payroll_projection_generations(id,week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 SELECT md5('storage-generation-'||n)::uuid,'2026-10-05',n,'old-exact-snapshot','2026-11-02','payroll-182-v4',7,'{}','{}','{}' FROM generate_series(1,12)n;
 INSERT INTO payroll_statement_versions(id,generation_id,kind,statement_id,week_start,detail,cost_evidence,summary,fee_cents,payable_cents,finalized,edit_version,settlement_version,resource_dependencies)
 SELECT md5('storage-version-'||n)::uuid,md5('storage-generation-'||n)::uuid,'driver','18900000-0000-0000-0000-000000000002','2026-10-05',
 jsonb_build_object('amount',CASE WHEN n=11 THEN 200 ELSE 100 END),'{}','{}',100,CASE WHEN n=11 THEN 200 ELSE 100 END,n BETWEEN 9 AND 11,1,1,
 (SELECT jsonb_object_agg('resource-'||i,1) FROM generate_series(1,500)i) FROM generate_series(1,12)n;
 INSERT INTO payroll_statement_state SELECT kind,statement_id,week_start,id FROM payroll_statement_versions WHERE id=md5('storage-version-12')::uuid;
 INSERT INTO payroll_projection_weeks VALUES('2026-10-05',md5('storage-generation-12')::uuid);
 INSERT INTO payroll_command_receipts(actor_id,request_id,command,request_hash,expected_revision,result,affected_statements)
 VALUES('18900000-0000-0000-0000-000000000001','18900000-0000-0000-0000-000000000003','driver.save','exact-hash',md5('storage-version-3')::uuid::text,
 jsonb_build_object('nested',jsonb_build_object('statementRevision',md5('storage-version-4')::uuid::text)),'[]');
 INSERT INTO payroll_command_versions VALUES('18900000-0000-0000-0000-000000000001','18900000-0000-0000-0000-000000000003',md5('storage-version-2')::uuid);`)
	exec(string(migration))
	var good bool
	err = admin.QueryRow(ctx, `SELECT
 (SELECT count(*)=1 FROM payroll_statement_current) AND (SELECT count(*)=3 FROM payroll_statement_references)
 AND (SELECT count(*)=1 FROM payroll_projection_generations)
 AND (SELECT count(*)=3 FROM payroll_statement_references WHERE id IN (md5('storage-version-2')::uuid,md5('storage-version-3')::uuid,md5('storage-version-4')::uuid))
 AND (SELECT bool_and(payroll_expand_resource_dependencies(resource_dependencies)=(SELECT jsonb_object_agg('resource-'||i,1) FROM generate_series(1,500)i)) FROM payroll_statement_versions)
 AND (SELECT previous_copies=12 AND current_statements=1 AND retained_references=3 FROM payroll_storage_cutovers)
 AND to_regclass(quote_ident(current_schema())||'.payroll_statement_versions_legacy') IS NULL`).Scan(&good)
	if err != nil || !good {
		t.Fatal("populated cutover lost a root, altered exact dependencies or retained duplicates", good, err)
	}
}
