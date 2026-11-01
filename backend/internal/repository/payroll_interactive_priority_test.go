package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollInteractivePriorityDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		repo := NewDriverPayRepository(pool)
		visible, _ := chargeWeek("2026-09-28")
		if _, err := pool.Exec(ctx, `INSERT INTO payroll_projection_jobs(week_start,priority) VALUES('2026-09-28',0),('2026-10-26',90)`); err != nil {
			t.Fatal(err)
		}
		if err := repo.prioritizePayrollWeek(ctx, visible); err != nil {
			t.Fatal(err)
		}
		var before, after string
		const state = `SELECT jsonb_agg(to_jsonb(j) ORDER BY week_start)::text FROM payroll_projection_jobs j`
		if err := pool.QueryRow(ctx, state).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if err := repo.prioritizePayrollWeek(ctx, visible); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, state).Scan(&after); err != nil || before != after {
			t.Fatal("rapid polling rewrote scheduling state", err)
		}
		job, err := repo.claimPayrollProjection(ctx)
		if err != nil || !job.Week.Equal(visible) || job.Generation != 1 {
			t.Fatal("visible week did not precede routine current week", job, err)
		}
		// A long calculation outlives the five-second interest refresh. The
		// ordinary stale-page poll must not create a new row version underneath
		// the worker's repeatable-read snapshot, even for an identical UPDATE.
		if _, err = pool.Exec(ctx, `UPDATE payroll_projection_jobs SET interactive_until=clock_timestamp()-interval '1 second' WHERE week_start=$1`, visible); err != nil {
			t.Fatal(err)
		}
		worker, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer worker.Rollback(ctx)
		var generation int64
		if err = worker.QueryRow(ctx, `SELECT requested_generation FROM payroll_projection_jobs WHERE week_start=$1`, visible).Scan(&generation); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if err = repo.RequestPayrollPreparation(ctx, visible, false); err != nil {
				t.Fatal(err)
			}
			if err = repo.prioritizePayrollWeek(ctx, visible); err != nil {
				t.Fatal(err)
			}
		}
		if err = worker.QueryRow(ctx, `SELECT requested_generation FROM payroll_projection_jobs WHERE week_start=$1 FOR UPDATE`, visible).Scan(&generation); err != nil || generation != job.Generation {
			t.Fatal("page polling invalidated the active worker snapshot", err, generation)
		}
		if err = worker.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err = repo.RequestPayrollPreparation(ctx, visible, true); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT requested_generation FROM payroll_projection_jobs WHERE week_start=$1`, visible).Scan(&generation); err != nil || generation != job.Generation+1 {
			t.Fatal("explicit retry no longer supersedes pending work", err, generation)
		}
		if _, err = pool.Exec(ctx, `UPDATE payroll_projection_jobs SET lease_token=NULL,lease_until=NULL,priority=0,interactive_until=clock_timestamp()-interval '1 second' WHERE week_start='2026-09-28'`); err != nil {
			t.Fatal(err)
		}
		job, err = repo.claimPayrollProjection(ctx)
		if err != nil || job.Week.Format("2006-01-02") != "2026-10-26" {
			t.Fatal("expired interest retained priority", job, err)
		}
		var changes int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_changes`).Scan(&changes); err != nil || changes != 0 {
			t.Fatal("priority generated source work", changes, err)
		}
	})
}
