package repository

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

// Reuses the production-sized synthetic fixture and its historical dependency
// component. This opt-in branch performs shadow parity, not financial timing.
func verifyPayrollProjectionFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	repo := NewDriverPayRepository(pool)
	if err := repo.EnablePayrollShadow(ctx); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MSERP_PAYROLL_RESOURCE_PROFILE") == "1" {
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
			t.Fatal(err)
		}
	}
	for _, date := range []string{"2026-09-28", "2026-10-19", "2026-10-26"} {
		week, _ := chargeWeek(date)
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if worked, err := repo.PrepareNextPayrollWeek(ctx); err != nil || !worked {
			t.Fatal(worked, err)
		}
		t.Logf("shadow fixture week=%s preparationMS=%.2f", date, float64(time.Since(start).Microseconds())/1000)
		for _, kind := range []string{"driver", "investor"} {
			for page := 1; ; page++ {
				totalPages := 1
				for _, summary := range []bool{true, false} {
					query := PayPageQuery{Pagination: Pagination{Page: page, PageSize: 25}, Summary: summary}
					fresh := NewDriverPayRepository(pool)
					measured, metrics := db.WithQueryMetrics(ctx)
					start = time.Now()
					got, err := fresh.ReadPreparedPayrollPage(measured, kind, week, query)
					elapsed := float64(time.Since(start).Microseconds()) / 1000
					if err != nil || got.State != "ready" {
						t.Fatal(kind, date, got.State, err)
					}
					totalPages = got.Report.Pagination.TotalPages
					phases, _ := metrics.Details()
					if phases["driver_calculation"] != 0 || phases["investor_calculation"] != 0 {
						t.Fatal("persistent read invoked a calculator")
					}
					want, err := repo.PayrollPage(ctx, kind, week, query)
					if err != nil {
						t.Fatal(err)
					}
					a, _ := json.Marshal(got.Report)
					b, _ := json.Marshal(want)
					if string(a) != string(b) {
						t.Fatalf("persistent fixture parity differs: %s %s page=%d summary=%t\ngot %s\nwant %s", kind, date, page, summary, a, b)
					}
					t.Logf("shadow fixture kind=%s week=%s page=%d summary=%t readMS=%.2f queries=%d bytes=%d calculations=0", kind, date, page, summary, elapsed, metrics.Queries.Load(), len(a))
				}
				if page >= totalPages {
					break
				}
			}
		}
	}
}
