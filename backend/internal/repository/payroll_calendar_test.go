package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollCompletedWeekCalendar(t *testing.T) {
	for _, tc := range []struct{ now, week string }{
		{"2026-11-02T04:59:59Z", "2026-10-19"}, // Sunday after fall DST change.
		{"2026-11-02T05:00:00Z", "2026-10-26"},
		{"2026-03-09T03:59:59Z", "2026-02-23"}, // Sunday after spring DST change.
		{"2026-03-09T04:00:00Z", "2026-03-02"},
		{"2027-01-04T05:00:00Z", "2026-12-28"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		if got := payrollLatestWeekAt(now); got != tc.week {
			t.Fatalf("%s: %s, want %s", tc.now, got, tc.week)
		}
	}
}

func TestPayrollCompletedWeekDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		latest, _ := chargeWeek(PayrollLatestWeek())
		current, _ := chargeWeek(ChargeCurrentWeek())
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		// A completed statement plus a frequently edited current-week board row.
		exec(`UPDATE gross_board_entries SET service_date=$1 WHERE driver_id=$2`, latest, projectionDriver)
		exec(`UPDATE loads SET pickup_time=$1 WHERE id=18201`, latest.Add(12*time.Hour))
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES($1,$2,'CURRENT-ONLY',1500,1800,600)`, projectionDriver, current)
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		repo.preparedReads, repo.preparedCommands = true, true
		if err := repo.RequestPayrollPreparation(ctx, latest, false); err != nil {
			t.Fatal(err)
		}
		if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
			t.Fatal(worked, err)
		}
		if err := repo.RequestPayrollPreparation(ctx, current, false); err == nil {
			t.Fatal("current week accepted")
		}
		assertParity := func() {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			full, err := readPayrollProjectionReference(ctx, tx, latest, true)
			if err != nil {
				t.Fatal(err)
			}
			published, err := readPublishedPayrollReports(ctx, tx, latest)
			if err != nil {
				t.Fatal(err)
			}
			if !samePayrollProjection(full, published) {
				t.Fatal("completed payroll differs from independent calculation")
			}
		}
		board := NewGrossBoardRepository(pool)
		for i := range 3 {
			measured, metrics := db.WithMetrics(WithPayrollPublication(ctx))
			_, err := board.SaveEntries(measured, []GrossBoardEntry{{DriverID: projectionDriver, Date: current.Format(time.DateOnly), Version: i + 1, LoadNumber: "CURRENT-ONLY", OriginalRate: "1900", DriverRate: "1600", Miles: "650", Fields: []string{"originalRate", "driverRate", "miles"}}})
			if err != nil {
				t.Fatal(err)
			}
			if n := metrics.Events()["payroll_calculation"]; n != 0 {
				t.Fatalf("current board save performed %d calculations", n)
			}
			assertParity()
		}
		// Provider/background captures use the same proof, without a native save.
		exec(`UPDATE gross_board_entries SET entered_miles=700 WHERE service_date=$1`, current)
		if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
			t.Fatal(err)
		}
		measured, metrics := db.WithMetrics(ctx)
		if worked, err := repo.PrepareNextPayrollWeek(measured); err != nil || !worked {
			t.Fatal(worked, err)
		}
		if n := metrics.Events()["payroll_calculation"]; n != 0 {
			t.Fatalf("current board background change performed %d calculations", n)
		}
		assertParity()
		assertExpense := func(id, amount string) {
			t.Helper()
			page, err := repo.ReadPreparedPayrollPage(ctx, "driver", latest, PayPageQuery{})
			if err != nil || page.State != "ready" {
				t.Fatal("expense page not ready", page.State, err)
			}
			for _, d := range page.Report.Drivers {
				for _, item := range d.Edits.ExpenseDeductions {
					if item.ExpenseID == id {
						if item.Amount != amount {
							t.Fatalf("invoice %s: %s, want %s", id, item.Amount, amount)
						}
						return
					}
				}
			}
			if amount != "" {
				t.Fatal("eligible unpaid invoice missing", id)
			}
		}
		// Unpaid invoices before the cutoff remain live, including Monday additions.
		var category string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`).Scan(&category); err != nil {
			t.Fatal(err)
		}
		expenses := NewExpenseRepository(pool)
		input := ExpenseInput{CategoryID: category, ExpenseDate: current, DriverID: payTestString(projectionDriver), Amount: "35", ExpenseType: payTestString("Monday unpaid expense"), CoveredBy: payTestString("Driver")}
		entry, err := expenses.CreateExpense(WithPayrollPublication(ctx), input)
		if err != nil {
			t.Fatal(err)
		}
		assertParity()
		assertExpense(entry.ID, "35.00")
		input.Amount = "55"
		if _, err = expenses.UpdateExpense(WithPayrollPublication(ctx), entry.ID, input); err != nil {
			t.Fatal(err)
		}
		assertParity()
		assertExpense(entry.ID, "55.00")
		// Thursday through Sunday invoices wait for the next completed week;
		// retaining their evidence must not recalculate the current payroll.
		for _, offset := range []int{3, 4, 5, 6} {
			input.ExpenseDate = current.AddDate(0, 0, offset)
			measured, metrics := db.WithMetrics(WithPayrollPublication(ctx))
			later, err := expenses.CreateExpense(measured, input)
			if err != nil {
				t.Fatal(err)
			}
			if n := metrics.Events()["payroll_calculation"]; n != 0 {
				t.Fatalf("post-cutoff expense performed %d calculations", n)
			}
			assertExpense(later.ID, "")
			assertParity()
			// A backdated correction must invalidate the earlier payroll again.
			if offset == 3 {
				input.ExpenseDate = current.AddDate(0, 0, 2)
				if _, err = expenses.UpdateExpense(WithPayrollPublication(ctx), later.ID, input); err != nil {
					t.Fatal(err)
				}
				assertExpense(later.ID, "55.00")
				assertParity()
			}
		}
		// The minute seed prepares the completed week and retires only derived
		// current-week work. Canonical drafts and future reservations survive.
		exec(`INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES($1,$2)`, projectionDriver, current)
		exec(`INSERT INTO payroll_projection_jobs(week_start) VALUES($1),($1::date+7)`, current)
		if _, err = repo.claimPayrollProjection(ctx); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("worker claimed an unfinished payroll week", err)
		}
		// Model an already prepared current week from the previous release.
		var priorJob payrollProjectionJob
		err = pool.QueryRow(ctx, `UPDATE payroll_projection_jobs SET lease_token=gen_random_uuid(),lease_until=now()+interval '1 minute'
 WHERE week_start=$1 RETURNING week_start,requested_generation,lease_token::text,impact_roots`, current).Scan(&priorJob.Week, &priorJob.Generation, &priorJob.Token, &priorJob.Roots)
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.preparePayrollProjection(ctx, priorJob); err != nil {
			t.Fatal(err)
		}
		if err = repo.SeedPayrollPreparation(ctx); err != nil {
			t.Fatal(err)
		}
		var valid bool
		err = pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM payroll_projection_jobs WHERE week_start>$1)
 AND NOT EXISTS(SELECT 1 FROM payroll_projection_weeks WHERE week_start>$1)
 AND NOT EXISTS(SELECT 1 FROM payroll_statement_current WHERE week_start>$1)
 AND EXISTS(SELECT 1 FROM payroll_projection_jobs WHERE week_start=$1)
 AND EXISTS(SELECT 1 FROM driver_pay_weeks WHERE driver_id=$2 AND week_start=$3)`, latest, projectionDriver, current).Scan(&valid)
		if err != nil || !valid {
			t.Fatal("completed-week seeding changed canonical data or kept current work", valid, err)
		}
	})
}
