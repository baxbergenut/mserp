package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollWorkPriority(t *testing.T) {
	priority := NewPayrollWorkPriority()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	work, release, err := priority.background(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	first := priority.interactive()
	second := priority.interactive()
	defer first()
	defer second()
	if !errors.Is(context.Cause(work), errPayrollWorkYielded) {
		t.Fatal("background work did not yield")
	}
	started := make(chan time.Time, 1)
	go func() {
		_, finish, err := priority.background(ctx)
		if err == nil {
			started <- time.Now()
			finish()
		}
	}()
	first()
	select {
	case <-started:
		t.Fatal("worker ran during another edit")
	case <-time.After(50 * time.Millisecond):
	}
	finished := time.Now()
	second()
	select {
	case at := <-started:
		if at.Sub(finished) < 450*time.Millisecond {
			t.Fatal("worker did not coalesce the edit burst")
		}
	case <-ctx.Done():
		t.Fatal("worker did not resume after edits")
	}
}

func TestGrossBoardPreemptsPayrollDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		var before int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_generations`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if err := pay.RequestPayrollPreparation(ctx, week, true); err != nil {
			t.Fatal(err)
		}
		// Freeze publication while the real worker holds the shared source gate.
		// An interactive edit must finish before this test releases that barrier.
		if _, err := admin.Exec(ctx, `SELECT pg_advisory_lock(736281999);
 CREATE FUNCTION hold_gross_background() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(736281999); RETURN NEW; END $$;
 CREATE TRIGGER hold_gross_background BEFORE INSERT ON payroll_projection_generations FOR EACH ROW EXECUTE FUNCTION hold_gross_background()`); err != nil {
			t.Fatal(err)
		}
		defer admin.Exec(ctx, `SELECT pg_advisory_unlock(736281999)`)
		priority := NewPayrollWorkPriority()
		gross := NewGrossBoardRepository(pool)
		gross.ConfigurePayrollWorkPriority(priority)
		work, release, err := priority.background(ctx)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { defer release(); _, err := pay.PrepareNextPayrollWeek(work); done <- err }()
		deadline := time.Now().Add(5 * time.Second)
		blocked := false
		for time.Now().Before(deadline) {
			if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=736281999 AND NOT granted)`).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			t.Fatal("worker did not reach publication barrier")
		}
		writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		start := time.Now()
		saved, err := gross.SaveChanges(WithPayrollPublication(writeCtx), []GrossBoardEntry{{DriverID: projectionDriver, Date: "2026-10-05", DriverRate: "795", Fields: []string{"driverRate"}}}, "")
		cancel()
		if err != nil || len(saved) != 1 || saved[0].DriverRate != "795.00" {
			t.Fatal("interactive save waited for background publication", saved, err)
		}
		t.Logf("interactive write interrupted blocked payroll publication in %s", time.Since(start))
		select {
		case err := <-done:
			if !errors.Is(err, ErrPayrollProjectionSuperseded) {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("worker did not release its lease")
		}
		var after int
		var message string
		var leased bool
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_generations`).Scan(&after); err != nil || before != after {
			t.Fatal("interrupted worker published partial data", before, after, err)
		}
		if err := pool.QueryRow(ctx, `SELECT last_error,lease_token IS NOT NULL FROM payroll_projection_jobs WHERE week_start=$1`, week).Scan(&message, &leased); err != nil || message != "" || leased {
			t.Fatal("yield treated as payroll failure", message, leased, err)
		}
		if _, err := admin.Exec(ctx, `SELECT pg_advisory_unlock(736281999); DROP TRIGGER hold_gross_background ON payroll_projection_generations`); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.DispatchPayrollChanges(ctx); err != nil {
			t.Fatal(err)
		}
		resume, finish, err := priority.background(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		if worked, err := pay.PrepareNextPayrollWeek(resume); err != nil || !worked {
			t.Fatal("worker did not resume durable work", worked, err)
		}
		for _, kind := range []string{"driver", "investor"} {
			page, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			if err != nil || page.State != "ready" {
				t.Fatal("payroll did not catch up", kind, page.State, err)
			}
		}
	})
}
