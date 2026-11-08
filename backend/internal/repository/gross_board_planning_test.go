package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGrossBoardPlanningMigrationContract(t *testing.T) {
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/206_gross_board_planning_entries.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ReplaceAll(string(source), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n")) {
		t.Fatal("fresh schema does not contain the exact planning migration")
	}
	if !strings.Contains(string(migration), "'gross_board_planning_entries','unrelated'") {
		t.Fatal("missing payroll classification")
	}
}

func TestGrossBoardPlanningDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_GROSS_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("local disposable MSERP_GROSS_BOARD_TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/206_gross_board_planning_entries.sql")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, ok := strings.Cut(strings.ReplaceAll(string(source), "\r\n", "\n"), strings.ReplaceAll(string(migration), "\r\n", "\n"))
	if !ok {
		t.Fatal("missing planning migration")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			name := fmt.Sprintf("gross_planning_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{name}.Sanitize()
			exec := func(sql string) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() {
				_, _ = admin.Exec(context.Background(), `UNLISTEN *; RESET ROLE; SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			}()
			if mode == "fresh" {
				exec(string(source))
			} else {
				exec(prefix)
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = name + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, err := pgxpool.NewWithConfig(ctx, app)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			seedPayrollProjectionTest(t, ctx, pool)
			exec(`INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,version) VALUES('` + projectionDriver + `','2026-10-05',1,'84X0165 transferred to another driver',7);
 INSERT INTO gross_board_entries(driver_id,service_date,day_status,version) VALUES('` + projectionDriver + `','2026-10-06','HOME',4)`)
			if mode == "migration" {
				exec(string(migration))
			}
			var unchanged bool
			if err := pool.QueryRow(ctx, `SELECT (SELECT load_number='84X0165 transferred to another driver' AND version=7 FROM gross_board_extra_entries WHERE driver_id=$1 AND service_date='2026-10-05' AND slot=1) AND (SELECT day_status='HOME' AND version=4 FROM gross_board_entries WHERE driver_id=$1 AND service_date='2026-10-06')`, projectionDriver).Scan(&unchanged); err != nil || !unchanged {
				t.Fatal("migration reclassified legacy data", err)
			}
			var owner, classification string
			if err := pool.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=current_schema() AND tablename='gross_board_planning_entries'`).Scan(&owner); err != nil || owner != "mserp_app" {
				t.Fatal("planning ownership", owner, err)
			}
			if err := pool.QueryRow(ctx, `SELECT classification FROM payroll_projection_sources WHERE table_name='gross_board_planning_entries'`).Scan(&classification); err != nil || classification != "unrelated" {
				t.Fatal("planning payroll classification", classification, err)
			}
			verifyGrossBoardPlanning(t, ctx, pool, admin)
		})
	}
}

