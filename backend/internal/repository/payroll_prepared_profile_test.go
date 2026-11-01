package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"mserp/internal/db"
)

// Diagnostic profiles only: these sparse samples are not the browser acceptance
// matrix. Run in isolation on the disposable production-sized fixture.
func profilePreparedPayrollCommands(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var actor string
	password, err := bcrypt.GenerateFromPassword([]byte("test-only"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Prepared profile',$1) RETURNING id::text`, string(password)).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	repo := NewDriverPayRepository(pool)
	repo.ConfigurePreparedReads(true)
	repo.ConfigurePreparedFinalization(true)
	status, err := repo.PayrollProtocolStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ConfigurePayrollProtocol(ctx, status.Mode == "resources"); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MSERP_PAYROLL_PROFILE_HISTORY") == "1" {
		// Include the full materialized calendar. A three-week diagnostic hides
		// repeated historical recalculation and is not representative of saves.
		first, _ := chargeWeek("2026-01-05")
		last, _ := chargeWeek("2026-11-09")
		for w := first; !w.After(last); w = w.AddDate(0, 0, 7) {
			if err := repo.RequestPayrollPreparation(ctx, w, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Actor display is a captured input; reconcile before measuring commands.
	if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
		t.Fatal(err)
	}
	for {
		worked, err := repo.PrepareNextPayrollWeek(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	week, _ := chargeWeek("2026-10-19")
	if value := os.Getenv("MSERP_PAYROLL_PROFILE_WEEK"); value != "" {
		week, err = chargeWeek(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	seq := 100
	for _, kind := range []string{"driver", "investor"} {
		for _, group := range []int{1, 171} {
			source := fmt.Sprintf("driver%d", group)
			if kind == "investor" {
				source = fmt.Sprintf("truck%d", group)
			}
			var id string
			if err := pool.QueryRow(ctx, `SELECT md5($1)::uuid::text`, source).Scan(&id); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"first_save", "repeat_save", "finalize", "reopen"} {
				page, err := repo.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
				if err != nil || len(page.Drivers) != 1 {
					t.Fatal("profile statement", kind, source, err)
				}
				row := page.Drivers[0]
				seq++
				request := fmt.Sprintf("18200000-0000-0000-0000-%012d", seq)
				measured, metrics := db.WithQueryMetrics(ctx)
				start := time.Now()
				if action == "first_save" || action == "repeat_save" {
					e := row.Edits
					e.RequestID = request
					e.ExpectedStatementRevision = row.StatementRevision
					e.Notes = action
					for i := range e.ExpenseDeductions {
						if !e.ExpenseDeductions[i].Saved {
							e.ExpenseDeductions[i].Apply = true
						}
					}
					if kind == "driver" {
						_, err = repo.Save(measured, e, actor)
					} else {
						_, err = repo.SaveInvestorPay(measured, e, actor)
					}
				} else {
					opt := PayrollSettlementCommand{RequestID: request, ExpectedStatementRevision: row.StatementRevision}
					if kind == "driver" {
						_, err = repo.Settle(WithPayrollReceipt(measured), week, id, page.Revision, actor, "Profile reopen", action == "reopen", action == "finalize", opt)
					} else {
						_, err = repo.SettleInvestor(WithPayrollReceipt(measured), week, id, page.Revision, actor, "Profile reopen", action == "reopen", action == "finalize", opt)
					}
				}
				elapsed := time.Since(start)
				if err != nil {
					t.Fatal("profile command", kind, source, action, err)
				}
				phases, queries := metrics.Details()
				body, _ := json.Marshal(map[string]any{"kind": kind, "group": group, "action": action, "ms": float64(elapsed.Microseconds()) / 1000, "calls": metrics.Queries.Load(), "phases": phases, "queries": queries})
				t.Logf("prepared command diagnostic %s", body)
			}
		}
	}
}
