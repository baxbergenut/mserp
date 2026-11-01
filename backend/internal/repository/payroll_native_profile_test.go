package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

// Opt-in incident reproduction on the existing production-sized synthetic fleet.
// Keep ordinary database tests stopped while collecting these diagnostic timings.
func profilePayrollNativeSaves(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var actor, category, driver string
	if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Native save profile',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT md5('driver1')::uuid::text`).Scan(&driver); err != nil {
		t.Fatal(err)
	}
	date, _ := chargeWeek("2026-09-21")
	input := ExpenseInput{CategoryID: category, ExpenseDate: date, DriverID: &driver, Amount: "20", ExpenseType: payTestString("Native save profile"), CoveredBy: payTestString("Driver")}
	expenses := NewExpenseRepository(pool)
	entry, err := expenses.CreateExpense(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	pay := NewDriverPayRepository(pool)
	if err = pay.EnablePayrollShadow(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pay.SwitchPayrollProtocol(ctx, "resources", 1); err != nil {
		t.Fatal(err)
	}
	pay.ConfigurePreparedReads(true)
	pay.ConfigurePreparedCommands(true)
	current, _ := chargeWeek(ChargeCurrentWeek())
	for n := 0; n < 69; n++ {
		if err = pay.RequestPayrollPreparation(ctx, current.AddDate(0, 0, -7*n), false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pay.DispatchPayrollChanges(ctx); err != nil {
		t.Fatal(err)
	}
	for {
		worked, e := pay.PrepareNextPayrollWeek(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if !worked {
			break
		}
	}
	t.Log("69 current/past periods prepared; starting native expense commands")
	for _, action := range []string{"edit", "verify"} {
		measured, metrics := db.WithMetrics(WithPayrollPublication(ctx))
		bounded, cancel := context.WithTimeout(measured, 25*time.Second)
		start := time.Now()
		if action == "edit" {
			input.Amount = "35"
			entry, err = expenses.UpdateExpense(bounded, entry.ID, input)
		} else {
			entry, err = expenses.VerifyExpense(bounded, entry.ID, category, "accountant", entry.UpdatedAt, AuthUser{ID: actor, Username: "Native save profile", Administrator: true})
		}
		cancel()
		t.Logf("native %s: %s, queries=%d, events=%v", action, time.Since(start), metrics.Queries.Load(), metrics.Events())
		if err != nil {
			t.Fatal(action, err)
		}
		for _, week := range []time.Time{date, current} {
			read, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			full, err := readPayrollProjectionReference(ctx, read, week, true)
			_ = read.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for i, kind := range []string{"driver", "investor"} {
				page, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
				a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
				b, _ := json.Marshal(page.Report)
				if err != nil || page.State != "ready" || string(a) != string(b) {
					t.Fatal("native save parity", action, kind, week, page.State, err)
				}
			}
		}
	}
}
