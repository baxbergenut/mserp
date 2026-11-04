package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollStatementImpactDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const other = "18200000-0000-0000-0000-000000008001"
		const truck = "18200000-0000-0000-0000-000000008002"
		const owner = "18200000-0000-0000-0000-000000008003"
		const invoice = "18200000-0000-0000-0000-000000008004"
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES($1,'Independent statement','independent statement','cpm',0.65,'2026-01-01')`, other)
		exec(`INSERT INTO investors(id,full_name) VALUES($1,'Independent owner')`, owner)
		exec(`INSERT INTO trucks(id,unit_number,owner_id) VALUES($1,'IMPACT-OTHER',$2)`, truck, owner)
		exec(`UPDATE truck_ownership_history SET start_known=false`)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,'2026-01-01')`, truck, other)
		exec(`INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES($1,$2,'2026-01-05',88)`, truck, owner)
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles) VALUES($1,'2026-10-05','IMPACT-OTHER',500,700,300)`, other)
		// Reporting another truck on a company invoice or a driver's own charge
		// must not join otherwise independent payrolls. Actual collections below do.
		exec(`INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,truck_id,covered_by)
 SELECT $1,id,name,'Actual driver balance','2026-10-05',100,$2,$3,'Driver' FROM expense_settings WHERE kind='category' AND name='Other'`, invoice, projectionDriver, truck)
		exec(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,truck_id,covered_by)
 SELECT id,name,'Company bookkeeping','2026-10-05',40,$1,$2,'Company' FROM expense_settings WHERE kind='category' AND name='Other'`, other, projectionTruck)
		repo := NewDriverPayRepository(pool)
		repo.preparedReads, repo.preparedCommands = true, true
		repo.resourceConcurrency = true
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Statement impact actor',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE payroll_projection_control SET protocol_mode='resources'`)
		week, _ := chargeWeek("2026-10-05")
		if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		prepare := func(wantDrivers, wantInvestors int64) {
			t.Helper()
			measured, m := db.WithMetrics(ctx)
			worked, err := repo.PrepareNextPayrollWeek(measured)
			if err != nil || !worked {
				t.Fatal(worked, err)
			}
			events := m.Events()
			if events["payroll_driver_statements_calculated"] != wantDrivers || events["payroll_investor_statements_calculated"] != wantInvestors {
				t.Fatalf("calculated unrelated statements: %v; want %d drivers/%d investors", events, wantDrivers, wantInvestors)
			}
		}
		dispatch := func() {
			t.Helper()
			if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
				t.Fatal(err)
			}
		}
		parity := func() {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			full, err := readPayrollProjectionReference(ctx, tx, week, true)
			if err != nil {
				t.Fatal(err)
			}
			got, err := readPublishedPayrollReports(ctx, tx, week)
			if err != nil || !samePayrollProjection(full, got) {
				t.Fatal("independent full calculation differs", err)
			}
		}
		states := func(first, second string) map[string]string {
			t.Helper()
			versions := map[string]string{}
			for _, kind := range []string{"driver", "investor"} {
				p, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{Summary: true})
				if err != nil {
					t.Fatal(err)
				}
				a, b := projectionDriver, other
				if kind == "investor" {
					a, b = projectionTruck, truck
				}
				for id, want := range map[string]string{a: first, b: second} {
					v, ok := p.Statements[id]
					if !ok || v.State != want {
						t.Fatalf("%s %s status %s; want %s (present %v)", kind, id, v.State, want, ok)
					}
					versions[kind+id] = v.Revision
				}
			}
			return versions
		}
		prepare(4, 4) // Initial preparation also runs the independent full reference.
		before := states("ready", "ready")
		// Background change: before dispatch and before preparation, only the
		// affected driver/investor pair becomes Updating.
		exec(`UPDATE gross_board_entries SET entered_miles=entered_miles+10 WHERE driver_id=$1`, projectionDriver)
		states("updating", "ready")
		dispatch()
		// An undispatched, unplaced provider load must not widen this leased work.
		exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(18203,'UNPLACED-ONLY','delivered',100,150,50,'{}')`)
		states("updating", "ready")
		prepare(1, 1)
		after := states("ready", "ready")
		if before["driver"+other] != after["driver"+other] || before["investor"+truck] != after["investor"+truck] {
			t.Fatal("unrelated revisions changed")
		}
		parity()
		dispatch()
		prepare(0, 0)
		exec(`UPDATE expenses SET amount=105 WHERE id=$1`, invoice)
		states("updating", "ready")
		dispatch()
		prepare(1, 1)
		states("ready", "ready")
		parity()
		// An edit in a later week is a freshness rebase, not a payroll calculation.
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles) VALUES($1,'2026-10-12','LATER-WEEK',300,400,100)`, projectionDriver)
		states("ready", "ready")
		// Ready must also mean an unrelated save can proceed before the worker
		// catches up, with the same exact expected revision and financial locks.
		editable, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{ID: projectionDriver}, true)
		if err != nil {
			t.Fatal(err)
		}
		input := editable.Drivers[0].Edits
		input.RequestID = "18200000-0000-4000-8000-000000008005"
		input.ExpectedStatementRevision = editable.Drivers[0].StatementRevision
		input.Notes = "Independent completed-week save"
		for i := range input.ExpenseDeductions {
			input.ExpenseDeductions[i].Apply = true
			input.ExpenseDeductions[i].Amount = "0.00"
		}
		if _, err = repo.Save(ctx, input, actor); err != nil {
			t.Fatal("ready statement blocked by later-week source", err)
		}
		dispatch()
		prepare(0, 0)
		states("ready", "ready")
		parity()
		// A post-cutoff invoice advances resource epochs. Only its exact captured
		// increments may be treated as irrelevant to this earlier payroll.
		exec(`INSERT INTO expenses(category_id,category,expense_type,expense_date,amount,driver_id,covered_by)
 SELECT id,name,'Later expense','2026-10-15',30,$1,'Driver' FROM expense_settings WHERE kind='category' AND name='Other'`, projectionDriver)
		states("ready", "ready")
		// Independent publication may give a row a newer source/resource baseline
		// than the weekly header. A header-based allowance would wrongly hide the
		// extra financial epoch below by counting this invoice a second time.
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := payrollCacheSnapshot(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		var newer string
		err = tx.QueryRow(ctx, `INSERT INTO payroll_projection_generations(week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 SELECT g.week_start,g.requested_generation,$2,g.calendar_week,g.calculator_version,g.schema_version,g.dependencies,g.driver_header,g.investor_header
 FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id WHERE w.week_start=$1 RETURNING id::text`, week, snapshot).Scan(&newer)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, `UPDATE payroll_statement_current SET generation_id=$1::uuid,
 resource_dependencies=payroll_intern_dependencies(payroll_resource_snapshot(ARRAY(SELECT jsonb_object_keys(payroll_expand_resource_dependencies(resource_dependencies)))))
 WHERE statement_id=ANY($2::uuid[])`, newer, []string{projectionDriver, projectionTruck})
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE payroll_resource_revisions SET revision=revision+1 WHERE resource_key=$1`, "driver:"+projectionDriver)
		states("updating", "ready")
		exec(`UPDATE payroll_resource_revisions SET revision=revision-1 WHERE resource_key=$1`, "driver:"+projectionDriver)
		dispatch()
		prepare(0, 0)
		states("ready", "ready")
		parity()
		var exact bool
		if err := pool.QueryRow(ctx, `SELECT bool_and(payroll_resources_current(resource_dependencies)) FROM payroll_statement_current WHERE week_start=$1`, week).Scan(&exact); err != nil || !exact {
			t.Fatal("unaffected resource certificates were not rebased", exact, err)
		}
		// A command may explain its own bookkeeping epoch, never an additional
		// financial epoch. Exercise both cases under the real resource claims.
		for _, unexplained := range []bool{false, true} {
			func() {
				tx, release, err := repo.beginPayrollWriteProtocol(ctx, true, "", "", "driver:"+projectionDriver)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				defer tx.Rollback(ctx)
				snapshot, err := payrollCacheSnapshot(ctx, tx)
				if err != nil {
					t.Fatal(err)
				}
				if unexplained {
					if _, err = tx.Exec(ctx, `UPDATE payroll_resource_revisions SET revision=revision+1 WHERE resource_key=$1`, "truck:"+truck); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = tx.Exec(ctx, `UPDATE expenses SET balance_version=balance_version+1 WHERE id=$1`, invoice); err != nil {
					t.Fatal(err)
				}
				impact, err := payrollMutationImpact(ctx, tx, &payrollCommand{beforeSnapshot: snapshot, name: "driver.save"}, "driver", projectionDriver)
				if err != nil || impact == nil {
					t.Fatal("command impact", err)
				}
				var generation string
				err = tx.QueryRow(ctx, `INSERT INTO payroll_projection_generations(week_start,requested_generation,input_snapshot,calendar_week,calculator_version,schema_version,dependencies,driver_header,investor_header)
 SELECT g.week_start,g.requested_generation,g.input_snapshot,g.calendar_week,g.calculator_version,g.schema_version,g.dependencies,g.driver_header,g.investor_header
 FROM payroll_projection_weeks w JOIN payroll_projection_generations g ON g.id=w.generation_id WHERE w.week_start=$1 RETURNING id::text`, week).Scan(&generation)
				if err != nil {
					t.Fatal(err)
				}
				if err = rebasePayrollStatementResources(ctx, tx, week, generation, snapshot, impact); err != nil {
					t.Fatal(err)
				}
				var current bool
				if err = tx.QueryRow(ctx, `SELECT payroll_resources_current(resource_dependencies) FROM payroll_statement_current WHERE kind='driver' AND statement_id=$1 AND week_start=$2`, other, week).Scan(&current); err != nil || current == unexplained {
					t.Fatal("command epoch proof", unexplained, current, err)
				}
			}()
		}
		// Saved investor allocations are genuine shared balances and must reconnect
		// both statements, even though the invoice's truck label alone did not.
		exec(`INSERT INTO expense_payments(expense_id,week_start,amount,investor_truck_id) VALUES($1,'2026-10-12',20,$2)`, invoice, truck)
		states("updating", "updating")
		dispatch()
		prepare(2, 2)
		states("ready", "ready")
		parity()
		exec(`UPDATE expense_payments SET amount=25 WHERE expense_id=$1`, invoice)
		states("updating", "updating")
		dispatch()
		prepare(2, 2)
		states("ready", "ready")
		parity()
		// No financial collection or frozen statement is changed by these reads.
		var collected string
		if err := pool.QueryRow(ctx, `SELECT amount::text FROM expense_payments WHERE expense_id=$1`, invoice).Scan(&collected); err != nil || collected != "25.00" {
			t.Fatal(collected, err)
		}
	})
}
