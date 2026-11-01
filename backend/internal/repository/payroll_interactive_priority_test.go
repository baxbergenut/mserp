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
		if _, err = pool.Exec(ctx, `UPDATE payroll_projection_jobs SET lease_token=NULL,lease_until=NULL,interactive_until=clock_timestamp()-interval '1 second' WHERE week_start='2026-09-28'`); err != nil {
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
