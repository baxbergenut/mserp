package repository

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestPayrollPlanningDisplayDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Planning labels',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		pay.ConfigurePreparedFinalization(true)
		pay.resourceConcurrency = true
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if done, err := pay.PrepareNextPayrollWeek(ctx); err != nil || !done {
			t.Fatal("prepare", done, err)
		}
		board := NewGrossBoardRepository(pool)
		pinned := map[string]string{}
		idFor := func(kind string) string {
			if kind == "investor" {
				return projectionTruck
			}
			return projectionDriver
		}
		read := func(kind, route string) DriverPayWeek {
			t.Helper()
			id := idFor(kind)
			var report DriverPayWeek
			var err error
			switch route {
			case "cached":
				if kind == "driver" {
					report, err = pay.GetDriverWeek(ctx, week, id)
				} else {
					report, err = pay.InvestorStatement(ctx, week, id)
				}
			case "summary", "page":
				report, err = pay.PayrollPage(ctx, kind, week, PayPageQuery{ID: id, Summary: route == "summary"})
			case "detail":
				report, err = pay.PayrollStatement(ctx, kind, week, id)
			default:
				var prepared PreparedPayrollPage
				prepared, err = pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{ID: id, Summary: route == "prepared-summary"})
				if err == nil && prepared.State != "ready" {
					t.Fatal("planning invalidated financial preparation", kind, route, prepared.State)
				}
				if pinned[kind] == "" {
					pinned[kind] = prepared.Statements[id].Revision
				}
				report = prepared.Report
			}
			if err != nil || len(report.Drivers) != 1 {
				t.Fatal("read", kind, route, err, len(report.Drivers))
			}
			return report
		}
		routes := []string{"cached", "summary", "page", "detail", "prepared-summary", "prepared-detail"}
		baseline := map[string]DriverPayWeek{}
		for _, kind := range []string{"driver", "investor"} {
			for _, route := range routes {
				baseline[kind+route] = read(kind, route)
			}
		}
		plans := []GrossBoardPlanningEntry{{DriverID: projectionDriver, Date: "2026-10-06", Kind: "note", Text: "HOME"}, {DriverID: projectionDriver, Date: "2026-10-06", Kind: "destination", Text: "Dallas, TX"}}
		save := func() {
			t.Helper()
			var err error
			plans, err = board.SavePlanningChanges(ctx, plans, actor)
			if err != nil {
				t.Fatal(err)
			}
		}
		save()
		want := []PayDayStatus{{Date: "2026-10-06", Status: "CUSTOM", Kind: "destination", Text: "Dallas, TX"}, {Date: "2026-10-06", Status: "CUSTOM", Kind: "note", Text: "HOME"}}
		check := func(kind string, labels []PayDayStatus, compareFinancial bool) {
			t.Helper()
			for _, route := range routes {
				report := read(kind, route)
				expected := labels
				if report.Drivers[0].Settlement != nil && report.Drivers[0].Settlement.Finalized && (route == "summary" || route == "prepared-summary") {
					expected = nil
				}
				if !(len(report.Drivers[0].DayStatuses) == 0 && len(expected) == 0) && !reflect.DeepEqual(report.Drivers[0].DayStatuses, expected) {
					t.Fatalf("%s %s labels: %#v, want %#v", kind, route, report.Drivers[0].DayStatuses, labels)
				}
				if compareFinancial {
					before := baseline[kind+route]
					a, b := before.Drivers[0], report.Drivers[0]
					if report.Revision != before.Revision || a.NetPayCents() != b.NetPayCents() || !reflect.DeepEqual(a.Loads, b.Loads) || a.HomeWeek != b.HomeWeek {
						t.Fatal("planning changed financial values/revision/load records", kind, route)
					}
				}
			}
		}
		check("driver", want, true)
		check("investor", want, true)
		for _, kind := range []string{"driver", "investor"} {
			page, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{ID: idFor(kind), StatementRevision: pinned[kind]})
			if err != nil || page.State != "ready" || !reflect.DeepEqual(page.Report.Drivers[0].DayStatuses, want) {
				t.Fatal("pinned detail lost live labels", kind, err)
			}
		}
		// Reuse the same cached financial and prepared versions through edits and clears.
		plans[0].Text = "Reset after delivery"
		save()
		want[1].Text = plans[0].Text
		check("driver", want, true)
		check("investor", want, true)
		plans[0].Text = ""
		save()
		want = want[:1]
		check("driver", want, true)
		check("investor", want, true)
		plans[1].Text = ""
		save()
		check("driver", nil, true)
		check("investor", nil, true)
		plans[1].Text = "Dallas, TX"
		plans[0].Text = "At finalization"
		save()
		want = append(want, PayDayStatus{Date: "2026-10-06", Status: "CUSTOM", Kind: "note", Text: plans[0].Text})
		// Both fast prepared finalizers must freeze live labels, not their older stored
		// financial version's empty labels. Metadata edits require no new preparation.
		for _, kind := range []string{"driver", "investor"} {
			page, err := pay.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: idFor(kind)}, true)
			if err != nil {
				t.Fatal(err)
			}
			var request string
			if err = pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&request); err != nil {
				t.Fatal(err)
			}
			command := PayrollSettlementCommand{RequestID: request, ExpectedStatementRevision: page.Drivers[0].StatementRevision, ExpenseRevision: page.Drivers[0].ExpenseRevision}
			measured, metrics := db.WithMetrics(WithPayrollReceipt(ctx))
			var frozen DriverPayWeek
			if kind == "driver" {
				frozen, err = pay.Settle(measured, week, idFor(kind), page.Revision, actor, "", false, true, command)
			} else {
				frozen, err = pay.SettleInvestor(measured, week, idFor(kind), page.Revision, actor, "", false, true, command)
			}
			if err != nil {
				t.Fatal("finalize", kind, err)
			}
			if len(frozen.Drivers) != 1 || !frozen.Drivers[0].Settlement.Finalized || !reflect.DeepEqual(frozen.Drivers[0].DayStatuses, want) {
				t.Fatal("frozen wrong labels", kind, frozen.Drivers)
			}
			if metrics.Events()["payroll_calculation"] != 0 {
				t.Fatal("planning invoked a financial calculator", kind, metrics.Events())
			}
			check(kind, want, false)
		}
		plans[0].Text = "Changed after finalization"
		plans[1].Text = ""
		save()
		check("driver", want, false)
		check("investor", want, false)
		plans[0].Text = ""
		save()
		check("driver", want, false)
		check("investor", want, false)
		// Legacy frozen reports without metadata must stay empty on a real full read.
		if _, err := pool.Exec(ctx, `UPDATE payroll_settlements SET report=report-'dayStatuses'; UPDATE investor_pay_weeks SET report=report-'dayStatuses' WHERE finalized`); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"driver", "investor"} {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var legacy DriverPayWeek
			if kind == "driver" {
				legacy, err = readDriverPayWeek(ctx, tx, week, projectionDriver)
			} else {
				legacy, err = readInvestorPay(ctx, tx, week)
			}
			_ = tx.Rollback(ctx)
			if err != nil || len(legacy.Drivers) != 1 || legacy.Drivers[0].DayStatuses != nil {
				t.Fatal("legacy frozen labels backfilled", kind, err, legacy.Drivers)
			}
		}
	})
}

func TestPayrollPlanningRoutingDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		testPayrollDayStatusRouting(t, ctx, pool)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		id := func(sql string, args ...any) string {
			t.Helper()
			var value string
			if err := tx.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := tx.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		main := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Planning Main','planning main','cpm',0.75) RETURNING id`)
		team := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Planning Team','planning team','cpm',0.75) RETURNING id`)
		oldTruck := id(`INSERT INTO trucks(unit_number) VALUES('PLANNING-OLD') RETURNING id`)
		newTruck := id(`INSERT INTO trucks(unit_number) VALUES('PLANNING-NEW') RETURNING id`)
		exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week,end_week) VALUES($1,$2,'2026-10-05','2026-10-12')`, main, team)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at) VALUES($1,$3,'2026-10-01 00:00-04','2026-10-07 00:00-04'),($2,$3,'2026-10-07 00:00-04',NULL)`, oldTruck, newTruck, main)
		exec(`INSERT INTO gross_board_planning_entries(driver_id,service_date,kind,text) VALUES($1,'2026-10-05','note','HOME'),($2,'2026-10-05','note','HOME'),($2,'2026-10-07','destination','Dallas, TX'),($2,'2026-10-08','note',''),($2,'2026-10-12','note','Next week')`, main, team)
		want := []PayDayStatus{{Date: "2026-10-05", Status: "CUSTOM", Kind: "note", Text: "HOME"}, {Date: "2026-10-07", Status: "CUSTOM", Kind: "destination", Text: "Dallas, TX"}}
		report := DriverPayWeek{WeekStart: "2026-10-05", Drivers: []DriverPayDriver{{ID: main}, {ID: team}}}
		if err = decoratePayDayStatuses(ctx, tx, "driver", &report); err != nil || !reflect.DeepEqual(report.Drivers[0].DayStatuses, want) || len(report.Drivers[1].DayStatuses) != 0 || payHomeWeek(report.WeekStart, report.Drivers[0]) {
			t.Fatal("team/duplicate/tombstone/week/status isolation", report, err)
		}
		investor := DriverPayWeek{WeekStart: report.WeekStart, Drivers: []DriverPayDriver{{ID: oldTruck}, {ID: newTruck}}}
		if err = decoratePayDayStatuses(ctx, tx, "investor", &investor); err != nil {
			t.Fatal(err)
		}
		for i, d := range investor.Drivers {
			if !reflect.DeepEqual(d.DayStatuses, want[i:i+1]) {
				t.Fatal("dated truck routing", investor)
			}
		}
		// Ambiguous overlapping assignments are omitted rather than guessed.
		exec(`UPDATE truck_driver_assignments SET unassigned_at='2026-10-15 00:00-04' WHERE truck_id=$1`, newTruck)
		exec(`INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,unassigned_at) VALUES($1,$2,'2026-10-08 00:00-04','2026-10-13 00:00-04')`, oldTruck, main)
		exec(`INSERT INTO gross_board_planning_entries(driver_id,service_date,kind,text) VALUES($1,'2026-10-09','note','Ambiguous')`, main)
		if err = decoratePayPlanningStatuses(ctx, tx, "investor", &investor); err != nil {
			t.Fatal(err)
		}
		for i, d := range investor.Drivers {
			if !reflect.DeepEqual(d.DayStatuses, want[i:i+1]) {
				t.Fatal("guessed ambiguous truck", investor)
			}
		}
		report.WeekStart = "2026-10-12"
		if err = decoratePayDayStatuses(ctx, tx, "driver", &report); err != nil || len(report.Drivers[0].DayStatuses) != 0 || len(report.Drivers[1].DayStatuses) != 1 || report.Drivers[1].DayStatuses[0].Text != "Next week" {
			t.Fatal("ended team routing", report, err)
		}
		report.Drivers[0].Settlement = &PayrollSettlement{Finalized: true}
		report.Drivers[0].DayStatuses = nil
		before, _ := json.Marshal(report.Drivers[0])
		if err = decoratePayDayStatuses(ctx, tx, "driver", &report); err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(report.Drivers[0])
		if string(before) != string(after) {
			t.Fatal("legacy frozen metadata changed")
		}
	})
}

func TestPayrollPlanningNotificationsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := admin.Exec(ctx, `LISTEN payroll_projection_visible`); err != nil {
			t.Fatal(err)
		}
		wait := func(want bool) {
			t.Helper()
			deadline, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
			defer cancel()
			n, err := admin.WaitForNotification(deadline)
			if want && (err != nil || n == nil || n.Channel != "payroll_projection_visible" || n.Payload != "") {
				t.Fatal("missing display refresh", n, err)
			}
			if !want && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("unexpected display refresh", n, err)
			}
		}
		board := NewGrossBoardRepository(pool)
		rows := []GrossBoardPlanningEntry{{DriverID: projectionDriver, Date: "2026-10-06", Kind: "note", Text: "First"}}
		rows, err := board.SavePlanningChanges(ctx, rows, "")
		if err != nil {
			t.Fatal(err)
		}
		wait(true)
		if _, err = board.SavePlanningChanges(ctx, rows, ""); err != nil {
			t.Fatal(err)
		}
		wait(false)
		stale := rows[0]
		stale.Version = 0
		stale.Text = "Stale"
		if _, err = board.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{{DriverID: projectionDriver, Date: "2026-10-06", Kind: "destination", Text: "Roll back"}, stale}, ""); !errors.Is(err, ErrGrossBoardConflict) {
			t.Fatal("expected rollback", err)
		}
		wait(false)
		rows[0].Text = ""
		if _, err = board.SavePlanningChanges(ctx, rows, ""); err != nil {
			t.Fatal(err)
		}
		wait(true)
	})
}

func TestPayrollPlanningSaveReceiptDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Planning receipt',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		pay.preparedCommands = true
		pay.resourceConcurrency = true
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if done, err := pay.PrepareNextPayrollWeek(ctx); err != nil || !done {
			t.Fatal(done, err)
		}
		board := NewGrossBoardRepository(pool)
		planning := []GrossBoardPlanningEntry{{DriverID: projectionDriver, Date: "2026-10-06", Kind: "note"}}
		for _, text := range []string{"First label", "Changed label", ""} {
			planning[0].Text = text
			var err error
			planning, err = board.SavePlanningChanges(ctx, planning, actor)
			if err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"driver", "investor"} {
				id := projectionDriver
				if kind == "investor" {
					id = projectionTruck
				}
				page, err := pay.PreparedPayroll(ctx, kind, week, PayPageQuery{ID: id}, true)
				if err != nil {
					t.Fatal(err)
				}
				edits := page.Drivers[0].Edits
				edits.Notes = "Reviewed " + text
				edits.ExpectedStatementRevision = page.Drivers[0].StatementRevision
				if err = pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&edits.RequestID); err != nil {
					t.Fatal(err)
				}
				measured, metrics := db.WithMetrics(WithPayrollReceipt(ctx))
				var saved DriverPayEdits
				if kind == "driver" {
					saved, err = pay.Save(measured, edits, actor)
				} else {
					saved, err = pay.SaveInvestorPay(measured, edits, actor)
				}
				if err != nil || saved.CommittedStatement == nil {
					t.Fatal("save receipt", kind, err)
				}
				statuses := saved.CommittedStatement.DayStatuses
				if text == "" && len(statuses) != 0 || text != "" && (len(statuses) != 1 || statuses[0].Text != text || statuses[0].Status != "CUSTOM") {
					t.Fatal("stale committed statement labels", kind, text, statuses)
				}
				if metrics.Events()["payroll_calculation"] != 0 {
					t.Fatal("planning forced save recalculation", kind, metrics.Events())
				}
			}
		}
	})
}

