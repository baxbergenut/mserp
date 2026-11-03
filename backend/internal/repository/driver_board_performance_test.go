package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStatusBoardOperationalDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, admin *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `UPDATE fleet_status_control SET enabled=true;
 INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 SELECT md5('board-performance-'||n)::uuid,'Board '||n,'board '||n,'cpm',0.65,'2026-01-01' FROM generate_series(1,20)n;
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload)
 SELECT 188000+n,'BOARD-'||n,'Dispatched',1500,1500,600,'{}' FROM generate_series(1,1000)n;
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate)
 SELECT md5('board-performance-'||n)::uuid,(now() AT TIME ZONE 'America/New_York')::date,'BOARD-'||n,188000+n,1400 FROM generate_series(1,20)n`); err != nil {
			t.Fatal(err)
		}
		pay := NewDriverPayRepository(pool)
		if err := pay.EnablePayrollShadow(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pay.SwitchPayrollProtocol(ctx, "resources", 1); err != nil {
			t.Fatal(err)
		}
		// Any synchronous payroll publication makes an operational save fail.
		if _, err := admin.Exec(ctx, `CREATE FUNCTION reject_board_payroll() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'board must not calculate payroll'; END $$;
 CREATE TRIGGER reject_board_payroll BEFORE INSERT ON payroll_projection_generations FOR EACH ROW EXECUTE FUNCTION reject_board_payroll()`); err != nil {
			t.Fatal(err)
		}
		rows, err := pool.Query(ctx, `SELECT d.id::text,e.load_number FROM drivers d JOIN gross_board_entries e ON e.driver_id=d.id WHERE normalized_name LIKE 'board %' ORDER BY d.id`)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		var loadNumbers []string
		for rows.Next() {
			var id, number string
			if err = rows.Scan(&id, &number); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
			loadNumbers = append(loadNumbers, number)
		}
		rows.Close()
		if len(ids) != 20 {
			t.Fatal("expected 20 independent users")
		}
		board := NewDriverBoardRepository(pool)
		// An unrelated payroll calculation may hold the source gate for seconds.
		// Operational dispatch still commits while that gate is held.
		blocker, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`); err != nil {
			t.Fatal(err)
		}
		fastCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err = board.Save(WithPayrollPublication(fastCtx), []DriverBoardEntry{{DriverID: projectionDriver, Notes: "does not wait for payroll", Status: "DISPATCHED", StatusEdited: true}})
		cancel()
		_ = blocker.Rollback(ctx)
		if err != nil {
			t.Fatal("operational board waited on payroll", err)
		}
		for _, query := range []string{
			`UPDATE drivers SET pay_rate=pay_rate+1 WHERE id=$1`,
			`INSERT INTO fleet_status_history(entity_kind,entity_id,entity_name,track,status,effective_at,actor_name,source) VALUES('driver',$1,'Guard fixture','availability','vacation',now(),'Test','test')`,
		} {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `SET LOCAL mserp.payroll.board_operational='on'`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, query, projectionDriver)
			_ = tx.Rollback(ctx)
			pgErr, ok := err.(*pgconn.PgError)
			if !ok || pgErr.ConstraintName != "board_requires_payroll_source" {
				t.Fatal("operational marker authorized financial mutation", err)
			}
		}
		entries := make([]DriverBoardEntry, 20)
		for i, id := range ids {
			entries[i] = DriverBoardEntry{DriverID: id}
		}
		var durations []time.Duration
		for round := 0; round < 6; round++ {
			start := make(chan struct{})
			var wg sync.WaitGroup
			errs := make([]error, 20)
			elapsed := make([]time.Duration, 20)
			for i := range entries {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					began := time.Now()
					e := entries[i]
					e.Notes = fmt.Sprintf("round %d", round)
					e.Status = "READY"
					if round%2 == 1 {
						e.Status = "DISPATCHED"
					}
					e.StatusEdited = true
					if round == 5 {
						e.CurrentLoad = loadNumbers[i]
						e.StatusEdited = false
					}
					result, err := board.Save(WithPayrollPublication(ctx), []DriverBoardEntry{e})
					elapsed[i] = time.Since(began)
					errs[i] = err
					if err == nil {
						entries[i] = result[0]
					}
				}(i)
			}
			close(start)
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if round == 5 {
				sort.Slice(elapsed, func(i, j int) bool { return elapsed[i] < elapsed[j] })
				t.Logf("20 simultaneous load selections among 1000 source loads: p50=%s p95=%s max=%s", elapsed[9], elapsed[18], elapsed[19])
			} else {
				durations = append(durations, elapsed...)
			}
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		t.Logf("20 simultaneous writers, %d committed saves: p50=%s p95=%s max=%s", len(durations), durations[49], durations[94], durations[99])
		var captured int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_changes WHERE table_name IN ('driver_board','driver_board_history','driver_board_load_state','fleet_status_history')`).Scan(&captured); err != nil || captured != 0 {
			t.Fatal("operational saves captured payroll work", captured, err)
		}
		// A real availability change commits and captures work without calculating.
		e := entries[0]
		var deadline, afterNotes, afterStatus time.Time
		if err = pool.QueryRow(ctx, `SELECT route_not_before FROM driver_board_automation WHERE driver_id=$1`, e.DriverID).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		e.Notes = "Notes do not postpone routing"
		noteSaved, err := board.Save(WithPayrollPublication(ctx), []DriverBoardEntry{e})
		if err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT route_not_before FROM driver_board_automation WHERE driver_id=$1`, e.DriverID).Scan(&afterNotes); err != nil || !deadline.Equal(afterNotes) {
			t.Fatal("notes restarted ETA pause", err)
		}
		e = noteSaved[0]
		e.Status = "ENROUTE"
		e.StatusEdited = true
		progressSaved, err := board.Save(WithPayrollPublication(ctx), []DriverBoardEntry{e})
		if err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT route_not_before FROM driver_board_automation WHERE driver_id=$1`, e.DriverID).Scan(&afterStatus); err != nil || !afterStatus.After(deadline) {
			t.Fatal("status change did not restart ETA pause", err)
		}
		e = progressSaved[0]
		e.Status = "HOME"
		e.StatusEdited = true
		saved, err := board.Save(WithPayrollPublication(ctx), []DriverBoardEntry{e})
		if err != nil {
			t.Fatal(err)
		}
		var availability string
		if err = pool.QueryRow(ctx, `SELECT availability_status FROM drivers WHERE id=$1`, e.DriverID).Scan(&availability); err != nil || availability != "home" {
			t.Fatal("profile did not synchronize", availability, err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM payroll_projection_changes WHERE table_name='drivers' AND dispatched_at IS NULL`).Scan(&captured); err != nil || captured == 0 {
			t.Fatal("canonical status change lost durable payroll capture", captured, err)
		}
		e = saved[0]
		e.Status = ""
		e.CurrentLoad = ""
		e.ResolveCurrentLoad = true
		saved, err = board.Save(WithPayrollPublication(ctx), []DriverBoardEntry{e})
		if err != nil || saved[0].Status != "HOME" {
			t.Fatal("clearing a load hid Home", saved, err)
		}
		// Stale concurrent edits must still fail without replacing the newer row.
		if _, err = board.Save(WithPayrollPublication(ctx), []DriverBoardEntry{e}); err != ErrDriverBoardConflict {
			t.Fatal("stale edit was not rejected", err)
		}
		// Truck restrictions survive clearing the operational selection, and the
		// optimized page read agrees with the canonical profile calculation.
		week, _ := chargeWeek(ChargeCurrentWeek())
		view, err := board.Get(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		var truckEntry DriverBoardEntry
		for _, candidate := range view.Entries {
			if candidate.DriverID == projectionDriver {
				truckEntry = candidate
			}
		}
		truckEntry.Status = "SHOP"
		truckEntry.StatusReason = "Test shop restriction"
		truckEntry.StatusEdited = true
		truckSaved, err := board.Save(WithPayrollPublication(ctx), []DriverBoardEntry{truckEntry})
		if err != nil {
			t.Fatal(err)
		}
		if truckSaved[0].FleetStatus == nil || truckSaved[0].FleetStatus.TruckOperatingStatus != "shop" {
			t.Fatal("committed truck status missing", truckSaved)
		}
		truckEntry = truckSaved[0]
		truckEntry.Status = ""
		truckEntry.ResolveCurrentLoad = true
		truckSaved, err = board.Save(WithPayrollPublication(ctx), []DriverBoardEntry{truckEntry})
		if err != nil || truckSaved[0].Status != "SHOP" {
			t.Fatal("clearing hid Shop", truckSaved, err)
		}
		view, err = board.Get(ctx, week)
		if err != nil {
			t.Fatal(err)
		}
		for _, driver := range view.Drivers {
			if driver.TruckID != "" {
				var canonical string
				if err = pool.QueryRow(ctx, `SELECT fleet_truck_operating_status($1)`, driver.TruckID).Scan(&canonical); err != nil || canonical != driver.TruckOperatingStatus {
					t.Fatal("board/profile status mismatch", canonical, driver.TruckOperatingStatus, err)
				}
			}
		}
		// Removing the row guard must fail coverage, never weaken the marker.
		if _, err = admin.Exec(ctx, `ALTER TABLE fleet_status_history DISABLE TRIGGER board_operational_history`); err != nil {
			t.Fatal(err)
		}
		if err = pay.EnablePayrollShadow(ctx); err == nil {
			t.Fatal("missing operational row guard accepted")
		}
		if _, err = admin.Exec(ctx, `ALTER TABLE fleet_status_history ENABLE TRIGGER board_operational_history; DROP TRIGGER reject_board_payroll ON payroll_projection_generations; DROP FUNCTION reject_board_payroll()`); err != nil {
			t.Fatal(err)
		}
		if _, err = pay.DispatchPayrollChanges(ctx); err != nil {
			t.Fatal("background status processing failed", err)
		}
	})
}

func TestStatusBoardLoadReturnToServiceDatabase(t *testing.T) {
	withPayrollProjectionDatabase(t, func(t *testing.T, ctx context.Context, pool *pgxpool.Pool, _ *pgx.Conn) {
		seedPayrollProjectionTest(t, ctx, pool)
		if _, err := pool.Exec(ctx, `UPDATE fleet_status_control SET enabled=true`); err != nil {
			t.Fatal(err)
		}
		board := NewDriverBoardRepository(pool)
		save := func(e DriverBoardEntry) DriverBoardEntry {
			t.Helper()
			rows, err := board.Save(ctx, []DriverBoardEntry{e}, "dispatch-test")
			if err != nil {
				t.Fatal(err)
			}
			return rows[0]
		}
		setStatus := func(e DriverBoardEntry, status string) DriverBoardEntry {
			e.Status, e.StatusEdited, e.StatusReason = status, true, "Test status change"
			return save(e)
		}
		assertStatus := func(e DriverBoardEntry, status, availability, truck string) {
			t.Helper()
			if e.Status != status || e.FleetStatus == nil || e.FleetStatus.AvailabilityStatus != availability || e.FleetStatus.TruckOperatingStatus != truck {
				t.Fatalf("want board %s, driver %s, truck %s, got %s %+v", status, availability, truck, e.Status, e.FleetStatus)
			}
		}
		action := func(e DriverBoardEntry, action, number, plan string) BoardLoadResult {
			t.Helper()
			view, err := board.Loads(ctx, e.DriverID, "")
			if err != nil {
				t.Fatal(err)
			}
			result, err := board.ChangeLoads(ctx, BoardLoadAction{DriverID: e.DriverID, Version: e.Version, Revision: view.Revision, Action: action, Number: number, PlanID: plan}, "dispatch-test")
			if err != nil {
				t.Fatal(err)
			}
			return result
		}

		entry := setStatus(DriverBoardEntry{DriverID: projectionDriver}, "SHOP")
		home := setStatus(entry, "HOME")
		assertStatus(home, "HOME", "home", "shop")
		entry = home
		entry.CurrentLoad = "NEW-CURRENT"
		entry = save(entry)
		assertStatus(entry, "DISPATCHED", "active", "in_use")
		if _, err := board.Save(ctx, []DriverBoardEntry{home}); !errors.Is(err, ErrDriverBoardConflict) {
			t.Fatal("stale save after return to service", err)
		}
		undone, err := board.Undo(ctx, entry.UndoID, entry.DriverID, entry.Version, "dispatch-test", true)
		if err != nil || undone.CurrentLoad != "" {
			t.Fatalf("undo load assignment: %+v %v", undone, err)
		}
		assertStatus(undone, "", "active", "in_use")

		entry = setStatus(undone, "REST")
		added := action(entry, "add", "QUEUED-RETURN", "")
		assertStatus(added.Entry, "READY", "active", "in_use")
		if added.Entry.CurrentLoad != "" || len(added.Loads.Next) != 1 {
			t.Fatal("queue addition changed current load or did not save the next load")
		}
		entry = setStatus(added.Entry, "SHOP")
		selected := action(entry, "select", "", added.Loads.Next[0].PlanID)
		assertStatus(selected.Entry, "DISPATCHED", "active", "in_use")
		if selected.Entry.CurrentLoad != "QUEUED-RETURN" {
			t.Fatal("drawer did not assign the selected load")
		}

		var truckVersion int
		if err := pool.QueryRow(ctx, `SELECT status_version FROM trucks WHERE id=$1`, projectionTruck).Scan(&truckVersion); err != nil {
			t.Fatal(err)
		}
		if _, err := NewFleetRepository(pool).ChangeTruckCondition(ctx, projectionTruck, "dispatch-test", FleetStatusChange{Status: "out_of_service", Reason: "Test OOS", Version: truckVersion}); err != nil {
			t.Fatal(err)
		}
		view, err := board.Get(ctx, time.Now())
		if err != nil || len(view.Entries) != 1 {
			t.Fatalf("read restricted board: %v", err)
		}
		home = setStatus(view.Entries[0], "HOME")
		entry = home
		entry.CurrentLoad = "BLOCKED"
		if _, err := board.Save(ctx, []DriverBoardEntry{entry}); !errors.Is(err, ErrTruckUnavailable) {
			t.Fatal("assignment should respect OOS", err)
		}
		view, err = board.Get(ctx, time.Now())
		if err != nil || view.Drivers[0].AvailabilityStatus != "home" || view.Entries[0].Version != home.Version || view.Entries[0].CurrentLoad != home.CurrentLoad {
			t.Fatal("failed assignment did not roll back", err)
		}
	})
}
