package repository

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollSourceGateDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if err := NewDriverPayRepository(pool).EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='dual' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		hold, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Release()
		if _, err = hold.Exec(ctx, `SELECT pg_advisory_lock_shared(736281940)`); err != nil {
			t.Fatal(err)
		}
		held := true
		unlock := func() {
			if held {
				_, _ = hold.Exec(ctx, `SELECT pg_advisory_unlock_shared(736281940)`)
				held = false
			}
		}
		defer unlock()
		done := make(chan error, 1)
		go func() { done <- NewTollRepository(pool).ReconcileTruckAssignments(work) }()
		waiting := false
		deadline := time.Now().Add(2 * time.Second)
		for !waiting && time.Now().Before(deadline) {
			if err = admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event='advisory' AND query LIKE 'DO $payroll_source$%')`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if !waiting {
				time.Sleep(10 * time.Millisecond)
			}
		}
		if !waiting {
			t.Fatal("source writer did not wait at the protocol entry")
		}
		probe, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var available bool
		err = probe.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(730077)`).Scan(&available)
		_ = probe.Rollback(ctx)
		if err != nil || !available {
			t.Fatal("source writer took the toll mapping lock before the payroll gate", available, err)
		}
		unlock()
		if err := <-done; err != nil {
			t.Fatal("source writer did not finish after the gate opened", err)
		}
	})
}

func TestPayrollResourceGuardsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewDriverPayRepository(pool)
		repo.resourceLockAudit = true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='dual' WHERE singleton;
 INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES('18200000-0000-0000-0000-000000000001','2026-10-05')`); err != nil {
			t.Fatal(err)
		}
		epoch := func() int64 {
			t.Helper()
			var revision int64
			if err := pool.QueryRow(ctx, `SELECT revision FROM payroll_resource_revisions WHERE resource_key=$1`, "statement:driver:"+projectionDriver+":2026-10-05").Scan(&revision); err != nil {
				t.Fatal(err)
			}
			return revision
		}
		before := epoch()
		assertRejected := func(err error) {
			t.Helper()
			var database *pgconn.PgError
			if !errors.As(err, &database) || database.Code != "40001" {
				t.Fatal("expected database resource rejection", err)
			}
		}
		// A supplied claim is not proof of a lock. The database checks pg_locks.
		for _, gate := range []bool{false, true} {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if gate {
				if err = lockTruckAccounting(ctx, tx); err != nil {
					t.Fatal(err)
				}
			}
			claim, _ := json.Marshal([]string{"driver:" + projectionDriver})
			if _, err = tx.Exec(ctx, `SELECT set_config('mserp.payroll.resources',$1,true),set_config('mserp.payroll.protocol_epoch','1',true)`, string(claim)); err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `UPDATE driver_pay_weeks SET notes='Invalid claim' WHERE driver_id=$1`, projectionDriver)
			assertRejected(err)
			_ = tx.Rollback(ctx)
		}
		if epoch() != before {
			t.Fatal("rejected mutation advanced resource revisions")
		}
		for _, commit := range []bool{false, true} {
			tx, closeWrite, err := repo.beginPayrollWrite(ctx, "driver:"+projectionDriver)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, `UPDATE driver_pay_weeks SET notes='Valid claim',version=version+1 WHERE driver_id=$1`, projectionDriver); err != nil {
				closeWrite()
				t.Fatal(err)
			}
			if commit {
				if err = tx.Commit(ctx); err != nil {
					closeWrite()
					t.Fatal(err)
				}
			}
			closeWrite()
			want := before
			if commit {
				want++
			}
			if got := epoch(); got != want {
				t.Fatal("resource revision did not follow canonical commit", got, want)
			}
		}
		// Even a valid financial claim cannot mutate source profiles.
		tx, closeWrite, err := repo.beginPayrollWrite(ctx, "driver:"+projectionDriver)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, `UPDATE drivers SET pay_rate=0.99 WHERE id=$1`, projectionDriver)
		assertRejected(err)
		closeWrite()
		// Removed guards disable prepared publication, independently of the queue.
		if _, err = admin.Exec(ctx, `ALTER TABLE driver_pay_weeks DISABLE TRIGGER payroll_protocol_row`); err != nil {
			t.Fatal(err)
		}
		read, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = payrollProjectionCoverage(ctx, read)
		_ = read.Rollback(ctx)
		if !errors.Is(err, ErrPayrollProjectionCoverage) {
			t.Fatal("disabled protocol guard was accepted", err)
		}
	})
}

func TestPayrollResourceDependenciesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const independent = "18200000-0000-0000-0000-000000002201"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000002201','Independent dependencies','independent dependencies','cpm',0.65,'2026-01-01');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000002201','2026-10-05','DEPENDENCIES',200,300,120);
 INSERT INTO driver_pay_weeks(driver_id,week_start,fuel_override)
 VALUES('18200000-0000-0000-0000-000000000001','2026-09-28',0)`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='dual' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		prepare := func() PreparedPayrollPage {
			t.Helper()
			if err := repo.RequestPayrollPreparation(ctx, week, true); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
			page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
			if err != nil || page.State != "ready" {
				t.Fatal("prepared resource dependencies", page.State, err)
			}
			return page
		}
		for _, changed := range []string{"statement:driver:" + projectionDriver + ":2026-10-05", "statement:driver:" + projectionDriver + ":2026-09-28"} {
			page := prepare()
			var tracked bool
			if err := pool.QueryRow(ctx, `SELECT resource_dependencies ? $2 FROM payroll_statement_versions WHERE id=$1::uuid`, page.Statements[projectionDriver].Revision, changed).Scan(&tracked); err != nil || !tracked {
				t.Fatal("statement dependency missing", changed, tracked, err)
			}
			// Deliberately change only a resource epoch: matching global counters
			// and an empty queue must not certify stale financial inputs.
			if _, err := pool.Exec(ctx, `INSERT INTO payroll_resource_revisions(resource_key,revision) VALUES($1,1)
 ON CONFLICT(resource_key) DO UPDATE SET revision=payroll_resource_revisions.revision+1`, changed); err != nil {
				t.Fatal(err)
			}
			current, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
			if err != nil || current.State != "updating" {
				t.Fatal("matching table revisions hid a changed resource", current.State, err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = validatePreparedPayrollCommand(ctx, tx, "driver", projectionDriver, "2026-10-05", page.Statements[projectionDriver].Revision)
			if !errors.Is(err, ErrDriverPayConflict) {
				_ = tx.Rollback(ctx)
				t.Fatal("stale resource accepted by financial command", err)
			}
			if err = validatePreparedPayrollCommand(ctx, tx, "driver", independent, "2026-10-05", page.Statements[independent].Revision); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal("unrelated financial command invalidated", err)
			}
			snapshot, err := payrollCacheSnapshot(ctx, tx)
			if err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(err)
			}
			_, err = preparedSettlementReport(ctx, tx, &payrollCommand{beforeSnapshot: snapshot}, "driver", week, "")
			_ = tx.Rollback(ctx)
			if !errors.Is(err, ErrDriverPayConflict) {
				t.Fatal("whole-week finalization accepted a stale resource", err)
			}
		}
	})
}