func TestPayrollPlanningFinalizationRaceDatabase(t *testing.T) {
	for _, mode := range []string{"global", "resources"} {
		t.Run(mode, func(t *testing.T) {
			withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				seedPayrollProjectionTest(t, ctx, pool)
				var actor string
				if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Planning race',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
					t.Fatal(err)
				}
				board := NewGrossBoardRepository(pool)
				planning, err := board.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{{DriverID: projectionDriver, Date: "2026-10-06", Kind: "note", Text: "Frozen first"}}, actor)
				if err != nil {
					t.Fatal(err)
				}
				pay := NewDriverPayRepository(pool)
				pay.ConfigurePreparedFinalization(true)
				pay.resourceConcurrency = mode == "resources"
				if err = pay.EnablePayrollShadow(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err = pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode=$1`, mode); err != nil {
					t.Fatal(err)
				}
				week, _ := chargeWeek("2026-10-05")
				if err = pay.RequestPayrollPreparation(ctx, week, false); err != nil {
					t.Fatal(err)
				}
				if done, err := pay.PrepareNextPayrollWeek(ctx); err != nil || !done {
					t.Fatal(done, err)
				}
				page, err := pay.PreparedPayroll(ctx, "driver", week, PayPageQuery{ID: projectionDriver}, true)
				if err != nil {
					t.Fatal(err)
				}
				// Pause the real finalizer after its report read and before the frozen row
				// is inserted. A planning save must wait at the earlier source gate.
				if _, err = admin.Exec(ctx, `CREATE FUNCTION pause_planning_finalization() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(207,206); RETURN NEW; END $$; CREATE TRIGGER pause_planning_finalization BEFORE INSERT ON payroll_settlements FOR EACH ROW EXECUTE FUNCTION pause_planning_finalization()`); err != nil {
					t.Fatal(err)
				}
				barrier, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer barrier.Rollback(context.Background())
				if _, err = barrier.Exec(ctx, `SELECT pg_advisory_xact_lock(207,206)`); err != nil {
					t.Fatal(err)
				}
				type finalResult struct {
					report DriverPayWeek
					err    error
				}
				final := make(chan finalResult, 1)
				go func() {
					report, err := pay.Settle(WithPayrollReceipt(ctx), week, projectionDriver, page.Revision, actor, "", false, true, PayrollSettlementCommand{RequestID: "20600000-0000-0000-0000-000000000001", ExpectedStatementRevision: page.Drivers[0].StatementRevision, ExpenseRevision: page.Drivers[0].ExpenseRevision})
					final <- finalResult{report, err}
				}()
				waitLock := func(sql string) {
					t.Helper()
					deadline := time.Now().Add(5 * time.Second)
					for {
						var blocked bool
						if err := admin.QueryRow(ctx, sql).Scan(&blocked); err != nil {
							t.Fatal(err)
						}
						if blocked {
							return
						}
						if time.Now().After(deadline) {
							t.Fatal("operation did not reach expected lock")
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
				waitLock(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=207 AND objid=206 AND NOT granted)`)
				planning[0].Text = "Edited after freeze"
				saved := make(chan error, 1)
				go func() { _, err := board.SavePlanningChanges(ctx, planning, actor); saved <- err }()
				waitLock(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=736281940 AND NOT granted)`)
				if err = barrier.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				completed := <-final
				if completed.err != nil {
					t.Fatal("finalize race", completed.err)
				}
				if err = <-saved; err != nil {
					t.Fatal("planning race", err)
				}
				got, err := pay.PreparedPayroll(ctx, "driver", week, PayPageQuery{ID: projectionDriver}, true)
				if err != nil || len(got.Drivers[0].DayStatuses) != 1 || got.Drivers[0].DayStatuses[0].Text != "Frozen first" {
					t.Fatal("race rewrote frozen label", err, got.Drivers)
				}
				live, err := pay.PreparedPayroll(ctx, "investor", week, PayPageQuery{ID: projectionTruck}, true)
				if err != nil || len(live.Drivers[0].DayStatuses) != 1 || live.Drivers[0].DayStatuses[0].Text != "Edited after freeze" {
					t.Fatal("race lost open label", err, live.Drivers)
				}
			})
		})
	}
}
