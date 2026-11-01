package repository

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

// Opt-in incident reproduction on the existing production-sized synthetic fleet.
// Keep ordinary database tests stopped while collecting these diagnostic timings.
func profilePayrollNativeSaves(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	periods := 69
	if raw := os.Getenv("MSERP_PAYROLL_NATIVE_PROFILE_WEEKS"); raw != "" {
		var e error
		periods, e = strconv.Atoi(raw)
		if e != nil || periods < 2 || periods > 69 {
			t.Fatal("choose 2 through 69 profile weeks")
		}
	}
	current, _ := chargeWeek(ChargeCurrentWeek())
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
	date := current.AddDate(0, 0, -7)
	input := ExpenseInput{CategoryID: category, ExpenseDate: date, DriverID: &driver, Amount: "20", ExpenseType: payTestString("Native save profile"), CoveredBy: payTestString("Driver")}
	expenses := NewExpenseRepository(pool)
	entry, err := expenses.CreateExpense(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	// Keep a dense source component inside the retained window so the small
	// two-week diagnostic also exercises shared dependency storage.
	if _, err = pool.Exec(ctx, `INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'Shared native profile',$1::date,1,$2::uuid,'Company'
 FROM expense_settings CROSS JOIN generate_series(1,180) WHERE kind='category' AND name='Other'`, date, driver); err != nil {
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
	for n := 0; n < periods; n++ {
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
	t.Logf("%d current/past periods prepared; starting native expense commands", periods)
	var packed, inlineBytes, expandedBytes, sharedBytes int64
	if err = pool.QueryRow(ctx, `SELECT count(*),
 coalesce(sum(pg_column_size(resource_dependencies)),0),coalesce(sum(octet_length(payroll_expand_resource_dependencies(resource_dependencies)::text)),0),
 (SELECT coalesce(sum(pg_column_size(dependencies)),0) FROM payroll_resource_dependency_sets)
 FROM payroll_statement_versions WHERE resource_dependencies ? '$shared'`).Scan(&packed, &inlineBytes, &expandedBytes, &sharedBytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("shared dependencies: packed=%d stored=%d expanded=%d shared=%d bytes", packed, inlineBytes, expandedBytes, sharedBytes)
	if packed == 0 || inlineBytes+sharedBytes >= expandedBytes/2 {
		t.Fatal("large fixture did not substantially share revision metadata")
	}
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