func verifyGrossBoardPlanning(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
	t.Helper()
	repo := NewGrossBoardRepository(pool)
	week, _ := time.Parse(time.DateOnly, "2026-10-05")
	queue := NewDriverBoardRepository(pool)
	queue.now = func() time.Time { return week.Add(12 * time.Hour) }
	pay := NewDriverPayRepository(pool)
	if err := pay.EnablePayrollShadow(ctx); err != nil {
		t.Fatal("planning breaks source coverage", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE payroll_projection_control SET protocol_mode='resources'`); err != nil {
		t.Fatal(err)
	}
	if err := pay.RequestPayrollPreparation(ctx, week, false); err != nil {
		t.Fatal(err)
	}
	if done, err := pay.PrepareNextPayrollWeek(ctx); err != nil || !done {
		t.Fatal("prepare baseline", done, err)
	}
	// A previous client sends only its load fields. Planning must not share that
	// record/version and old manual loads must still be queued before any import.
	oldLoad, err := repo.SaveChanges(ctx, []GrossBoardEntry{{DriverID: projectionDriver, Date: "2026-10-07", LoadNumber: "MANUAL-NOT-IN-DATATRUCK", OriginalRate: "120", DriverRate: "100", Miles: "40"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	statusWordLoad, err := repo.SaveChanges(ctx, []GrossBoardEntry{{DriverID: projectionDriver, Date: "2026-10-09", LoadNumber: "HOME", DayStatus: ""}}, "")
	if err != nil || len(statusWordLoad) != 1 || statusWordLoad[0].DayStatus != "" || statusWordLoad[0].LoadNumber != "HOME" {
		t.Fatal("explicit status-word load was reclassified", statusWordLoad, err)
	}
	beforeQueue, err := queue.Loads(ctx, projectionDriver, week.Format(time.DateOnly))
	if err != nil {
		t.Fatal(err)
	}
	foundManual, foundLegacy, foundStatusWord := false, false, false
	for _, load := range beforeQueue.Next {
		foundManual = foundManual || load.Number == "MANUAL-NOT-IN-DATATRUCK"
		foundLegacy = foundLegacy || load.Number == "84X0165 transferred to another driver"
		foundStatusWord = foundStatusWord || load.Number == "HOME"
	}
	if !foundManual || !foundLegacy || !foundStatusWord {
		t.Fatal("manual/legacy loads left queue", beforeQueue)
	}
	financial := func() string {
		t.Helper()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SET LOCAL jit=off`); err != nil {
			t.Fatal(err)
		}
		if err = payrollProjectionCoverage(ctx, tx); err != nil {
			t.Fatal(err)
		}
		fence, err := payrollProjectionFence(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		cache, err := payrollCacheSnapshot(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		board, err := readGrossBoard(ctx, tx, week)
		if err != nil {
			t.Fatal(err)
		}
		if len(board.PlanningEntries) != 0 {
			t.Fatal("planning leaked into shared payroll board")
		}
		driver, err := readDriverPayWeek(ctx, tx, week, "")
		if err != nil {
			t.Fatal(err)
		}
		investor, err := readInvestorPay(ctx, tx, week)
		if err != nil {
			t.Fatal(err)
		}
		var state string
		err = tx.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY kind,statement_id,week_start) FROM payroll_statement_current t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY week_start) FROM payroll_projection_jobs t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM loads t),
 (SELECT jsonb_agg(to_jsonb(t)) FROM expense_sheet_exports t),
 (SELECT count(*) FROM payroll_projection_changes))::text`).Scan(&state)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal([]any{fence, cache, board, driver, investor, state})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before := financial()
	// Planning saves must work while a financial command owns its source gate.
	hold, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(736281940)`); err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	entries := []GrossBoardPlanningEntry{{DriverID: projectionDriver, Date: "2026-10-07", Kind: "note", Text: "private family emergency"}, {DriverID: projectionDriver, Date: "2026-10-07", Kind: "destination", Text: "Dallas, TX"}}
	saved, err := repo.SavePlanningChanges(work, entries, "")
	cancel()
	_ = hold.Rollback(ctx)
	if err != nil || len(saved) != 2 || saved[0].Version != 1 || saved[1].Version != 1 {
		t.Fatal("independent planning save", saved, err)
	}
	if after := financial(); after != before {
		t.Fatal("planning changed financial/report/export/provider state")
	}
	afterQueue, err := queue.Loads(ctx, projectionDriver, week.Format(time.DateOnly))
	if err != nil || !reflect.DeepEqual(beforeQueue, afterQueue) {
		t.Fatal("planning changed queue or revision", err)
	}
	board, err := repo.Get(ctx, week)
	if err != nil || len(board.PlanningEntries) != 2 {
		t.Fatal("dispatch read missing planning", board.PlanningEntries, err)
	}
	otherWeek, err := repo.Get(ctx, week.AddDate(0, 0, 7))
	if err != nil || len(otherWeek.PlanningEntries) != 0 {
		t.Fatal("planning crossed weeks", err)
	}
	// An actual old full-row save, followed by clearing its load, preserves both
	// planning kinds and cannot leak planning text into native accounting records.
	oldLoad[0].DriverRate = "95"
	oldLoad[0].Note = nil
	oldLoad, err = repo.SaveChanges(ctx, oldLoad, "")
	if err != nil {
		t.Fatal(err)
	}
	oldLoad[0].LoadNumber = ""
	oldLoad[0].LoadRecordID = nil
	oldLoad[0].DriverRate = ""
	oldLoad[0].OriginalRate = ""
	oldLoad[0].Miles = ""
	oldLoad[0].EnteredOriginalRate = ""
	oldLoad[0].EnteredMiles = ""
	if _, err = repo.SaveChanges(ctx, oldLoad, ""); err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil || !reflect.DeepEqual(board.PlanningEntries, []GrossBoardPlanningEntry{saved[1], saved[0]}) {
		t.Fatal("old load save cleared planning", board.PlanningEntries, err)
	}
	listenerConfig := admin.Config().Copy()
	// A connected pgx config carries the original connection's notification
	// callback. A separate listener must install its own notification buffer.
	listenerConfig.OnNotification = nil
	listener, err := pgx.ConnectConfig(ctx, listenerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close(ctx)
	if _, err = listener.Exec(ctx, `LISTEN mserp_gross_board`); err != nil {
		t.Fatal(err)
	}
	expectNotification := func(want bool) {
		t.Helper()
		wait, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		n, err := listener.WaitForNotification(wait)
		if want && (err != nil || n == nil || n.Channel != "mserp_gross_board") {
			t.Fatal("missing committed board event", n, err)
		}
		if !want && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("no-op/rollback emitted board event", n, err)
		}
	}
	retried, err := repo.SavePlanningChanges(ctx, entries, "")
	if err != nil || !reflect.DeepEqual(retried, saved) {
		t.Fatal("identical retry", retried, err)
	}
	expectNotification(false)
	// Different kinds can independently save from the same day snapshot.
	changed := saved[0]
	changed.Text = "private revised note"
	updated, err := repo.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{changed}, "")
	if err != nil || updated[0].Version != 2 {
		t.Fatal(updated, err)
	}
	expectNotification(true)
	stale := saved[0]
	stale.Text = "stale overwrite"
	if _, err = repo.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{stale}, ""); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal("stale edit accepted", err)
	}
	expectNotification(false)
	// A conflict rolls back a different kind edited earlier in the same batch.
	other := saved[1]
	other.Text = "Austin, TX"
	if _, err = repo.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{other, stale}, ""); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal("batch conflict", err)
	}
	expectNotification(false)
	deleted := updated[0]
	deleted.Text = ""
	removed, err := repo.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{deleted}, "")
	if err != nil || removed[0].Version != 3 {
		t.Fatal("planning deletion", removed, err)
	}
	expectNotification(true)
	if _, err = repo.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{entries[0]}, ""); !errors.Is(err, ErrGrossBoardConflict) {
		t.Fatal("stale recreation accepted", err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil || len(board.PlanningEntries) != 2 || board.PlanningEntries[1].Text != "" || board.PlanningEntries[1].Version != 3 || board.PlanningEntries[0].Text != "Dallas, TX" {
		t.Fatal("tombstone or independent destination lost", board.PlanningEntries, err)
	}
	// Absent-row races and existing-row races both allow one differing writer.
	for _, date := range []string{"2026-10-08", "2026-10-07"} {
		version := 0
		if date == "2026-10-07" {
			version = 3
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, text := range []string{"concurrent A", "concurrent B"} {
			wg.Add(1)
			go func(text string) {
				defer wg.Done()
				<-start
				_, err := repo.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{{DriverID: projectionDriver, Date: date, Kind: "note", Text: text, Version: version}}, "")
				results <- err
			}(text)
		}
		close(start)
		wg.Wait()
		close(results)
		success, conflicts := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, ErrGrossBoardConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflicts != 1 {
			t.Fatal("concurrent writers", success, conflicts)
		}
	}
	if _, err = listener.Exec(ctx, `UNLISTEN *`); err != nil {
		t.Fatal(err)
	}
	// Planning-only historical visibility is dispatch-only, not a payroll roster
	// input. Clearing the last kind hides the inactive driver but keeps versions.
	var inactive string
	if err = pool.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,active,hire_date,termination_date) VALUES('Planning Inactive','planning inactive','cpm',0.5,false,'2026-01-01','2026-09-01') RETURNING id::text`).Scan(&inactive); err != nil {
		t.Fatal(err)
	}
	plan, err := repo.SavePlanningChanges(ctx, []GrossBoardPlanningEntry{{DriverID: inactive, Date: "2026-10-05", Kind: "note", Text: "available later"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, d := range board.Drivers {
		visible = visible || d.ID == inactive
	}
	if !visible {
		t.Fatal("planning-only history disappeared")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	financialBoard, err := readGrossBoard(ctx, tx, week)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range financialBoard.Drivers {
		if d.ID == inactive {
			t.Fatal("planning-only driver leaked into payroll")
		}
	}
	plan[0].Text = ""
	if _, err = repo.SavePlanningChanges(ctx, plan, ""); err != nil {
		t.Fatal(err)
	}
	board, err = repo.Get(ctx, week)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range board.Drivers {
		if d.ID == inactive {
			t.Fatal("empty planning retained inactive roster")
		}
	}
}
