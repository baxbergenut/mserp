package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollFleetScopeIdentityGateDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if err := NewDriverPayRepository(pool).EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		const operation = "18200000-0000-0000-0000-000000009701"
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton;
 UPDATE fleetscope_sync_state SET mode='paused';
 INSERT INTO fleetscope_sync_operations(id,entity_key,kind,company_id,environment,epoch,payload,body_sha256,state,last_http_status)
 VALUES('18200000-0000-0000-0000-000000009701','driver:18200000-0000-0000-0000-000000000001','driver.link',
 '18200000-0000-0000-0000-000000009702','test','18200000-0000-0000-0000-000000009703','{}','fixture','blocked',409);
 INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source,confirmed)
 VALUES('18200000-0000-0000-0000-000000009702','18200000-0000-0000-0000-000000009704',
 '18200000-0000-0000-0000-000000000001','review',false);`); err != nil {
			t.Fatal(err)
		}
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
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- (&FleetSyncRepository{pool: pool}).Supersede(work, operation, "") }()
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
			t.Fatal("identity writer did not enter the payroll gate before integration locks")
		}
		probe, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = probe.Exec(ctx, `SELECT singleton FROM fleetscope_sync_state FOR UPDATE NOWAIT`)
		_ = probe.Rollback(ctx)
		if err != nil {
			t.Fatal("blocked identity writer holds integration state row", err)
		}
		unlock()
		if err = <-done; err != nil {
			t.Fatal(err)
		}
		var state string
		var retained bool
		if err = pool.QueryRow(ctx, `SELECT state,EXISTS(SELECT 1 FROM fleetscope_driver_identity WHERE driver_id=$2::uuid)
 FROM fleetscope_sync_operations WHERE id=$1::uuid`, operation, projectionDriver).Scan(&state, &retained); err != nil || state != "superseded" || retained {
			t.Fatal("identity supersession did not finish atomically", state, retained, err)
		}
	})
}
