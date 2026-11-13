package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

const projectionDriver = "18200000-0000-0000-0000-000000000001"
const projectionTruck = "18200000-0000-0000-0000-000000000002"

func withPayrollProjectionDatabase(t *testing.T, run func(*testing.T, context.Context, *pgxpool.Pool, *pgx.Conn)) {
	t.Helper()
	dsn := os.Getenv("MSERP_DRIVER_PAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable MSERP_DRIVER_PAY_TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	// Keep the shared extension outside disposable schemas, including when a
	// failed fixture leaves a schema for diagnosis.
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/182_payroll_projections.sql")
	if err != nil {
		t.Fatal(err)
	}
	telemetryMigration, err := os.ReadFile("../../sql/183_payroll_telemetry_classification.sql")
	if err != nil {
		t.Fatal(err)
	}
	operationalMigration, err := os.ReadFile("../../sql/184_payroll_operational_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	sharedMigration, err := os.ReadFile("../../sql/185_payroll_shared_dependencies.sql")
	if err != nil {
		t.Fatal(err)
	}
	verificationMigration, err := os.ReadFile("../../sql/186_expense_verification_metadata.sql")
	if err != nil {
		t.Fatal(err)
	}
	availabilityMigration, err := os.ReadFile("../../sql/187_load_source_availability.sql")
	if err != nil {
		t.Fatal(err)
	}
	boardMigration, err := os.ReadFile("../../sql/188_status_board_operational.sql")
	if err != nil {
		t.Fatal(err)
	}
	modifiedSyncMigration, err := os.ReadFile("../../sql/196_datatruck_modified_sync.sql")
	if err != nil {
		t.Fatal(err)
	}
	routeUsageMigration, err := os.ReadFile("../../sql/197_board_route_usage.sql")
	if err != nil {
		t.Fatal(err)
	}
	accessorialMigration, err := os.ReadFile("../../sql/203_accessorial_expenses.sql")
	if err != nil {
		t.Fatal(err)
	}
	escrowMigration, err := os.ReadFile("../../sql/204_driver_pay_escrow_settlement.sql")
	if err != nil {
		t.Fatal(err)
	}
	planningMigration, err := os.ReadFile("../../sql/206_gross_board_planning_entries.sql")
	if err != nil {
		t.Fatal(err)
	}
	stopGeocodeMigration, err := os.ReadFile("../../sql/216_status_board_stop_geocoding.sql")
	if err != nil {
		t.Fatal(err)
	}
	readyLocationMigration, err := os.ReadFile("../../sql/207_ready_location.sql")
	if err != nil {
		t.Fatal(err)
	}
	escrowBalanceMigration, err := os.ReadFile("../../sql/209_escrow_balance_reductions.sql")
	if err != nil {
		t.Fatal(err)
	}
	escrowAdjustmentMigration, err := os.ReadFile("../../sql/210_escrow_balance_adjustments.sql")
	if err != nil {
		t.Fatal(err)
	}
	weekLockMigration, err := os.ReadFile("../../sql/211_gross_board_week_lock.sql")
	if err != nil {
		t.Fatal(err)
	}
	weekLockActorMigration, err := os.ReadFile("../../sql/212_gross_board_week_lock_actor.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "incremental"} {
		t.Run(mode, func(t *testing.T) {
			name := fmt.Sprintf("payroll_projection_%d", time.Now().UnixNano())
			schema := pgx.Identifier{name}.Sanitize()
			exec := func(sql string) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + schema + `; SET search_path TO ` + schema + `,public`)
			defer admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+schema+` CASCADE`)
			body := strings.ReplaceAll(string(source), "\r\n", "\n")
			if mode == "incremental" {
				upgrade := strings.ReplaceAll(string(migration), "\r\n", "\n")
				if !strings.Contains(body, upgrade) {
					t.Fatal("fresh schema does not contain the exact migration")
				}
				prior := strings.Replace(body, strings.ReplaceAll(string(telemetryMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(operationalMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(sharedMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(verificationMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(availabilityMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(boardMigration), "\r\n", "\n"), "", 1)
				storageMigration, storageErr := os.ReadFile("../../sql/189_payroll_current_storage.sql")
				if storageErr != nil {
					t.Fatal(storageErr)
				}
				prior = strings.Replace(prior, strings.ReplaceAll(string(storageMigration), "\r\n", "\n"), "", 1)
				prior = withoutUnrelatedPayrollMigration(t, withoutPTIPayrollMigration(t, prior))
				prior = strings.Replace(prior, strings.ReplaceAll(string(modifiedSyncMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(routeUsageMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(accessorialMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(escrowMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(planningMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(stopGeocodeMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(readyLocationMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(escrowBalanceMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(escrowAdjustmentMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(weekLockMigration), "\r\n", "\n"), "", 1)
				prior = strings.Replace(prior, strings.ReplaceAll(string(weekLockActorMigration), "\r\n", "\n"), "", 1)
				exec(strings.Replace(prior, upgrade, "", 1))
				exec(string(migration))
				exec(string(telemetryMigration))
				exec(string(operationalMigration))
				exec(string(sharedMigration))
				exec(string(verificationMigration))
				exec(string(availabilityMigration))
				exec(string(boardMigration))
				exec(string(storageMigration))
				applyPTIPayrollMigration(t, ctx, admin)
				applyUnrelatedPayrollMigration(t, ctx, admin)
				exec(string(modifiedSyncMigration))
				exec(string(routeUsageMigration))
				exec(string(accessorialMigration))
				exec(string(escrowMigration))
				exec(string(planningMigration))
				exec(string(readyLocationMigration))
				exec(string(escrowBalanceMigration))
				exec(string(escrowAdjustmentMigration))
				exec(string(weekLockMigration))
				exec(string(weekLockActorMigration))
				exec(string(stopGeocodeMigration))
			} else {
				exec(body)
			}
			exec(`GRANT USAGE ON SCHEMA ` + schema + ` TO mserp_app`)
			var wrongOwner int
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()
 AND tablename IN ('payroll_projection_control','payroll_projection_sources','payroll_projection_changes',
 'payroll_projection_jobs','payroll_projection_generations','payroll_statement_versions','payroll_statement_state',
 'payroll_projection_weeks','payroll_command_receipts','payroll_command_versions') AND tableowner<>'mserp_app'`).Scan(&wrongOwner); err != nil || wrongOwner != 0 {
				t.Fatal("new runtime tables missing application ownership", wrongOwner, err)
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + schema + ` TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + schema + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.Tracer = db.Tracer{}
			app.ConnConfig.RuntimeParams["search_path"] = name + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, app)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			run(t, ctx, pool, admin)
		})
	}
}

func seedPayrollProjectionTest(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000000001','Projection Driver','projection driver','cpm',0.75,'2026-01-01');
 INSERT INTO investors(id,full_name) VALUES('18200000-0000-0000-0000-000000000003','Projection Investor');
 INSERT INTO trucks(id,unit_number,owner_id) VALUES('18200000-0000-0000-0000-000000000002','PROJECTION','18200000-0000-0000-0000-000000000003');
 UPDATE truck_ownership_history SET start_known=false;
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at)
 VALUES('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000000001','2026-01-01');
 INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent)
 VALUES('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000000003','2026-01-05',88);
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000000001','2026-10-05','PROJECTION-1',1000,1200,400);
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,raw_payload)
 VALUES(18201,'PROJECTION-1','delivered',1000,1200,400,'2026-10-05 12:00Z','{}');`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPayrollProjectionDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		repo := NewDriverPayRepository(pool)
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		count := func(sql string) int64 {
			t.Helper()
			var n int64
			if err := pool.QueryRow(ctx, sql).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		if count(`SELECT count(*) FROM payroll_projection_changes`) != 0 {
			t.Fatal("capture enabled by migration alone")
		}
		oldWriter, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer oldWriter.Rollback(ctx)
		var wasEnabled bool
		if err = oldWriter.QueryRow(ctx, `SELECT capture_enabled FROM payroll_projection_control`).Scan(&wasEnabled); err != nil || wasEnabled {
			t.Fatal("capture activation fixture", err)
		}
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = oldWriter.Exec(ctx, `UPDATE loads SET total_pay=total_pay+1 WHERE id=18201`)
		var activationErr *pgconn.PgError
		if !errors.As(err, &activationErr) || activationErr.Code != "40001" {
			t.Fatal("writer with disabled-capture snapshot bypassed activation fence", err)
		}
		_ = oldWriter.Rollback(ctx)
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		t.Run("capture_commit_rollback_and_privacy", func(t *testing.T) {
			exec(`UPDATE loads SET synced_at=now() WHERE id=18201`)
			if count(`SELECT count(*) FROM payroll_projection_changes`) != 0 {
				t.Fatal("timestamp-only sync captured")
			}
			exec(`BEGIN; UPDATE loads SET total_pay=total_pay+1 WHERE id=18201; ROLLBACK`)
			if count(`SELECT count(*) FROM payroll_projection_changes`) != 0 {
				t.Fatal("rolled-back event retained")
			}
			exec(`UPDATE loads SET pickup_time='2026-10-06',raw_payload='{"private":"never copy this"}' WHERE id=18201`)
			var before, after string
			if err := pool.QueryRow(ctx, `SELECT old_routes::text,new_routes::text FROM payroll_projection_changes WHERE table_name='loads'`).Scan(&before, &after); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(before, "2026-10-05") || !strings.Contains(after, "2026-10-06") || strings.Contains(after, "private") {
				t.Fatalf("routing capture %s %s", before, after)
			}
			if n, err := repo.DispatchPayrollChanges(ctx); err != nil || n != 1 {
				t.Fatal(n, err)
			}
		})
		t.Run("publish_both_pages_and_restart", func(t *testing.T) {
			if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
				t.Fatal(worked, err)
			}
			if count(`SELECT count(*) FROM payroll_projection_generations`) != 1 || count(`SELECT count(*) FROM payroll_statement_state`) != 2 {
				t.Fatal("both pages were not published atomically")
			}
			if count(`SELECT count(*) FROM payroll_projection_changes WHERE dispatched_at IS NULL`) != 0 {
				t.Fatal("derived publication recursively invalidated itself")
			}
			restarted := NewDriverPayRepository(pool)
			for _, kind := range []string{"driver", "investor"} {
				for _, query := range []PayPageQuery{{Summary: true}, {}, {Summary: true, DetailIDs: map[string]bool{projectionDriver: true, projectionTruck: true}}} {
					measured, metrics := db.WithQueryMetrics(ctx)
					got, err := restarted.ReadPreparedPayrollPage(measured, kind, week, query)
					if err != nil || got.State != "ready" {
						t.Fatalf("prepared %s: %s %v", kind, got.State, err)
					}
					phases, _ := metrics.Details()
					for phase := range phases {
						if strings.Contains(phase, "calculation") {
							t.Fatal("prepared read executed payroll calculator", phases)
						}
					}
					want, err := repo.PayrollPage(ctx, kind, week, query)
					if err != nil {
						t.Fatal(err)
					}
					a, _ := json.Marshal(got.Report)
					b, _ := json.Marshal(want)
					if string(a) != string(b) {
						t.Fatalf("prepared %s differs\ngot %s\nwant %s", kind, a, b)
					}
				}
			}
		})
		t.Run("freshness_is_not_queue_state", func(t *testing.T) {
			exec(`UPDATE loads SET total_pay=total_pay+10 WHERE id=18201`)
			got, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
			if err != nil || got.State != "updating" || len(got.Report.Drivers) != 1 {
				t.Fatal("stale values incorrectly current or discarded", got.State, err)
			}
			if count(`SELECT count(*) FROM payroll_projection_jobs WHERE completed_generation=requested_generation`) != 1 {
				t.Fatal("test must observe source commit before dispatch")
			}
			if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
				t.Fatal(err)
			}
			job, err := repo.claimPayrollProjection(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repo.claimPayrollProjection(ctx); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal("active lease claimed twice", err)
			}
			exec(`UPDATE payroll_projection_jobs SET lease_until=clock_timestamp()-interval '1 second'`)
			reclaimed, err := repo.claimPayrollProjection(ctx)
			if err != nil || reclaimed.Token == job.Token {
				t.Fatal("expired lease not reclaimed", err)
			}
			if err = repo.preparePayrollProjection(ctx, job); !errors.Is(err, ErrPayrollProjectionSuperseded) {
				t.Fatal("stale worker published", err)
			}
			if err = repo.preparePayrollProjection(ctx, reclaimed); err != nil {
				t.Fatal(err)
			}
			if count(`SELECT count(*) FROM payroll_projection_generations`) != 2 {
				t.Fatal("failed worker left partial publication")
			}
		})
		t.Run("late_commit_lower_outbox_id", func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, `UPDATE loads SET total_pay=total_pay+1 WHERE id=18201`); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE investors SET notes='out of order test' WHERE id='18200000-0000-0000-0000-000000000003'`)
			if n, err := repo.DispatchPayrollChanges(ctx); err != nil || n < 1 {
				t.Fatal(n, err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if n, err := repo.DispatchPayrollChanges(ctx); err != nil || n != 1 {
				t.Fatal("late committed event lost", n, err)
			}
		})
		t.Run("coverage_rejects_unknown_or_disabled_inputs", func(t *testing.T) {
			for _, sql := range []string{`CREATE TABLE unclassified_payroll_source(id int)`, `ALTER TABLE loads DISABLE TRIGGER payroll_projection_update`, `ALTER TABLE loads DISABLE TRIGGER payroll_cache_update`, `ALTER TABLE app_users DISABLE TRIGGER zz_payroll_user_metadata`, `ALTER TABLE tolls DISABLE TRIGGER payroll_cache_update`} {
				tx, err := admin.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
				err = payrollProjectionCoverage(ctx, tx)
				_ = tx.Rollback(ctx)
				if !errors.Is(err, ErrPayrollProjectionCoverage) {
					t.Fatal("unsafe capture accepted", sql, err)
				}
			}
		})
		t.Run("source_commit_during_publication", func(t *testing.T) {
			job, err := repo.claimPayrollProjection(ctx)
			if err != nil {
				t.Fatal(err)
			}
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(ctx)
			if _, err = blocker.Exec(ctx, `SELECT revision FROM payroll_cache_revisions WHERE table_name='loads' FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			workCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- repo.preparePayrollProjection(workCtx, job) }()
			deadline := time.Now().Add(5 * time.Second)
			locked := false
			for time.Now().Before(deadline) {
				if err = admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'SELECT table_name,revision FROM payroll_cache_revisions%')`).Scan(&locked); err != nil {
					t.Fatal(err)
				}
				if locked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !locked {
				t.Fatal("worker did not reach revision publication fence")
			}
			if _, err = blocker.Exec(ctx, `UPDATE loads SET total_pay=total_pay+1 WHERE id=18201`); err != nil {
				t.Fatal(err)
			}
			if err = blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-done
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "40001" {
				t.Fatal("concurrent source commit was published as current", err)
			}
			if count(`SELECT count(*) FROM payroll_projection_generations`) != 2 {
				t.Fatal("aborted publication retained rows")
			}
			if err = repo.failPayrollProjection(ctx, job, err); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("backfill_has_no_financial_side_effects", func(t *testing.T) {
			if err := repo.SeedPayrollPreparation(ctx); err != nil {
				t.Fatal(err)
			}
			if count(`SELECT count(*) FROM payroll_projection_jobs`) < 9 {
				t.Fatal("recent/current backfill missing")
			}
			if count(`SELECT (SELECT count(*) FROM driver_pay_weeks)+(SELECT count(*) FROM investor_pay_weeks)+(SELECT count(*) FROM payroll_settlements)+(SELECT count(*) FROM expense_payments)+(SELECT count(*) FROM driver_escrow_payments)`) != 0 {
				t.Fatal("preparation wrote canonical financial records")
			}
		})
		t.Run("frozen_snapshots_survive_source_changes", func(t *testing.T) {
			var actor string
			if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Projection Test',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"driver", "investor"} {
				live, err := repo.PayrollPreview(ctx, kind, week)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "driver" {
					_, err = repo.Settle(ctx, week, projectionDriver, live.Revision, actor, "", false, true)
				} else {
					_, err = repo.SettleInvestor(ctx, week, projectionTruck, live.Revision, actor, "", false, true)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			var frozenDriver, frozenInvestor []byte
			if err := pool.QueryRow(ctx, `SELECT report FROM payroll_settlements WHERE driver_id=$1`, projectionDriver).Scan(&frozenDriver); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT report FROM investor_pay_weeks WHERE truck_id=$1`, projectionTruck).Scan(&frozenInvestor); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE loads SET total_pay=9000,total_miles=9000 WHERE id=18201`)
			if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
				t.Fatal(err)
			}
			if err := repo.RequestPayrollPreparation(ctx, week, true); err != nil {
				t.Fatal(err)
			}
			if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
				t.Fatal(worked, err)
			}
			for i, kind := range []string{"driver", "investor"} {
				got, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
				if err != nil || got.State != "ready" || len(got.Report.Drivers) != 1 {
					t.Fatal(kind, got.State, err)
				}
				var frozen DriverPayDriver
				if err = json.Unmarshal([][]byte{frozenDriver, frozenInvestor}[i], &frozen); err != nil {
					t.Fatal(err)
				}
				row := got.Report.Drivers[0]
				if row.Settlement == nil || !row.Settlement.Finalized || row.Loads[0].Fee != frozen.Loads[0].Fee || row.Loads[0].TotalMiles != frozen.Loads[0].TotalMiles {
					t.Fatal("frozen finances rebuilt from live source", kind)
				}
			}
			var unchanged bool
			if err := pool.QueryRow(ctx, `SELECT (SELECT report=$1::jsonb FROM payroll_settlements WHERE driver_id=$3) AND
 (SELECT report=$2::jsonb FROM investor_pay_weeks WHERE truck_id=$4)`, frozenDriver, frozenInvestor, projectionDriver, projectionTruck).Scan(&unchanged); err != nil || !unchanged {
				t.Fatal("shadow preparation changed canonical frozen JSON", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE payroll_statement_versions SET payable_cents=0`); err == nil {
				t.Fatal("published history is mutable")
			}
		})
	})
}
