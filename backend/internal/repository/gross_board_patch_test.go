package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/db"
)

func TestGrossBoardPatchPublicationDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		gross := NewGrossBoardRepository(pool)
		patch := GrossBoardEntry{DriverID: projectionDriver, Date: "2026-10-05", DriverRate: "775", Fields: []string{"driverRate"}}
		saved, err := gross.SaveEntries(WithPayrollPublication(ctx), []GrossBoardEntry{patch}, "")
		if err != nil || len(saved) != 1 || saved[0].DriverRate != "775.00" || saved[0].LoadNumber != "PROJECTION-1" {
			t.Fatal(saved, err)
		}
		reference, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, reference, week, true)
		_ = reference.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			after, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(after.Report)
			if err != nil || after.State != "ready" || string(a) != string(b) {
				t.Fatal("field save broke atomic payroll publication", kind, err)
			}
		}
		if _, err = admin.Exec(ctx, `CREATE FUNCTION fail_gross_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test publication failure'; END $$;
 CREATE TRIGGER fail_gross_publication BEFORE INSERT ON payroll_projection_generations FOR EACH ROW EXECUTE FUNCTION fail_gross_publication()`); err != nil {
			t.Fatal(err)
		}
		patch.DriverRate = "650"
		if _, err = gross.SaveEntries(WithPayrollPublication(ctx), []GrossBoardEntry{patch}, ""); err == nil {
			t.Fatal("publication failure acknowledged as saved")
		}
		var rate string
		if err = pool.QueryRow(ctx, `SELECT driver_rate::text FROM gross_board_entries WHERE driver_id=$1 AND service_date=$2`, projectionDriver, week).Scan(&rate); err != nil || rate != "775.00" {
			t.Fatal("failed publication did not roll back plan", rate, err)
		}
	})
}

func TestGrossBoardAsyncSaveDatabase(t *testing.T) {
	for _, protocol := range []string{"global", "resources"} {
		t.Run(protocol, func(t *testing.T) { testGrossBoardAsyncSave(t, protocol) })
	}
}