// Protocol primitives only: these checks do not claim that financial commands
// have switched away from the existing global protocol.
func TestPayrollResourcesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const independent = "18200000-0000-0000-0000-000000002101"
		const expense = "18200000-0000-0000-0000-000000002102"
		const escrow = "18200000-0000-0000-0000-000000002103"
		const schedule = "18200000-0000-0000-0000-000000002104"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000002101','Independent resource','independent resource','cpm',0.65,'2026-01-01');
 INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT '18200000-0000-0000-0000-000000002102',id,name,'Resource test','2026-10-05',100,'18200000-0000-0000-0000-000000000001','Driver' FROM expense_settings WHERE kind='category' AND name='Other';
 INSERT INTO expense_payments(expense_id,week_start,amount) VALUES('18200000-0000-0000-0000-000000002102','2027-01-04',20);
 INSERT INTO driver_escrows(id,driver_id,driver_name,start_date,amount,opening_paid)
 VALUES('18200000-0000-0000-0000-000000002103','18200000-0000-0000-0000-000000000001','Projection Driver','2026-10-05',300,50);
 INSERT INTO driver_charge_schedules(id,driver_id,kind,name,direction,start_week,eligibility,total)
 VALUES('18200000-0000-0000-0000-000000002104','18200000-0000-0000-0000-000000000001','installment','Resource advance','charge','2026-10-05','calendar',125)`); err != nil {
			t.Fatal(err)
		}
		discover := func(roots ...string) *payrollResourceSet {
			t.Helper()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			set, err := discoverPayrollResources(ctx, tx, roots)
			_ = tx.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return set
		}
		set := discover("driver:" + projectionDriver)
		for _, key := range []string{"driver:" + projectionDriver, "truck:" + projectionTruck, "owner:18200000-0000-0000-0000-000000000003", "expense:" + expense, "escrow:" + escrow, "schedule:" + schedule} {
			if !slices.Contains(set.keys, key) {
				t.Fatal("missing cross-week financial resource", key, set.keys)
			}
		}
		if other := discover("truck:" + projectionTruck); !slices.Equal(set.keys, other.keys) {
			t.Fatal("shared driver/investor resources differ", set.keys, other.keys)
		}
		separate := discover("driver:" + independent)
		for _, key := range separate.keys {
			if slices.Contains(set.keys, key) {
				t.Fatal("unrelated resources overlap", key)
			}
		}
		first, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer first.Release()
		release, err := acquirePayrollResources(ctx, first, set)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		second, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer second.Release()
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		unlock, err := acquirePayrollResources(bounded, second, separate)
		if err != nil {
			t.Fatal("independent resources blocked", err)
		}
		unlock()
		blocked, cancelBlocked := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancelBlocked()
		if _, err := acquirePayrollResources(blocked, second, set); err == nil || !second.Conn().IsClosed() {
			t.Fatal("shared resources were not blocked or canceled connection leaked locks", err)
		}
		// A new principal created between discovery and the new financial snapshot
		// must fail revalidation. No automatic posting against the expanded set.
		if _, err := pool.Exec(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'New scope input','2026-10-05',15,$1,'Driver' FROM expense_settings WHERE kind='category' AND name='Other'`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		tx, err := first.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := revalidatePayrollResources(ctx, tx, []string{"driver:" + projectionDriver}, set); !errors.Is(err, ErrPayrollResourceChanged) {
			t.Fatal("changed selection scope was accepted", err)
		}
	})
}
