package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func withoutPTIPayrollMigration(t *testing.T, source string) string {
	t.Helper()
	body, err := os.ReadFile("../../sql/191_payroll_pti_classification.sql")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Replace(source, strings.ReplaceAll(string(body), "\r\n", "\n"), "", 1)
}

func applyPTIPayrollMigration(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	body, err := os.ReadFile("../../sql/191_payroll_pti_classification.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
}

func TestPayrollPTIClassificationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('PTI classification',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
			t.Fatal(worked, err)
		}
		read := func() (string, [2]DriverPayWeek) {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			snapshot, err := payrollProjectionFence(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			reports, err := readPayrollProjectionReference(ctx, tx, week, true)
			if err != nil {
				t.Fatal(err)
			}
			return snapshot, reports
		}
		before, reports := read()
		var pointers, jobs string
		if err := pool.QueryRow(ctx, `SELECT (SELECT jsonb_agg(to_jsonb(s) ORDER BY kind,statement_id,week_start)::text FROM payroll_statement_state s),
 (SELECT jsonb_agg(to_jsonb(j) ORDER BY week_start)::text FROM payroll_projection_jobs j)`).Scan(&pointers, &jobs); err != nil {
			t.Fatal(err)
		}
		_, err := pool.Exec(ctx, `INSERT INTO weekly_pti_weeks VALUES('2026-11-02',now(),'System',false);
 INSERT INTO weekly_pti_rows(week_start,truck_id,data) VALUES('2026-11-02',$1,'{"eligibility":"required","review":"pending","received":false}');
 UPDATE weekly_pti_rows SET version=version+1,data=data||'{"notes":"Inspection updated"}';
 INSERT INTO weekly_pti_events VALUES(gen_random_uuid(),'request','2026-11-02',$1,'{}','{}',$2,'Reviewer',now());
 INSERT INTO weekly_pti_layout(truck_id,display_group,position) VALUES($1,'company',0);
 UPDATE weekly_pti_layout SET position=1;
 UPDATE weekly_pti_layout_state SET version=version+1;
 INSERT INTO weekly_pti_layout_events(actor_id,before_layout,after_layout) VALUES($2,'[]','[]')`, pgx.QueryExecModeSimpleProtocol, projectionTruck, actor)
		if err != nil {
			t.Fatal(err)
		}
		// Both ordinary PTI saves must finish while a payroll command holds its
		// shared source gate. Neither checklist owns any financial source write.
		hold, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Rollback(ctx)
		if _, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`); err != nil {
			t.Fatal(err)
		}
		work, cancel := context.WithTimeout(ctx, 2*time.Second)
		fleet := NewFleetRepository(pool)
		_, err = fleet.SavePTILayout(work, actor, PTILayoutChange{Version: 2, Placements: []PTIPlacement{{TruckID: projectionTruck, Group: "company"}}})
		if err == nil {
			_, err = fleet.ChangePTI(work, "2026-11-02", projectionTruck, actor, PTIChange{RequestID: "19100000-0000-0000-0000-000000000001", Version: 2, Review: "pending", Notes: "Concurrent inspection save"})
		}
		cancel()
		_ = hold.Rollback(ctx)
		if err != nil {
			t.Fatal("PTI save blocked behind unrelated payroll", err)
		}
		after, updatedReports := read()
		if before != after || !samePayrollProjection(reports, updatedReports) {
			t.Fatal("inspection-only changes invalidated payroll or changed financial values")
		}
		if count, err := repo.DispatchPayrollChanges(ctx); err != nil || count != 0 {
			t.Fatal("inspection changes queued payroll preparation", count, err)
		}
		var afterPointers, afterJobs string
		var revision int
		if err := pool.QueryRow(ctx, `SELECT (SELECT jsonb_agg(to_jsonb(s) ORDER BY kind,statement_id,week_start)::text FROM payroll_statement_state s),
 (SELECT jsonb_agg(to_jsonb(j) ORDER BY week_start)::text FROM payroll_projection_jobs j),
 (SELECT revision FROM payroll_cache_revisions WHERE table_name='weekly_pti_rows')`).Scan(&afterPointers, &afterJobs, &revision); err != nil || pointers != afterPointers || jobs != afterJobs || revision < 2 {
			t.Fatal("inspection changes must retain ordinary revisions without rewriting payroll", revision, err)
		}
		for _, kind := range []string{"driver", "investor"} {
			page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			if err != nil || page.State != "ready" {
				t.Fatal("inspection changes left statements Updating", kind, page.State, err)
			}
		}
		if _, err = pool.Exec(ctx, `UPDATE truck_settlement_terms SET share_percent=80 WHERE truck_id=$1`, projectionTruck); err != nil {
			t.Fatal(err)
		}
		after, _ = read()
		if after == before {
			t.Fatal("financial truck changes stopped invalidating payroll")
		}
	})
}
