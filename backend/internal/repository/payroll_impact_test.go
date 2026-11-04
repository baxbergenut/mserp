package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPayrollIncrementalSaveDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(q string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Fatal(err)
			}
		}
		var actor, independent string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Incremental Test',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES('Unrelated Driver','unrelated driver','cpm',0.65,'2026-01-01') RETURNING id::text`).Scan(&independent); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles) VALUES($1,'2026-10-05','UNRELATED',200,300,120)`, independent)
		var category string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM expense_settings WHERE kind='category' AND name='Other'`).Scan(&category); err != nil {
			t.Fatal(err)
		}
		date, _ := time.Parse(time.DateOnly, "2026-10-05")
		driver := projectionDriver
		expense, err := NewExpenseRepository(pool).CreateExpense(ctx, ExpenseInput{CategoryID: category, ExpenseDate: date, DriverID: &driver, Amount: "200.00", ExpenseType: payTestString("Incremental test"), CoveredBy: payTestString("Driver")})
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount) VALUES($1,'2026-10-19',20)`, expense.ID)
		repo := NewDriverPayRepository(pool)
		if err = repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		weeks := []time.Time{}
		for _, s := range []string{"2026-09-28", "2026-10-05", "2026-10-12", "2026-10-19"} {
			w, _ := chargeWeek(s)
			weeks = append(weeks, w)
			if err = repo.RequestPayrollPreparation(ctx, w, false); err != nil {
				t.Fatal(err)
			}
			if _, err = repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
		}
		versions := map[string]string{}
		for _, w := range weeks {
			p, e := repo.ReadPreparedPayrollPage(ctx, "driver", w, PayPageQuery{})
			if e != nil {
				t.Fatal(e)
			}
			versions[w.Format(time.DateOnly)] = p.Statements[independent].Revision
		}
		week := weeks[1]
		for _, kind := range []string{"driver", "investor"} {
			id := projectionDriver
			if kind == "investor" {
				id = projectionTruck
			}
			page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{ID: id})
			if err != nil || len(page.Report.Drivers) != 1 {
				t.Fatal("missing statement", err)
			}
			edits := page.Report.Drivers[0].Edits
			edits.ExpectedStatementRevision = page.Statements[id].Revision
			edits.RequestID = "18200000-0000-0000-0000-000000000081"
			if kind == "investor" {
				edits.RequestID = "18200000-0000-0000-0000-000000000082"
			}
			edits.Notes = "First comment preserves statement-local choices"
			edits.Adjustments = append(edits.Adjustments, DriverPayAdjustment{ID: "18200000-0000-0000-0000-000000000083", Kind: "addition", Amount: "12.34", Name: "Reviewed adjustment"})
			if kind == "driver" {
				for i := range edits.ExpenseDeductions {
					if edits.ExpenseDeductions[i].ExpenseID == expense.ID {
						edits.ExpenseDeductions[i].Apply = true
						edits.ExpenseDeductions[i].Mode = "fixed"
						edits.ExpenseDeductions[i].Amount = "40.00"
					}
				}
			}
			var committed DriverPayEdits
			if kind == "driver" {
				committed, err = repo.Save(ctx, edits, actor)
			} else {
				committed, err = repo.SaveInvestorPay(ctx, edits, actor)
			}
			if err != nil {
				t.Fatal(kind, err)
			}
			for _, affected := range committed.AffectedStatements {
				if affected.ID == independent {
					var events string
					_ = pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_object('table',table_name,'old',old_routes,'new',new_routes))::text FROM payroll_projection_changes`).Scan(&events)
					t.Fatal("unrelated driver recalculated", kind, affected, events)
				}
			}
			if kind == "driver" {
				var amount string
				var payments int
				if err = pool.QueryRow(ctx, `SELECT expense_choices->$1->>'amount',(SELECT count(*) FROM expense_payments WHERE expense_id=$1::uuid AND week_start=$2) FROM driver_pay_weeks WHERE driver_id=$3 AND week_start=$2`, expense.ID, week, id).Scan(&amount, &payments); err != nil || amount != "40.00" || payments != 0 {
					t.Fatal("draft choice was omitted or posted", amount, payments, err)
				}
			}
			for _, w := range weeks {
				tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				if err != nil {
					t.Fatal(err)
				}
				full, err := readPayrollProjectionReference(ctx, tx, w, true)
				_ = tx.Rollback(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for i, pageKind := range []string{"driver", "investor"} {
					got, err := repo.ReadPreparedPayrollPage(ctx, pageKind, w, PayPageQuery{})
					if err != nil || got.State != "ready" {
						t.Fatal("not current", pageKind, w, err)
					}
					if pageKind == "driver" && got.Statements[independent].Revision != versions[w.Format(time.DateOnly)] {
						t.Fatal("unrelated retained version replaced", w)
					}
					a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
					b, _ := json.Marshal(got.Report)
					if string(a) != string(b) {
						t.Fatalf("incremental parity %s %s\nwant %s\ngot %s", pageKind, w, a, b)
					}
				}
			}
		}
	})
}

func TestPayrollRemovedStatementReceiptDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Removal receipt',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
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
		before, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil || len(before.Report.Drivers) != 1 {
			t.Fatal("missing original statement", err)
		}
		version := before.Statements[projectionDriver].Revision
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = lockTruckAccounting(ctx, tx); err != nil {
			t.Fatal(err)
		}
		command, _, err := beginPayrollCommand(ctx, tx, actor, "18200000-0000-0000-0000-000000000091", "test.remove", version, map[string]string{"id": projectionDriver})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM gross_board_entries WHERE driver_id=$1`, projectionDriver); err != nil {
			t.Fatal(err)
		}
		affected, err := publishPayrollCommand(ctx, tx, week.Format(time.DateOnly))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, identity := range affected {
			found = found || identity == (PayrollStatementIdentity{"driver", projectionDriver, "2026-10-05"})
		}
		if !found {
			t.Fatal("removed statement omitted from affected identities", affected)
		}
		if err = command.store(ctx, tx, affected, affected); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		after, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
		if err != nil || after.State != "ready" || len(after.Report.Drivers) != 0 {
			t.Fatal("removed statement remained current", after.State, len(after.Report.Drivers), err)
		}
		var retained bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payroll_command_versions WHERE actor_id=$1 AND version_id=$2)`, actor, version).Scan(&retained); err != nil || !retained {
			t.Fatal("removed statement evidence not retained", retained, err)
		}
	})
}
