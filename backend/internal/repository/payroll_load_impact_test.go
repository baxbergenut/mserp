package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPreparedPayrollSourceActionsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		const independent = "18200000-0000-0000-0000-000000001701"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES('18200000-0000-0000-0000-000000001701','Unrelated source','unrelated source','cpm',0.65,'2026-01-01');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate,entered_original_rate,entered_miles)
 VALUES('18200000-0000-0000-0000-000000001701','2026-10-05','UNRELATED',100,200,150),
 ('18200000-0000-0000-0000-000000000001','2026-10-11','TRANSFER-IMPACT',400,500,200);
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,raw_payload)
 VALUES(18203,'TRANSFER-IMPACT','delivered',400,500,200,'2026-10-11 12:00Z','{}');
 UPDATE loads SET total_pay=1300,total_miles=450 WHERE id=18201;
 INSERT INTO driver_pay_weeks(driver_id,week_start) VALUES
 ('18200000-0000-0000-0000-000000000001','2026-10-12'),('18200000-0000-0000-0000-000000000001','2026-10-19');
 INSERT INTO investor_pay_weeks(truck_id,week_start,owner_id,edits) SELECT
 '18200000-0000-0000-0000-000000000002',week::date,'18200000-0000-0000-0000-000000000003','{}'::jsonb
 FROM (VALUES('2026-10-12'),('2026-10-19')) w(week)`); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		weeks := []time.Time{}
		versions := map[string]string{}
		for _, date := range []string{"2026-10-05", "2026-10-12", "2026-10-19"} {
			week, _ := chargeWeek(date)
			weeks = append(weeks, week)
			if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.PrepareNextPayrollWeek(ctx); err != nil {
				t.Fatal(err)
			}
			page, err := repo.ReadPreparedPayrollPage(ctx, "driver", week, PayPageQuery{})
			if err != nil {
				t.Fatal(err)
			}
			versions[date] = page.Statements[independent].Revision
		}
		check := func() {
			t.Helper()
			for _, week := range weeks {
				tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				if err != nil {
					t.Fatal(err)
				}
				full, err := readPayrollProjectionReference(ctx, tx, week, true)
				_ = tx.Rollback(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for i, kind := range []string{"driver", "investor"} {
					page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
					a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
					b, _ := json.Marshal(page.Report)
					if err != nil || page.State != "ready" || string(a) != string(b) {
						t.Fatalf("source action parity %s %s: %s %v\n%s\n%s", kind, week, page.State, err, a, b)
					}
					if kind == "driver" && page.Statements[independent].Revision != versions[week.Format(time.DateOnly)] {
						t.Fatal("source action reconstructed an unrelated statement")
					}
				}
			}
		}
		write := WithPayrollPublication(ctx)
		var undo string
		steps := []struct {
			name string
			run  func() error
		}{
			{"accept source", func() error {
				var err error
				undo, err = repo.AcceptPaySourceWithUndo(write, PaySourceAcceptance{DriverID: projectionDriver, Date: "2026-10-05", Version: 1, LoadRecordID: 18201, OriginalRate: "1300.00", Miles: "450.00"}, "source-test")
				return err
			}},
			{"undo source", func() error { return repo.UndoPaySource(write, undo, "source-test") }},
			{"transfer weekend", func() error {
				return repo.TransferLoadWeek(write, LoadWeekTransferInput{DriverID: projectionDriver, Date: "2026-10-11", BoardVersion: 1, Mode: "everyone"}, "source-test")
			}},
			{"restore weekend", func() error {
				return repo.TransferLoadWeek(write, LoadWeekTransferInput{DriverID: projectionDriver, Date: "2026-10-11", BoardVersion: 1, TransferVersion: 1, Mode: "restore"}, "source-test")
			}},
			{"add historical load", func() error {
				return repo.PlacePayLoad(write, "driver", PayLoadPlacementInput{DriverID: projectionDriver, WeekStart: "2026-10-12", Action: "add", LoadNumber: "PROJECTION-1"}, "source-test")
			}},
			{"edit and remove placement", func() error {
				for _, action := range []string{"gross", "remove"} {
					page, err := repo.ReadPreparedPayrollPage(ctx, "driver", weeks[1], PayPageQuery{ID: projectionDriver})
					if err != nil || len(page.Report.Drivers) != 1 || len(page.Report.Drivers[0].Loads) != 1 {
						t.Fatal("missing placed load", page, err)
					}
					load := page.Report.Drivers[0].Loads[0]
					err = repo.PlacePayLoad(write, "driver", PayLoadPlacementInput{DriverID: projectionDriver, WeekStart: "2026-10-12", Action: action, LoadRecordID: 18201, CommentKey: load.CommentKey, BoardVersion: load.BoardVersion, PlacementVersion: load.PayrollPlacementVersion, DriverGross: "975"}, "source-test")
					if err != nil {
						return err
					}
					check()
				}
				return nil
			}},
			{"add after removed week", func() error {
				return repo.PlacePayLoad(write, "investor", PayLoadPlacementInput{DriverID: projectionTruck, WeekStart: "2026-10-19", Action: "add", LoadNumber: "PROJECTION-1"}, "source-test")
			}},
		}
		for _, step := range steps {
			if !t.Run(step.name, func(t *testing.T) {
				if err := step.run(); err != nil {
					t.Fatal(err)
				}
				check()
			}) {
				return
			}
		}
	})
}

func TestPayrollFrozenLoadImpactDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		var actor string
		if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Frozen source impact',crypt('test-only',gen_salt('bf'))) RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		repo := NewDriverPayRepository(pool)
		repo.ConfigurePreparedReads(true)
		week, _ := chargeWeek("2026-10-05")
		for _, kind := range []string{"driver", "investor"} {
			id := projectionDriver
			if kind == "investor" {
				id = projectionTruck
			}
			if _, err := settlePayrollFixture(t, ctx, repo, kind, week, id, "", actor, "", false, true, true); err != nil {
				t.Fatal(err)
			}
		}
		frozen := func() string {
			t.Helper()
			var value string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(report ORDER BY kind)::text FROM (
 SELECT 'driver' kind,report FROM payroll_settlements WHERE finalized UNION ALL
 SELECT 'investor',report FROM investor_pay_weeks WHERE finalized) s`).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := frozen()
		// Provider changes are allowed after finalization. Their current warnings
		// are published independently of the immutable financial statement.
		write := WithPayrollPublication(ctx)
		tx, err := beginPayrollMutation(write, pool)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `UPDATE loads SET total_pay=1300,total_miles=450 WHERE id=18201`); err != nil {
			t.Fatal(err)
		}
		impact, err := payrollMutationImpact(ctx, tx, &payrollCommand{name: "native", beforeSnapshot: tx.(*payrollMutationTransaction).beforeSnapshot}, "", "")
		if err != nil || impact == nil {
			t.Fatal("source impact was not resolved", impact, err)
		}
		if err = commitPayrollMutation(write, tx); err != nil {
			t.Fatal(err)
		}
		if after := frozen(); before != after {
			t.Fatal("source correction changed frozen financial JSON")
		}
		read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, read, week, true)
		_ = read.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			page, err := repo.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if err != nil || page.State != "ready" || string(a) != string(b) {
				t.Fatalf("frozen %s source parity: %s %v\n%s\n%s", kind, page.State, err, a, b)
			}
		}
	})
}

func TestPayrollLoadUpdateWeekScopeDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,driver_id,truck_id)
 VALUES(18202,'LATER-ONLY','delivered',600,700,300,$1,$2),
 (18203,'UNPLACED','delivered',600,700,300,$1,$2)`, projectionDriver, projectionTruck)
		exec(`INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate)
 VALUES($1,'2026-10-12','LATER-ONLY',18202,500)`, projectionDriver)

		repo := NewDriverPayRepository(pool)
		repo.preparedReads, repo.preparedCommands, repo.resourceConcurrency = true, true, true
		if err := repo.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE payroll_projection_control SET protocol_mode='resources'`)
		earlier, _ := chargeWeek("2026-10-05")
		later, _ := chargeWeek("2026-10-12")
		for _, week := range []time.Time{earlier, later} {
			if err := repo.RequestPayrollPreparation(ctx, week, false); err != nil {
				t.Fatal(err)
			}
			if worked, err := repo.PrepareNextPayrollWeek(ctx); !worked || err != nil {
				t.Fatal(worked, err)
			}
		}
		ready := func(week time.Time, want string) string {
			t.Helper()
			page, err := repo.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
			if err != nil || page.Preparation == nil || page.Preparation.State != want || len(page.Drivers) != 1 || page.Drivers[0].PreparationState != want {
				t.Fatalf("%s page state want %s: preparation=%+v rows=%d err=%v", week.Format(time.DateOnly), want, page.Preparation, len(page.Drivers), err)
			}
			return page.Drivers[0].StatementRevision
		}
		revision := ready(earlier, "ready")
		ready(later, "ready")
		// Both provider rows carry the same driver/truck as the older statement.
		// Only the native placement of the later load can affect payroll.
		exec(`UPDATE loads SET total_pay=total_pay+10 WHERE id IN (18202,18203)`)
		if ready(earlier, "ready") != revision {
			t.Fatal("unrelated loads changed the statement revision")
		}
		if _, err := repo.PreparedPayroll(ctx, "driver", earlier, PayPageQuery{}, false); err != nil {
			t.Fatal("completed-week export blocked by later and unplaced loads", err)
		}
		ready(later, "updating")
		if _, err := repo.DispatchPayrollChanges(ctx); err != nil {
			t.Fatal(err)
		}
		ready(earlier, "ready")
		for range 2 {
			if worked, err := repo.PrepareNextPayrollWeek(ctx); !worked || err != nil {
				t.Fatal(worked, err)
			}
		}
		ready(later, "ready")
		if ready(earlier, "ready") != revision {
			t.Fatal("preparation changed the unrelated statement revision")
		}

		// A later board edit in the same capture interval preserves both proofs.
		exec(`UPDATE loads SET total_pay=total_pay+10 WHERE id=18202`)
		exec(`UPDATE gross_board_entries SET entered_miles=310 WHERE load_record_id=18202`)
		ready(earlier, "ready")
		ready(later, "updating")

		// The later load acquiring an old number can break a prior number-only
		// match. The provider date/placement cannot exclude that earlier week.
		exec(`UPDATE loads SET load_id='PROJECTION-1' WHERE id=18202`)
		ready(earlier, "updating")
		ready(later, "updating")
	})
}
