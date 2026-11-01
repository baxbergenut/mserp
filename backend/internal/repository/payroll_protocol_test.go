package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollProtocolSwitchDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewDriverPayRepository(pool)
		if _, err := repo.SwitchPayrollProtocol(ctx, "dual", 1); err == nil {
			t.Fatal("switch without capture")
		}
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		var financial string
		if err := pool.QueryRow(ctx, payrollFinancialParitySQL).Scan(&financial); err != nil {
			t.Fatal(err)
		}
		for i, mode := range []string{"dual", "resources", "global"} {
			if i == 1 {
				hold, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`); err != nil {
					t.Fatal(err)
				}
				short, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
				_, err = repo.SwitchPayrollProtocol(short, mode, int64(i+1))
				cancel()
				_ = hold.Rollback(ctx)
				if err == nil {
					t.Fatal("switch crossed active writer gate")
				}
			}
			result, err := repo.SwitchPayrollProtocol(ctx, mode, int64(i+1))
			if err != nil || result.Mode != mode || result.Epoch != int64(i+2) {
				t.Fatal("switch", result, err)
			}
			if _, err = repo.SwitchPayrollProtocol(ctx, mode, int64(i+1)); !errors.Is(err, ErrPayrollResourceChanged) {
				t.Fatal("stale operator epoch accepted", err)
			}
			if err = repo.ConfigurePayrollProtocol(ctx, mode == "resources"); err != nil {
				t.Fatal(err)
			}
			if err = NewDriverPayRepository(pool).ConfigurePayrollProtocol(ctx, mode != "resources"); err == nil {
				t.Fatal("incompatible runtime accepted")
			}
			page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
			if err != nil || page.State != "updating" {
				t.Fatal("transition presented old projection as current", page.State, err)
			}
			if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal("prepare after transition", err)
			}
			page, err = repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
			if err != nil || page.State != "ready" {
				t.Fatal("post-transition preparation", page.State, err)
			}
			var after string
			if err = pool.QueryRow(ctx, payrollFinancialParitySQL).Scan(&after); err != nil || after != financial {
				t.Fatal("protocol transition changed financial history", err)
			}
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM payroll_protocol_events`).Scan(&count); err != nil || count != 3 {
			t.Fatal("transition receipts", count, err)
		}
	})
}
