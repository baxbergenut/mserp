package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPreparedPayrollSourceRaceDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Race actor',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedFinalization(true)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		seq := 90
		for _, kind := range []string{"driver", "investor"} {
			for _, finalize := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/finalize=%t", kind, finalize), func(t *testing.T) {
					seq++
					if err := repo.RequestPayrollPreparation(ctx, week, true); err != nil {
						t.Fatal(err)
					}
					if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
						t.Fatal(err)
					}
					page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{}, true)
					if err != nil {
						t.Fatal(err)
					}
					d := page.Drivers[0]
					blocker, err := pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer blocker.Rollback(ctx)
					if _, err = blocker.Exec(ctx, `SELECT revision FROM payroll_cache_revisions WHERE table_name='loads' FOR UPDATE`); err != nil {
						t.Fatal(err)
					}
					request := fmt.Sprintf("18200000-0000-0000-0000-%012d", seq)
					work, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					done := make(chan error, 1)
					go func() {
						var e error
						if finalize {
							opt := PayrollSettlementCommand{RequestID: request, ExpectedStatementRevision: d.StatementRevision}
							if kind == "driver" {
								_, e = repo.Settle(WithPayrollReceipt(work), week, d.ID, page.Revision, actor, "", false, true, opt)
							} else {
								_, e = repo.SettleInvestor(WithPayrollReceipt(work), week, d.ID, page.Revision, actor, "", false, true, opt)
							}
						} else {
							edits := d.Edits
							edits.RequestID = request
							edits.ExpectedStatementRevision = d.StatementRevision
							edits.Notes = "Must abort on newer source"
							if kind == "driver" {
								_, e = repo.Save(work, edits, actor)
							} else {
								_, e = repo.SaveInvestorPay(work, edits, actor)
							}
						}
						done <- e
					}()
					waiting := false
					deadline := time.Now().Add(5 * time.Second)
					for time.Now().Before(deadline) {
						if err = admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'SELECT table_name,revision FROM payroll_cache_revisions%')`).Scan(&waiting); err != nil {
							t.Fatal(err)
						}
						if waiting {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					if !waiting {
						t.Fatal("command did not reach source fence")
					}
					if _, err = blocker.Exec(ctx, `UPDATE loads SET total_pay=total_pay+1 WHERE id=18201`); err != nil {
						t.Fatal(err)
					}
					if err = blocker.Commit(ctx); err != nil {
						t.Fatal(err)
					}
					err = <-done
					var pgerr *pgconn.PgError
					if !errors.As(err, &pgerr) || pgerr.Code != "40001" {
						t.Fatal("stale command did not abort", err)
					}
					var written int
					if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM payroll_command_receipts)+(SELECT count(*) FROM driver_pay_weeks)+(SELECT count(*) FROM investor_pay_weeks)+(SELECT count(*) FROM payroll_settlements)+(SELECT count(*) FROM driver_pay_cost_collections)+(SELECT count(*) FROM pay_source_collections)`).Scan(&written); err != nil || written != 0 {
						t.Fatal("aborted command left financial state", written, err)
					}
				})
			}
		}
	})
}
