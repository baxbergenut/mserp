package repository

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"mserp/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBoardPlanCacheKeepsStopEvidence(t *testing.T) {
	var cache boardWeekCache[map[string][]BoardLoad]
	lat := 43.0
	read := func() (map[string][]BoardLoad, error) {
		return map[string][]BoardLoad{"d": {{queueAlias: "alias", Stops: []BoardStop{{geocode: &boardStopGeocode{state: "TX"}, Latitude: &lat, Schedule: &BoardSchedule{Label: "pickup"}}}}}}, nil
	}
	first, err := cache.get(context.Background(), "one", cloneBoardPlans, read)
	if err != nil {
		t.Fatal(err)
	}
	first["d"][0].Stops[0].geocode.state = "other"
	*first["d"][0].Stops[0].Latitude = 20
	first["d"][0].Stops[0].Schedule.Label = ""
	second, err := cache.get(context.Background(), "one", cloneBoardPlans, func() (map[string][]BoardLoad, error) { t.Fatal("unexpected second read"); return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	p := second["d"][0]
	stop := p.Stops[0]
	if p.queueAlias != "alias" || stop.geocode == nil || stop.geocode.state != "TX" || *stop.Latitude != 43 || stop.Schedule.Label != "pickup" {
		t.Fatal("cached stop evidence changed")
	}
}

func TestSharedBoardPendingViewerDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		_, err := pool.Exec(ctx, `INSERT INTO app_roles(name,permissions) VALUES ('shared-board-direct','{}'),('shared-board-role','{}');
 INSERT INTO app_users(username,email,password_hash,role_id,active)
 SELECT n,n||'@example.test',crypt('fixture',gen_salt('bf')),r.id,n<>'inactive' FROM (VALUES ('direct'),('member'),('other'),('inactive')) v(n)
 JOIN app_roles r ON r.name=CASE WHEN n IN ('member','inactive') THEN 'shared-board-role' ELSE 'shared-board-direct' END;
 INSERT INTO fleetscope_driver_intake(company_id,fleetscope_driver_id,driver_data,normalized_name,occurred_at)
 VALUES(gen_random_uuid(),gen_random_uuid(),'{"fullName":"Pending fixture"}','pending fixture',now());
 UPDATE system_task_assignments SET assignee_ids=ARRAY[(SELECT id FROM app_users WHERE username='direct')],
 assignee_role_ids=ARRAY[(SELECT id FROM app_roles WHERE name='shared-board-role')] WHERE kind='driver_onboarding'`)
		if err != nil {
			t.Fatal(err)
		}
		week, _ := time.Parse(time.DateOnly, "2026-11-16")
		repo := NewDriverBoardRepository(pool)
		shared, err := repo.GetShared(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		check := func(name string, admin bool, want bool) {
			t.Helper()
			var id string
			if err := pool.QueryRow(ctx, `SELECT id FROM app_users WHERE username=$1`, name).Scan(&id); err != nil {
				t.Fatal(err)
			}
			pending := shared.ForViewer(id, admin).PendingDrivers
			if (len(pending) == 1) != want || (want && pending[0].FullName != "Pending fixture") || (!want && len(pending) != 0) {
				t.Fatalf("viewer %s admin %t: %d pending", name, admin, len(pending))
			}
		}
		check("other", false, false)
		check("direct", false, true)
		check("member", false, true)
		check("inactive", false, false)
		check("other", true, true)
		if _, err = pool.Exec(ctx, `UPDATE system_task_assignments SET assignee_ids='{}',assignee_role_ids='{}',assignee_id=NULL WHERE kind='driver_onboarding'`); err != nil {
			t.Fatal(err)
		}
		shared, err = repo.GetShared(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		check("direct", false, false)
		check("member", false, false)
	})
}

func TestSharedBoardWeekEntriesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		week := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
		gross := NewGrossBoardRepository(pool)
		status := gross.StatusBoard(false)
		gridRead := func(name string, wantEntries, wantOpenings int) {
			t.Helper()
			measured, metrics := db.WithMetrics(ctx)
			var err error
			if name == "status" {
				_, err = status.GetShared(measured, week)
			} else {
				_, err = gross.Get(measured, week)
			}
			counts := metrics.Events()
			if err != nil || int(counts["board_week_entries_queries"]) != wantEntries || int(counts["gross_opening_balances_queries"]) != wantOpenings {
				t.Fatalf("%s: entry queries %v, opening queries %v: %v", name, counts["board_week_entries_queries"], counts["gross_opening_balances_queries"], err)
			}
		}
		gridRead("status", 1, 0)
		gridRead("gross", 0, 1)
		gridRead("gross", 0, 0)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		before, err := boardWeekRevision(ctx, tx, week, true)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO gross_board_rate_resets(driver_id,week_start,target,week_change) VALUES(gen_random_uuid(),$1,50,0)`, week)
		if err != nil {
			t.Fatal(err)
		}
		after, err := boardWeekRevision(ctx, tx, week, true)
		if err != nil || before.entries != after.entries || before.openings == after.openings {
			t.Fatal("reset must invalidate only opening balances", err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(1920101,'SHARED-WEEK-192','Dispatched',0,0,'{}')`); err != nil {
			t.Fatal(err)
		}
		gridRead("gross", 1, 1)
		gridRead("status", 0, 0)
	})
}

func TestBoardQueueSnapshotDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		repo := NewDriverBoardRepository(pool)
		week, _ := chargeWeek("2026-10-05")
		driver := []string{projectionDriver}
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := admin.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		begin := func() pgx.Tx {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}
		check := func(tx pgx.Tx, ids []string, wantRead bool) {
			t.Helper()
			keys, err := boardWeekRevision(ctx, tx, week, false)
			if err != nil {
				t.Fatal(err)
			}
			measured, metrics := db.WithMetrics(ctx)
			got, err := repo.readQueueSnapshot(measured, tx, keys.queues, ids)
			if err != nil {
				t.Fatal(err)
			}
			phases, _ := metrics.Details()
			_, read := phases["status_queue_payloads"]
			direct, err := repo.readQueueSnapshot(ctx, tx, "", ids)
			if err != nil || !reflect.DeepEqual(got, direct) || read != wantRead {
				t.Fatalf("direct match=%t payload read=%t want=%t: %v", reflect.DeepEqual(got, direct), read, wantRead, err)
			}
		}
		current := func(ids []string, wantRead bool) {
			t.Helper()
			tx := begin()
			defer tx.Rollback(ctx)
			check(tx, ids, wantRead)
		}
		current(driver, true)
		current(driver, false)
		exec(`INSERT INTO driver_board_load_state(driver_id,payload) VALUES($1,'{"orderLabels":["first"]}')
 ON CONFLICT(driver_id) DO UPDATE SET payload=excluded.payload`, projectionDriver)
		old := begin()
		defer old.Rollback(ctx)
		check(old, driver, true)
		exec(`UPDATE driver_board_load_state SET payload='{"orderLabels":["second"]}' WHERE driver_id=$1`, projectionDriver)
		current(driver, true)
		check(old, driver, true)
		current(driver, true)
		current(nil, true)
		current(driver, true)
		exec(`UPDATE fleet_status_control SET enabled=NOT enabled WHERE singleton`)
		current(driver, true)
		exec(`DELETE FROM driver_board_load_state WHERE driver_id=$1`, projectionDriver)
		current(driver, true)
		// Release the old snapshot's relation locks before checking DDL fallback.
		if err := old.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		exec(`ALTER TABLE driver_board_load_state DISABLE TRIGGER payroll_cache_insert`)
		exec(`INSERT INTO driver_board_load_state(driver_id,payload) VALUES($1,'{"orderLabels":["untracked"]}')`, projectionDriver)
		current(driver, true)
		current(driver, true)
		exec(`ALTER TABLE driver_board_load_state ENABLE TRIGGER payroll_cache_insert`)
		current(driver, true)
		current(driver, false)
		exec(`TRUNCATE driver_board_load_state`)
		current(driver, true)

		shadow, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer shadow.Rollback(ctx)
		if _, err = shadow.Exec(ctx, `CREATE TEMP TABLE payroll_cache_revisions ON COMMIT DROP AS TABLE payroll_cache_revisions`); err != nil {
			t.Fatal(err)
		}
		keys, err := boardWeekRevision(ctx, shadow, week, false)
		if err != nil || keys.queues != "" {
			t.Fatalf("shadowed revision table certified queue data: %q %v", keys.queues, err)
		}
	})
}

func TestBoardManualSourcesDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		var caches boardLoadSourceCaches
		states := map[string]boardLoadState{"driver": {Manual: []BoardLoad{{PlanID: "first", Number: "Manual-Cache-170"}}}}
		read := func() BoardLoad {
			t.Helper()
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			key, err := boardWeekRevision(ctx, tx, time.Now(), false)
			if err != nil || key.entries == "" {
				t.Fatalf("missing revision %q, %v", key.entries, err)
			}
			out, err := caches.readManual(ctx, tx, key.entries, states)
			if err != nil {
				t.Fatal(err)
			}
			return out["driver"][0]
		}
		if p := read(); p.LoadID != nil {
			t.Fatal("unexpected source")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES (1701001,'MANUAL-CACHE-170','Dispatched',0,0,'{}')`); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			p := read()
			if p.LoadID == nil || *p.LoadID != 1701001 || p.Number != "Manual-Cache-170" || p.PlanID != "first" {
				t.Fatal("new source missing on read", i)
			}
		}
		states["driver"] = boardLoadState{Manual: []BoardLoad{{PlanID: "second", Number: "manual-cache-170"}}}
		if p := read(); p.PlanID != "second" || p.Number != "manual-cache-170" {
			t.Fatal("manual queue edit was lost")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES (1701002,'MANUAL-CACHE-170','Dispatched',0,0,'{}')`); err != nil {
			t.Fatal(err)
		}
		if p := read(); p.LoadID != nil {
			t.Fatal("ambiguous source was selected")
		}
	})
}

func TestBoardRosterQueueInputsDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		week := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
		repo := NewDriverBoardRepository(pool)
		repo.now = func() time.Time { return week }
		if _, err := pool.Exec(ctx, `UPDATE fleet_status_control SET enabled=true WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		empty, err := repo.Get(ctx, week)
		if err != nil || !empty.StatusSyncEnabled || len(empty.Drivers) != 0 {
			t.Fatalf("empty roster with fleet sync: drivers=%d enabled=%v err=%v", len(empty.Drivers), empty.StatusSyncEnabled, err)
		}
		const driver = "19700000-0000-0000-0000-000000000001"
		const plan = "19700000-0000-0000-0000-000000000002"
		if _, err := pool.Exec(ctx, `INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('19700000-0000-0000-0000-000000000001','Queue Roster','queue roster','cpm',0);
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(1971001,'QUEUE-197','Delivered',0,0,'{}');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,plan_id) VALUES('19700000-0000-0000-0000-000000000001','2026-10-19','QUEUE-197',1971001,'19700000-0000-0000-0000-000000000002');`); err != nil {
			t.Fatal(err)
		}
		check := func(want bool) {
			t.Helper()
			board, err := repo.Get(ctx, week)
			if err != nil {
				t.Fatal(err)
			}
			view, ok := board.Loads[driver]
			if !ok || view.Current != nil {
				t.Fatal("retained queue became an active selection")
			}
			if got := slices.ContainsFunc(view.Unavailable, func(p BoardLoad) bool { return p.PlanID == plan }); got != want {
				t.Fatalf("historical completed plan visible = %v, want %v", got, want)
			}
		}
		check(false)
		if _, err := pool.Exec(ctx, `INSERT INTO driver_board_load_state(driver_id,payload) VALUES($1,jsonb_build_object('order',jsonb_build_array($2::text)))`, driver, plan); err != nil {
			t.Fatal(err)
		}
		check(true)
		if _, err := pool.Exec(ctx, `UPDATE driver_board_load_state SET payload='{}' WHERE driver_id=$1`, driver); err != nil {
			t.Fatal(err)
		}
		check(false)
	})
}
