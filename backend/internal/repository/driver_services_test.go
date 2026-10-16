package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
	"mserp/internal/relay"
	"testing"
	"time"
)

// Runs against both fresh and migrated schemas through TestRelayDriverDatabase.
func testDriverServices(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	run := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(sql string, args ...any) string {
		t.Helper()
		var s string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	run(`INSERT INTO weighmytruck_imports(digest,row_count) VALUES('services-fixture',0)`)
	relayFake := &relayRosterFake{drivers: map[string]relay.Driver{}, assignments: map[string]*relay.PolicyAssignment{}}
	fuel := NewFuelRepository(pool)
	fuel.ConfigureRelayDrivers(relayFake, "production")
	wmtFake := &wmtFake{}
	wmt := NewWeighMyTruckRepository(pool, wmtFake)
	repo := NewDriverServiceRepository(pool, fuel, wmt)
	fleet := NewFleetRepository(pool)
	phone := "5559877654"
	input := DriverInput{FullName: "Service Enrollment", PayType: "cpm", PayRate: .65, Active: true, Status: "active", Phone: &phone, EscrowAmount: "0", ChargeActor: actor}
	intents := []DriverServiceIntent{{Service: "relay", Action: "add"}, {Service: "weighmytruck", Action: "add"}}
	// Bad setup intent rolls back the local creation; contact omissions do not.
	invalid := WithDriverServiceIntents(ctx, []DriverServiceIntent{{Service: "unknown", Action: "add"}}, actor)
	if _, err := fleet.CreateDriver(invalid, input); err == nil {
		t.Fatal("invalid intent accepted")
	}
	if scalar(`SELECT count(*)::text FROM drivers WHERE full_name='Service Enrollment'`) != "0" {
		t.Fatal("partial driver save")
	}
	driver, err := fleet.CreateDriver(WithDriverServiceIntents(ctx, intents, actor), input)
	if err != nil {
		t.Fatal(err)
	}
	if relayFake.calls != 0 || wmtFake.calls != 0 {
		t.Fatal("vendor called during driver save")
	}
	if scalar(`SELECT count(*)::text FROM driver_service_requests WHERE driver_id=$1`, driver.ID) != "2" {
		t.Fatal("requests missing")
	}
	if err = repo.Request(ctx, driver.ID, actor, DriverServiceIntent{Service: "relay", Action: "add", Version: 1}); err == nil {
		t.Fatal("queued replay accepted")
	}
	status := func(id, service string) DriverServiceState {
		t.Helper()
		view, e := repo.Status(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		for _, i := range view.Items {
			if i.Service == service {
				return i
			}
		}
		t.Fatal("missing state")
		return DriverServiceState{}
	}
	process := func() {
		t.Helper()
		if e := repo.Process(ctx); e != nil {
			t.Fatal(e)
		}
	}
	process()
	if status(driver.ID, "relay").State != "needs_setup" || status(driver.ID, "weighmytruck").State != "review" {
		t.Fatalf("unexpected states: %+v / %+v", status(driver.ID, "relay"), status(driver.ID, "weighmytruck"))
	}
	if relayFake.calls != 1 || wmtFake.calls != 0 {
		t.Fatal("missing-contact behavior")
	}
	process()
	if relayFake.calls != 1 {
		t.Fatal("worker re-enrolled")
	}
	member := status(driver.ID, "weighmytruck")
	run(`UPDATE drivers SET email='service@example.test' WHERE id=$1`, driver.ID)
	if err = repo.Request(ctx, driver.ID, actor, DriverServiceIntent{Service: "weighmytruck", Action: "add", Version: member.Version}); err != nil {
		t.Fatal(err)
	}
	process()
	if status(driver.ID, "weighmytruck").State != "added" || wmtFake.calls != 1 {
		t.Fatalf("WMT did not finish: %+v calls=%d", status(driver.ID, "weighmytruck"), wmtFake.calls)
	}
	entry := status(driver.ID, "relay").Relay
	if err = fuel.ChangeRelayDriver(ctx, RelayDriverChange{RequestID: scalar(`SELECT gen_random_uuid()::text`), Action: "assign", RelayDriverID: entry.Driver.ID, Revision: entry.Revision, PolicyID: "plc_company"}, actor); err != nil {
		t.Fatal(err)
	}
	process()
	if scalar(`SELECT count(*)::text FROM system_task_records WHERE service_driver_id=$1 AND completed_at IS NOT NULL`, driver.ID) != "2" {
		t.Fatal("tasks did not auto-complete")
	}
	// Future reminders remain durable without calling the provider or entering today's task count.
	input.FullName = "Deferred Service"
	phone = "5559877655"
	input.Phone = &phone
	tomorrow := time.Now().AddDate(0, 0, 2).Format(time.DateOnly)
	deferred, err := fleet.CreateDriver(WithDriverServiceIntents(ctx, []DriverServiceIntent{{Service: "weighmytruck", Action: "defer", FollowUpOn: tomorrow}}, actor), input)
	if err != nil {
		t.Fatal(err)
	}
	process()
	if wmtFake.calls != 1 {
		t.Fatal("reminder enrolled automatically")
	}
	board, err := NewCustomTaskRepository(pool).Board(ctx, Pagination{Page: 1, PageSize: 25}, "Deferred Service", "open", true, true)
	if err != nil || board.Total != 0 {
		t.Fatalf("future task visible: %+v %v", board, err)
	}
	s := status(deferred.ID, "weighmytruck")
	if err = repo.Request(ctx, deferred.ID, actor, DriverServiceIntent{Service: "weighmytruck", Action: "defer", FollowUpOn: ChargeCurrentWeek(), Version: s.Version}); err != nil {
		t.Fatal(err)
	}
	board, err = NewCustomTaskRepository(pool).Board(ctx, Pagination{Page: 1, PageSize: 25}, "Deferred Service", "open", true, true)
	if err != nil || board.Total != 1 || board.Items[0].ServiceDriverID != deferred.ID {
		t.Fatalf("due task absent: %+v %v", board, err)
	}
	if scalar(`SELECT count(*)::text FROM system_task_records WHERE service_driver_id=$1`, deferred.ID) != "1" {
		t.Fatal("duplicate reminder")
	}
	process()
	if scalar(`SELECT (reminder_notified_on=follow_up_on)::text FROM driver_service_requests WHERE driver_id=$1`, deferred.ID) != "true" {
		t.Fatal("due reminder not notified")
	}
	// An uncertain response cannot replay, even after a reminder is set.
	run(`UPDATE drivers SET email='deferred@example.test' WHERE id=$1`, deferred.ID)
	s = status(deferred.ID, "weighmytruck")
	wmtFake.fail = true
	if err = repo.Request(ctx, deferred.ID, actor, DriverServiceIntent{Service: "weighmytruck", Action: "add", Version: s.Version}); err != nil {
		t.Fatal(err)
	}
	process()
	s = status(deferred.ID, "weighmytruck")
	if s.State != "unconfirmed" {
		t.Fatalf("uncertainty lost: %+v", s)
	}
	if err = repo.Request(ctx, deferred.ID, actor, DriverServiceIntent{Service: "weighmytruck", Action: "defer", FollowUpOn: tomorrow, Version: s.Version}); err != nil {
		t.Fatal(err)
	}
	s = status(deferred.ID, "weighmytruck")
	if err = repo.Request(ctx, deferred.ID, actor, DriverServiceIntent{Service: "weighmytruck", Action: "add", Version: s.Version}); err == nil {
		t.Fatal("uncertain replay accepted")
	}
	process()
	if wmtFake.calls != 2 {
		t.Fatal("uncertain provider replayed")
	}
	if err = wmt.Verify(ctx, s.Membership.ID, WMTVerification{Version: s.Membership.Version, Enrolled: true, Reason: "Verified test fixture company membership"}, actor); err != nil {
		t.Fatal(err)
	}
	process()
	if scalar(`SELECT state FROM driver_service_requests WHERE driver_id=$1`, deferred.ID) != "done" {
		t.Fatal("verified task not closed")
	}
	// Onboarding saves even without vendor prerequisites, and records both intents atomically.
	event := fleetscope.Event{Version: 1, Type: "driver.hired", EventID: "07900000-0000-0000-0000-000000000011", CompanyID: "07900000-0000-0000-0000-000000000012", OccurredAt: time.Now(), Driver: fleetscope.Driver{ID: "07900000-0000-0000-0000-000000000013", FullName: "Onboard Service", DriverType: "company", HireDate: ChargeCurrentWeek()}}
	hire, err := fleet.AcceptFleetScopeHire(ctx, event, "service-onboard")
	if err != nil {
		t.Fatal(err)
	}
	input.FullName = "Onboard Service"
	input.Phone = nil
	onboard, err := fleet.CompleteDriverIntake(WithDriverServiceIntents(ctx, intents, actor), hire.IntakeID, actor, "", &input, false)
	if err != nil {
		t.Fatal(err)
	}
	if scalar(`SELECT count(*)::text FROM driver_service_requests WHERE driver_id=$1`, onboard.ID) != "2" {
		t.Fatal("onboarding intents missing")
	}
	run(`UPDATE drivers SET status='terminated',active=false WHERE id=$1`, onboard.ID)
	process()
	if scalar(`SELECT count(*)::text FROM driver_service_requests WHERE driver_id=$1 AND state='canceled'`, onboard.ID) != "2" {
		t.Fatal("termination didn't cancel")
	}
}
