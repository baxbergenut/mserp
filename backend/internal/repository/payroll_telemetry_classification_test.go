package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollTelemetryClassificationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		read := func() (string, [2]DriverPayWeek) {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if err = payrollProjectionCoverage(ctx, tx); err != nil {
				t.Fatal(err)
			}
			snapshot, err := payrollCacheSnapshot(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			reports, err := readPayrollProjectionReference(ctx, tx, week, true)
			if err != nil {
				t.Fatal(err)
			}
			return snapshot, reports
		}
		before, reportsBefore := read()
		for _, mutation := range []string{
			`INSERT INTO maintenance_telemetry(truck_id,vin,mileage,reported_at) VALUES('18200000-0000-0000-0000-000000000002','TELEMETRY',100,now())`,
			`UPDATE maintenance_telemetry SET mileage=200,checked_at=clock_timestamp(),reported_at=clock_timestamp()`,
			`DELETE FROM maintenance_telemetry`,
		} {
			if _, err := pool.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			after, reportsAfter := read()
			if before != after || !samePayrollProjection(reportsBefore, reportsAfter) {
				t.Fatal("maintenance telemetry changed payroll inputs or financial reports")
			}
		}
		var events, revision int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM payroll_projection_changes),
 (SELECT revision FROM payroll_cache_revisions WHERE table_name='maintenance_telemetry')`).Scan(&events, &revision); err != nil || events != 0 || revision < 3 {
			t.Fatal("telemetry must retain ordinary revisions without capturing payroll events", events, revision, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE truck_settlement_terms SET share_percent=80 WHERE truck_id='18200000-0000-0000-0000-000000000002'`); err != nil {
			t.Fatal(err)
		}
		after, _ := read()
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_changes WHERE table_name='truck_settlement_terms'`).Scan(&events); err != nil || events != 1 || before == after {
			t.Fatal("actual financial source must still invalidate and capture", events, err)
		}
	})
}
