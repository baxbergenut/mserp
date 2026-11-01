package repository

import (
	"context"
	"mserp/internal/db"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollWorkerImpactDatabase(t *testing.T) {
	for _, mode := range []string{"global", "resources"} {
		for _, prepared := range []bool{false, true} {
			name := "shadow"
			if prepared {
				name = "prepared"
			}
			t.Run(mode+"/"+name, func(t *testing.T) { testPayrollWorkerImpact(t, mode, prepared) })
		}
	}
}

func testPayrollWorkerImpact(t *testing.T, mode string, prepared bool) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const second = "18200000-0000-0000-0000-000000001600"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES($1,'External principal','external principal','cpm',0.65,'2026-01-01')`, second); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(prepared)
		repo.ConfigurePreparedCommands(prepared)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode=$1 WHERE singleton`, mode); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		before, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil {
			t.Fatal(err)
		}
		var expense string
		if err = pool.QueryRow(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'External principal','2026-10-05',75,$1,'Driver' FROM expense_settings WHERE kind='category' AND name='Other' RETURNING id::text`, second).Scan(&expense); err != nil {
			t.Fatal(err)
		}
		dispatch := func() []string {
			t.Helper()
			if n, err := repo.DispatchPayrollChanges(ctx); err != nil || n == 0 {
				t.Fatal("missing captured changes", n, err)
			}
			var roots []string
			if err := pool.QueryRow(ctx, `SELECT impact_roots FROM payroll_projection_jobs WHERE week_start=$1`, week).Scan(&roots); err != nil {
				t.Fatal(err)
			}
			return roots
		}
		roots := dispatch()
		if !slices.Equal(roots, []string{"driver:" + second}) {
			t.Fatal("external principal was not retained on its durable job", roots)
		}
		measured, metrics := db.WithQueryMetrics(ctx)
		if _, err = repo.PrepareNextPayrollWeek(measured); err != nil {
			t.Fatal("incremental worker publication", err)
		}
		phases, _ := metrics.Details()
		if (phases["projection_reconciliation"] == 0) != prepared {
			t.Fatal("unexpected full reference calculation for worker capability", prepared, phases)
		}
		assertParity := func() {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			published, err := readPublishedPayrollReports(ctx, tx, week)
			if err != nil {
				t.Fatal(err)
			}
			full, err := readPayrollProjectionReference(ctx, tx, week, true)
			if err != nil || !samePayrollProjection(published, full) {
				t.Fatal("worker publication differs from independent full reference", err)
			}
		}
		assertParity()
		after, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil || after.State != "ready" || after.Statements[second].Revision == "" || before.Statements[projectionDriver].Revision != after.Statements[projectionDriver].Revision {
			t.Fatal("worker did not preserve unrelated statement or introduce the new principal", after.State, err)
		}
		// Separate committed batches must union their roots until publication.
		if _, err = pool.Exec(ctx, `UPDATE expenses SET amount=80 WHERE id=$1`, expense); err != nil {
			t.Fatal(err)
		}
		dispatch()
		if _, err = pool.Exec(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'Other external principal','2026-10-05',25,$1,'Driver' FROM expense_settings WHERE kind='category' AND name='Other'`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		roots = dispatch()
		if !slices.Contains(roots, "driver:"+second) || !slices.Contains(roots, "driver:"+projectionDriver) {
			t.Fatal("coalescing lost an earlier principal", roots)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		// A committed but undispatched source is not covered by those roots.
		assertParity()
		// The worker must reconcile fully rather than certify retained stale rows.
		if _, err = pool.Exec(ctx, `UPDATE expenses SET amount=85 WHERE id=$1`, expense); err != nil {
			t.Fatal(err)
		}
		dispatch()
		if _, err = pool.Exec(ctx, `UPDATE drivers SET full_name='Corrected projection name' WHERE id=$1`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{ID: projectionDriver})
		if err != nil || page.State != "ready" || len(page.Report.Drivers) != 1 || page.Report.Drivers[0].FullName != "Corrected projection name" {
			t.Fatal("undispatched source was certified without recalculation", page, err)
		}
		// This type-level input has no registered principal resolver yet.
		assertParity()
		if _, err = pool.Exec(ctx, `INSERT INTO driver_charge_types(name,direction,amount,amounts) VALUES('Unregistered source impact','charge',15,ARRAY[15]::numeric[])`); err != nil {
			t.Fatal(err)
		}
		if roots = dispatch(); roots != nil {
			t.Fatal("unknown source must request full preparation", roots)
		}
		if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		var paid int
		assertParity()
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM expense_payments`).Scan(&paid); err != nil || paid != 0 {
			t.Fatal("background preparation recorded a financial collection", paid, err)
		}
	})
}
