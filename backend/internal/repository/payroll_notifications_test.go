package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollCommittedNotificationsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `LISTEN payroll_projection_visible`); err != nil {
			t.Fatal(err)
		}
		wait := func(want bool) {
			t.Helper()
			duration := 150 * time.Millisecond
			if want {
				duration = 2 * time.Second
			}
			deadline, cancel := context.WithTimeout(ctx, duration)
			defer cancel()
			n, err := admin.WaitForNotification(deadline)
			if want && (err != nil || n == nil || n.Channel != "payroll_projection_visible" || n.Payload != "") {
				t.Fatalf("commit hint: %v %v", n, err)
			}
			if !want && (n != nil || deadline.Err() == nil) {
				t.Fatalf("uncommitted notification: %v %v", n, err)
			}
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `UPDATE loads SET total_pay=total_pay+1 WHERE id=18201`); err != nil {
			t.Fatal(err)
		}
		wait(false)
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		wait(false)
		if _, err = pool.Exec(ctx, `UPDATE loads SET total_pay=total_pay+1 WHERE id=18201`); err != nil {
			t.Fatal(err)
		}
		wait(true)
		week, _ := chargeWeek("2026-10-05")
		if err = repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		var before, after int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_changes`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
			t.Fatal("publication", worked, err)
		}
		wait(true)
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_changes`).Scan(&after); err != nil || before != after {
			t.Fatal("publication recursively captured source work", before, after, err)
		}
		if err = repo.RequestPayrollPreparation(ctx, week, true); err != nil {
			t.Fatal(err)
		}
		job, err := repo.claimPayrollProjection(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.failPayrollProjection(ctx, job, errors.New("synthetic preparation failure")); err != nil {
			t.Fatal(err)
		}
		wait(true)
		page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil || page.State != "failed" {
			t.Fatal("failure notification did not expose a retryable state", page.State, err)
		}
		if err = repo.failPayrollProjection(ctx, job, ErrPayrollProjectionSuperseded); err != nil {
			t.Fatal(err)
		}
		wait(false)
	})
}