func testGrossBoardAsyncSave(t *testing.T, protocol string) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		pay := NewDriverPayRepository(pool)
		pay.ConfigurePreparedFinalization(true)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode=$1 WHERE singleton`, protocol); err != nil {
			t.Fatal(err)
		}
		week, _ := chargeWeek("2026-10-05")
		if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.PrepareNextPayrollWeek(ctx); err != nil {
			t.Fatal(err)
		}
		before, err := pay.PreparedPayroll(ctx, "driver", week, PayPageQuery{}, true)
		if err != nil {
			t.Fatal(err)
		}
		var actor string
		if err = pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash) VALUES('Gross Async','$2a$test') RETURNING id::text`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		// A broken calculator cannot prevent durable board writes. No worker is
		// running, so success and live reads must be independent of publication.
		if _, err = admin.Exec(ctx, `CREATE FUNCTION fail_async_gross_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test publication failure'; END $$;
 CREATE TRIGGER fail_async_gross_publication BEFORE INSERT ON payroll_projection_generations FOR EACH ROW EXECUTE FUNCTION fail_async_gross_publication()`); err != nil {
			t.Fatal(err)
		}
		gross := NewGrossBoardRepository(pool)
		listener, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close(ctx)
		if _, err = listener.Exec(ctx, `LISTEN mserp_gross_board`); err != nil {
			t.Fatal(err)
		}
		patch := GrossBoardEntry{DriverID: projectionDriver, Date: "2026-10-05", DriverRate: "775", Fields: []string{"driverRate"}}
		for _, rate := range []string{"775", "780", "790"} {
			patch.DriverRate = rate
			measured, metrics := db.WithMetrics(WithPayrollPublication(ctx))
			start := time.Now()
			saved, err := gross.SaveChanges(measured, []GrossBoardEntry{patch}, actor)
			if err != nil || len(saved) != 1 || saved[0].DriverRate != rate+".00" {
				t.Fatal(saved, err)
			}
			if metrics.Events()["payroll_calculation"] != 0 {
				t.Fatal("board save waited for payroll calculations")
			}
			t.Logf("durable board save %s: %.1fms, queries=%d, calculations=0", rate, float64(time.Since(start).Microseconds())/1000, metrics.Queries.Load())
		}
		board, err := NewGrossBoardRepository(pool).Get(ctx, week)
		if err != nil || len(board.Entries) != 1 || board.Entries[0].DriverRate != "790.00" {
			t.Fatal("saved edit not immediately readable", board.Entries, err)
		}
		notifyCtx, cancel := context.WithTimeout(ctx, time.Second)
		_, err = listener.WaitForNotification(notifyCtx)
		cancel()
		if err != nil {
			t.Fatal("board was not broadcast before payroll preparation", err)
		}
		for _, kind := range []string{"driver", "investor"} {
			page, err := pay.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			if err != nil || page.State != "updating" {
				t.Fatal("stale payroll presented as ready", kind, page.State, err)
			}
		}
		_, err = pay.Settle(ctx, week, projectionDriver, before.Revision, actor, "", false, true,
			PayrollSettlementCommand{RequestID: "18200000-0000-0000-0000-000000000099", ExpectedStatementRevision: before.Drivers[0].StatementRevision})
		if !errors.Is(err, ErrDriverPayConflict) {
			t.Fatal("stale payroll finalization accepted", err)
		}
		// A newly constructed worker proves the pending work is in PostgreSQL,
		// not a request goroutine or process-local queue.
		worker := NewDriverPayRepository(pool)
		if n, err := worker.DispatchPayrollChanges(ctx); err != nil || n == 0 {
			t.Fatal("missing durable work", n, err)
		}
		if _, err = worker.PrepareNextPayrollWeek(ctx); err == nil {
			t.Fatal("injected worker failure was not exercised")
		}
		board, err = gross.Get(ctx, week)
		if err != nil || board.Entries[0].DriverRate != "790.00" {
			t.Fatal("worker failure lost board edit", err)
		}
		if _, err = admin.Exec(ctx, `DROP TRIGGER fail_async_gross_publication ON payroll_projection_generations`); err != nil {
			t.Fatal(err)
		}
		if err = worker.RequestPayrollPreparation(ctx, week, true); err != nil {
			t.Fatal(err)
		}
		if worked, err := worker.PrepareNextPayrollWeek(ctx); err != nil || !worked {
			t.Fatal("durable recovery failed", worked, err)
		}
		reference, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		full, err := readPayrollProjectionReference(ctx, reference, week, true)
		_ = reference.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"driver", "investor"} {
			page, err := worker.ReadPreparedPayrollPage(ctx, kind, week, PayPageQuery{})
			a, _ := json.Marshal(PaginatePay(full[i], PayPageQuery{}))
			b, _ := json.Marshal(page.Report)
			if err != nil || page.State != "ready" || string(a) != string(b) {
				t.Fatal("async payroll differs from reference", kind, page.State, err)
			}
		}
		preview, err := pay.PayrollPreview(ctx, "driver", week)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pay.Settle(ctx, week, projectionDriver, preview.Revision, actor, "", false, true); err != nil {
			t.Fatal(err)
		}
		var frozenBefore, frozenAfter []byte
		if err = pool.QueryRow(ctx, `SELECT report FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2`, projectionDriver, week).Scan(&frozenBefore); err != nil {
			t.Fatal(err)
		}
		patch.DriverRate = "810"
		if _, err = gross.SaveChanges(WithPayrollPublication(ctx), []GrossBoardEntry{patch}, actor); err != nil {
			t.Fatal(err)
		}
		if _, err = worker.DispatchPayrollChanges(ctx); err != nil {
			t.Fatal(err)
		}
		if worked, err := worker.PrepareNextPayrollWeek(ctx); err != nil || !worked {
			t.Fatal(worked, err)
		}
		if err = pool.QueryRow(ctx, `SELECT report FROM payroll_settlements WHERE driver_id=$1 AND week_start=$2`, projectionDriver, week).Scan(&frozenAfter); err != nil || string(frozenAfter) != string(frozenBefore) {
			t.Fatal("background publication changed frozen financial snapshot", err)
		}
	})
}
