package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func TestDriverLeaveDates(t *testing.T) {
	for _, p := range []DriverLeavePlan{{HomeStart: "2026-02-30", HomeEnd: "2026-03-01"}, {HomeStart: "2026-10-23"}, {HomeStart: "2026-10-24", HomeEnd: "2026-10-23"}, {VacationReturn: "2026-13"}, {VacationReturn: "2026-02-30"}} {
		if p.Validate() == nil {
			t.Fatalf("accepted invalid plan %+v", p)
		}
	}
	for _, p := range []DriverLeavePlan{{}, {HomeStart: "2026-10-23", HomeEnd: "2026-10-23"}, {VacationReturn: "2026-12"}, {VacationReturn: "2026-12-31"}} {
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if got := leaveReturnDeadline(&DriverLeavePlan{VacationReturn: "2028-02"}, "vacation", "2028-03-01"); got != "2028-02-29" {
		t.Fatal(got)
	}
	if got := leaveReturnDeadline(&DriverLeavePlan{HomeStart: "2026-11-01", HomeEnd: "2026-11-04"}, "home", "2026-10-23"); got != "" {
		t.Fatal("future leave became overdue", got)
	}
	if got := leaveReturnDeadline(&DriverLeavePlan{HomeStart: "2026-10-01", HomeEnd: "2026-10-04"}, "active", "2026-10-23"); got != "" {
		t.Fatal(got)
	}
}

// Runs in both fresh and migrated schemas as the application role.
func testDriverLeaveWorkflow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	fleet := NewFleetRepository(pool)
	board := NewDriverBoardRepository(pool)
	tasks := NewCustomTaskRepository(pool)
	email := "leave-dispatcher@example.test"
	dispatcher, err := fleet.CreateDispatcher(ctx, DispatcherInput{FullName: "Leave Dispatcher", Email: &email, Active: true, AccountPasswordHash: "$2a$test", AccountActor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher.UserAccountID == nil {
		t.Fatal("dispatcher account missing")
	}
	input := DriverInput{FullName: "Leave Driver", PayType: "cpm", Active: true, DispatcherID: &dispatcher.ID}
	driver, err := fleet.CreateDriver(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	plan := &DriverLeavePlan{HomeStart: "2026-10-20", HomeEnd: "2026-10-23"}
	saved, err := board.Save(ctx, []DriverBoardEntry{{DriverID: driver.ID, Status: "HOME", StatusEdited: true, LeavePlan: plan}}, actor)
	if err != nil {
		t.Fatal(err)
	}
	current, err := fleet.GetDriver(ctx, driver.ID)
	if err != nil || current.LeavePlan.HomeEnd != "2026-10-23" {
		t.Fatal("profile lost dates", current, err)
	}
	at := func(value string) time.Time { d, _ := time.Parse(time.RFC3339, value); return d }
	count := func(viewer string) int {
		page, e := tasks.Board(WithTaskViewer(ctx, viewer, false), Pagination{Page: 1, PageSize: 100}, "Leave Driver", "open", true, true)
		if e != nil {
			t.Fatal(e)
		}
		return page.Total
	}
	if err = tasks.RefreshDriverLeaveTasks(ctx, at("2026-10-24T03:59:59Z")); err != nil {
		t.Fatal(err)
	}
	if count(*dispatcher.UserAccountID) != 0 {
		t.Fatal("reminder before New York midnight")
	}
	if err = tasks.RefreshDriverLeaveTasks(ctx, at("2026-10-24T04:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if err = tasks.RefreshDriverLeaveTasks(ctx, at("2026-10-24T04:00:01Z")); err != nil {
		t.Fatal(err)
	}
	if count(*dispatcher.UserAccountID) != 1 || count(actor) != 0 {
		t.Fatal("duplicate or private reminder leaked")
	}
	// A stale profile cannot replace a newer board plan.
	_, err = fleet.ChangeAvailability(ctx, driver.ID, actor, FleetStatusChange{Status: "home", Version: current.StatusVersion, LeavePlan: plan, LeavePlanVersion: 0})
	if !errors.Is(err, ErrDriverBoardConflict) {
		t.Fatal("stale profile accepted", err)
	}
	extended := &DriverLeavePlan{HomeStart: plan.HomeStart, HomeEnd: "2026-10-26"}
	current, err = fleet.ChangeAvailability(ctx, driver.ID, actor, FleetStatusChange{Status: "home", Version: current.StatusVersion, LeavePlan: extended, LeavePlanVersion: current.LeavePlanVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err = tasks.RefreshDriverLeaveTasks(ctx, at("2026-10-24T12:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if count(*dispatcher.UserAccountID) != 0 {
		t.Fatal("extended return did not clear task")
	}
	entry, err := boardEntryForTest(ctx, pool, driver.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry.LeavePlan = &DriverLeavePlan{HomeStart: plan.HomeStart, HomeEnd: "2026-10-28"}
	saved, err = board.Save(ctx, []DriverBoardEntry{entry}, actor)
	if err != nil {
		t.Fatal(err)
	}
	undone, err := board.Undo(ctx, saved[0].UndoID, driver.ID, saved[0].Version, actor, true)
	if err != nil || undone.LeavePlan.HomeEnd != "2026-10-26" {
		t.Fatal("date history undo", undone, err)
	}
	current, err = fleet.GetDriver(ctx, driver.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err = fleet.ChangeAvailability(ctx, driver.ID, actor, FleetStatusChange{Status: "vacation", Version: current.StatusVersion, LeavePlan: &DriverLeavePlan{VacationReturn: "2026-10"}, LeavePlanVersion: current.LeavePlanVersion})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		now  string
		want int
	}{{"2026-10-31T16:00:00Z", 0}, {"2026-11-01T04:00:00Z", 1}} {
		if err = tasks.RefreshDriverLeaveTasks(ctx, at(v.now)); err != nil {
			t.Fatal(err)
		}
		if got := count(*dispatcher.UserAccountID); got != v.want {
			t.Fatal("month precision", v, got)
		}
	}
	current, err = fleet.ChangeAvailability(ctx, driver.ID, actor, FleetStatusChange{Status: "vacation", Version: current.StatusVersion, LeavePlan: &DriverLeavePlan{}, LeavePlanVersion: current.LeavePlanVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err = tasks.RefreshDriverLeaveTasks(ctx, at("2026-11-02T12:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if count(*dispatcher.UserAccountID) != 0 {
		t.Fatal("unknown return reminded")
	}
	// Updating the account and dispatcher remains one identity.
	if _, err = pool.Exec(ctx, `UPDATE app_users SET username='Renamed Leave Dispatcher',email='renamed-leave@example.test' WHERE id=$1`, *dispatcher.UserAccountID); err != nil {
		t.Fatal(err)
	}
	dispatcher, err = fleet.GetDispatcher(ctx, dispatcher.ID)
	if err != nil || dispatcher.FullName != "Renamed Leave Dispatcher" {
		t.Fatal("identity sync", dispatcher, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM app_roles WHERE dispatcher_role`); err == nil {
		t.Fatal("permanent role deleted")
	}
	if _, err = pool.Exec(ctx, `UPDATE app_roles SET name='Other' WHERE dispatcher_role`); err == nil {
		t.Fatal("permanent role renamed")
	}
	if _, err = pool.Exec(ctx, `UPDATE app_users SET role_id=(SELECT id FROM app_roles WHERE system_role) WHERE id=$1`, *dispatcher.UserAccountID); err == nil {
		t.Fatal("dispatcher role replaced")
	}
	badEmail := "rollback-leave@example.test"
	missing := "00000000-0000-4000-8000-000000000999"
	_, err = fleet.CreateDispatcher(ctx, DispatcherInput{FullName: "Rollback Dispatcher", Email: &badEmail, Active: true, AccountPasswordHash: "$2a$test", AccountActor: actor, Updaters: &UpdaterAssignments{MainUpdaterID: &missing}})
	if err == nil {
		t.Fatal("invalid updater accepted")
	}
	var orphans int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM app_users WHERE email=$1`, badEmail).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatal("atomic account creation left an orphan", orphans, err)
	}
}
