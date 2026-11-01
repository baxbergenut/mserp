package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Conservative lock claims may connect historical coworkers. A current version
// must depend on its actual period component, otherwise an unrelated save marks
// it stale with no pending job able to repair it.
func TestPayrollResourcePeriodDependenciesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const independent = "18200000-0000-0000-0000-000000009501"
		const actor = "18200000-0000-0000-0000-000000009502"
		if _, err := pool.Exec(ctx, `UPDATE truck_driver_assignments SET assigned_at='2026-09-01';
 INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000009501','Historical coworker','historical coworker','cpm',0.65,'2026-01-01');
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at)
 VALUES('18200000-0000-0000-0000-000000000002','18200000-0000-0000-0000-000000009501','2026-01-01','2026-02-01');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000009501','2026-10-05','HISTORICAL-COWORKER',200,300,120);
 INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,charge_driver_id,covered_by)
 SELECT id,name,'Current personal invoice','2026-10-01',50,'18200000-0000-0000-0000-000000000001','Driver' FROM expense_settings WHERE kind='category' AND name='Other';
 INSERT INTO app_users(id,username,password_hash) VALUES('18200000-0000-0000-0000-000000009502','Period dependencies',crypt('test-only',gen_salt('bf')));`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(true)
		repo.ConfigurePreparedFinalization(true)
		repo.resourceConcurrency = true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources' WHERE singleton`); err != nil {
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
		if err != nil || before.State != "ready" {
			t.Fatal("initial preparation", before.State, err)
		}
		card, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{ID: projectionDriver}, true)
		if err != nil || len(card.Drivers) != 1 {
			t.Fatal(card, err)
		}
		edits := card.Drivers[0].Edits
		edits.RequestID = "18200000-0000-0000-0000-000000009503"
		edits.ExpectedStatementRevision = card.Drivers[0].StatementRevision
		edits.Notes = "First allocation must not stale historical coworkers"
		for i := range edits.ExpenseDeductions {
			edits.ExpenseDeductions[i].Apply = true
		}
		if _, err := repo.Save(ctx, edits, actor); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"driver", "investor"} {
			after, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			if err != nil || after.State != "ready" {
				t.Fatal("save left another statement stale", kind, after.State, err)
			}
			if kind == "driver" && before.Statements[independent].Revision != after.Statements[independent].Revision {
				t.Fatal("unrelated current statement republished")
			}
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			full, err := readPayrollProjectionReference(ctx, tx, week, true)
			_ = tx.Rollback(ctx)
			if err != nil {
				t.Fatal(err)
			}
			i := 0
			if kind == "investor" {
				i = 1
			}
			want, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			got, _ := json.Marshal(after.Report)
			if string(want) != string(got) {
				t.Fatalf("period dependency parity %s\n%s\n%s", kind, want, got)
			}
		}
		card, err = repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{ID: projectionDriver}, true)
		if err != nil {
			t.Fatal(err)
		}
		_, err = repo.Settle(WithPayrollReceipt(ctx), week, projectionDriver, card.Revision, actor, "", false, true, PayrollSettlementCommand{RequestID: "18200000-0000-0000-0000-000000009504", ExpectedStatementRevision: card.Drivers[0].StatementRevision})
		if err != nil {
			t.Fatal("finalization after historical coworker save", err)
		}
	})
}
