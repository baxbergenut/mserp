package repository

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/relay"
)

func TestRelayStatusEnabled(t *testing.T) {
	for _, status := range []string{"READY", "REST", "COVERED", "DISPATCHED", "ENROUTE", "RESERVED", "DEADHEAD", "IN THE YARD", "HOLD FOR LOAD", "ISSUE WITH PU/DEL", "At PU", "At DEL", "34 RESET"} {
		if !relayStatusEnabled(status) || !relayStatusEnabled(" "+strings.ToLower(status)+" ") {
			t.Errorf("enabled status rejected: %q", status)
		}
	}
	for _, status := range []string{"", "HOME", "HOME (YARD)", "VACATION", "Family issue", "Health issue", "NEW DRIVER", "SHOP", "STOP", "ACCIDENT", "HOLD", "Left", "NO LOAD", "unknown", "READY tomorrow", "RESET"} {
		if relayStatusEnabled(status) {
			t.Errorf("blocked status enabled: %q", status)
		}
	}
}

// Count reads as well as mutations: default-off must not contact the provider.
type relayStatusReadFake struct {
	*relayRosterFake
	reads            int
	rejectEnable     bool
	beforeAssignment func() error
	beforeEnable     func() error
}

func (f *relayStatusReadFake) Drivers(ctx context.Context) ([]relay.Driver, error) {
	f.reads++
	return f.relayRosterFake.Drivers(ctx)
}
func (f *relayStatusReadFake) Driver(ctx context.Context, id string) (relay.Driver, error) {
	f.reads++
	return f.relayRosterFake.Driver(ctx, id)
}
func (f *relayStatusReadFake) Policies(ctx context.Context) ([]relay.DriverPolicy, error) {
	f.reads++
	return f.relayRosterFake.Policies(ctx)
}
func (f *relayStatusReadFake) Assignment(ctx context.Context, id string) (*relay.PolicyAssignment, error) {
	f.reads++
	if f.beforeAssignment != nil {
		hook := f.beforeAssignment
		f.beforeAssignment = nil
		if err := hook(); err != nil {
			return nil, err
		}
	}
	return f.relayRosterFake.Assignment(ctx, id)
}

func (f *relayStatusReadFake) EnablePolicy(ctx context.Context, id string, on bool) error {
	if f.beforeEnable != nil {
		if err := f.beforeEnable(); err != nil {
			return err
		}
	}
	if f.rejectEnable && on {
		f.calls++
		return &relay.APIError{Status: 503}
	}
	return f.relayRosterFake.EnablePolicy(ctx, id, on)
}
func testRelayStatusPolicy(t *testing.T, parent context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) string {
		t.Helper()
		var out string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	exec(`UPDATE fleet_status_control SET enabled=true`)
	setup := func(names ...string) (*FuelRepository, *relayStatusReadFake, map[string]string) {
		t.Helper()
		fake := &relayStatusReadFake{relayRosterFake: &relayRosterFake{drivers: map[string]relay.Driver{}, assignments: map[string]*relay.PolicyAssignment{}}}
		locals := map[string]string{}
		for _, name := range names {
			locals[name] = id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.7) RETURNING id`, "Status Policy "+name)
			fake.drivers[name] = relay.Driver{ID: name, FirstName: name, LastName: "Status"}
			fake.assignments[name] = &relay.PolicyAssignment{ID: "pa_" + name, PolicyID: "plc_company", Enabled: true, Limits: []relay.PolicyLimit{{Usage: "15", Remaining: "85"}}}
		}
		repo := NewFuelRepository(pool)
		repo.ConfigureRelayDrivers(fake, "staging")
		if err := repo.RefreshRelayDrivers(ctx); err != nil {
			t.Fatal(err)
		}
		for name, local := range locals {
			exec(`UPDATE relay_driver_links SET driver_id=$1 WHERE relay_environment='staging' AND relay_driver_id=$2`, local, name)
		}
		return repo, fake, locals
	}
	board := func(local, status string) {
		t.Helper()
		exec(`SELECT set_config('mserp.status_projection','1',false)`)
		exec(`INSERT INTO driver_board(driver_id,status) VALUES($1,$2) ON CONFLICT(driver_id) DO UPDATE SET status=excluded.status,version=driver_board.version+1`, local, status)
		exec(`SELECT set_config('mserp.status_projection','',false)`)
	}
	run := func(repo *FuelRepository) {
		t.Helper()
		if err := repo.ProcessRelayStatusPolicy(ctx); err != nil {
			t.Fatal(err)
		}
	}
	preview := func(repo *FuelRepository, name string) RelayStatusImpact {
		t.Helper()
		rows, err := repo.PreviewRelayStatusPolicy(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.RelayDriverID == name {
				return row
			}
		}
		t.Fatalf("missing preview for %s", name)
		return RelayStatusImpact{}
	}

	t.Run("default off and read only preview", func(t *testing.T) {
		repo, fake, locals := setup("status_off")
		board(locals["status_off"], "HOME")
		reads, calls := fake.reads, fake.calls
		run(repo)
		if got := preview(repo, "status_off"); got.Action != "disable" {
			t.Fatalf("preview: %+v", got)
		}
		if fake.reads != reads || fake.calls != calls {
			t.Fatal("disabled processing or preview called provider")
		}
		if got := id(`SELECT count(*)::text FROM relay_driver_operations WHERE relay_driver_id='status_off'`); got != "0" {
			t.Fatal("preview wrote operation", got)
		}
	})
	t.Run("status transitions preserve assignment and usage", func(t *testing.T) {
		repo, fake, locals := setup("status_transition")
		repo.ConfigureRelayStatusPolicy(true)
		before := *fake.assignments["status_transition"]
		board(locals["status_transition"], "HOME")
		run(repo)
		if fake.calls != 1 || fake.assignments["status_transition"].Enabled {
			t.Fatal("status did not disable")
		}
		if got := id(`SELECT count(*)::text FROM relay_driver_operations WHERE relay_driver_id='status_transition' AND source='fleet_status' AND state='confirmed' AND actor_id IS NULL`); got != "1" {
			t.Fatal("missing system receipt", got)
		}
		run(repo)
		if fake.calls != 1 {
			t.Fatal("disable replayed")
		}
		// Provider usage can advance while disabled and must be preserved.
		fake.assignments["status_transition"].Limits[0].Usage = "20"
		fake.assignments["status_transition"].Limits[0].Remaining = "80"
		before.Limits = []relay.PolicyLimit{{Usage: "20", Remaining: "80"}}
		board(locals["status_transition"], "READY")
		run(repo)
		if fake.calls != 2 || !fake.assignments["status_transition"].Enabled {
			t.Fatal("READY did not restore disabled assignment")
		}
		run(repo)
		if fake.calls != 2 {
			t.Fatal("enable replayed")
		}
		if !reflect.DeepEqual(before, *fake.assignments["status_transition"]) {
			t.Fatal("enable/disable changed assignment or usage")
		}
	})
	t.Run("preexisting disabled assignment follows current ON status", func(t *testing.T) {
		repo, fake, locals := setup("status_manual")
		fake.assignments["status_manual"].Enabled = false
		board(locals["status_manual"], "READY")
		repo.ConfigureRelayStatusPolicy(true)
		run(repo)
		if fake.calls != 1 || !fake.assignments["status_manual"].Enabled {
			t.Fatal("preexisting disabled assignment was not enabled")
		}
		if err := repo.RefreshRelayDrivers(ctx); err != nil {
			t.Fatal(err)
		}
		if got := preview(repo, "status_manual"); got.Action != "no_change" {
			t.Fatalf("preview: %+v", got)
		}
	})
	t.Run("uncertain hold never replays", func(t *testing.T) {
		repo, fake, locals := setup("status_uncertain")
		board(locals["status_uncertain"], "HOME")
		repo.ConfigureRelayStatusPolicy(true)
		fake.uncertain = true
		if err := repo.ProcessRelayStatusPolicy(ctx); err == nil {
			t.Fatal("uncertain mutation reported success")
		}
		fake.uncertain = false
		calls := fake.calls
		run(repo)
		if fake.calls != calls {
			t.Fatal("uncertain mutation replayed")
		}
		if got := preview(repo, "status_uncertain"); got.Action != "uncertain_hold" {
			t.Fatalf("preview: %+v", got)
		}
		if got := id(`SELECT count(*)::text FROM relay_driver_operations WHERE relay_driver_id='status_uncertain' AND state='pending'`); got != "1" {
			t.Fatal("missing durable uncertain receipt", got)
		}
	})
	t.Run("team follows main despite personal home", func(t *testing.T) {
		repo, fake, locals := setup("status_main", "status_team")
		board(locals["status_team"], "HOME")
		exec(`INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) VALUES($1,$2,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date)`, locals["status_main"], locals["status_team"])
		exec(`UPDATE drivers SET availability_status='home' WHERE id=$1`, locals["status_team"])
		board(locals["status_main"], "DISPATCHED")
		if got := preview(repo, "status_team"); got.StatusDriverID != locals["status_main"] || got.Status != "DISPATCHED" || !got.DesiredEnabled {
			t.Fatalf("main routing: %+v", got)
		}
		repo.ConfigureRelayStatusPolicy(true)
		run(repo)
		if !fake.assignments["status_main"].Enabled || !fake.assignments["status_team"].Enabled || fake.calls != 0 {
			t.Fatal("personal home overrode main dispatched status")
		}
		board(locals["status_main"], "HOME")
		run(repo)
		if fake.assignments["status_main"].Enabled || fake.assignments["status_team"].Enabled {
			t.Fatal("main home did not disable both policies")
		}
		board(locals["status_main"], "DISPATCHED")
		run(repo)
		if !fake.assignments["status_main"].Enabled || !fake.assignments["status_team"].Enabled {
			t.Fatal("main dispatched did not restore both policies")
		}
	})
	t.Run("home remains employed and failed enable retries after backoff", func(t *testing.T) {
		repo, fake, locals := setup("status_retry")
		repo.ConfigureRelayStatusPolicy(true)
		exec(`UPDATE drivers SET status='home',availability_status='home' WHERE id=$1`, locals["status_retry"])
		run(repo)
		if fake.assignments["status_retry"].Enabled || fake.calls != 1 {
			t.Fatal("employed home driver not disabled")
		}
		exec(`UPDATE drivers SET status='active',availability_status='active' WHERE id=$1`, locals["status_retry"])
		board(locals["status_retry"], "READY")
		fake.rejectEnable = true
		if err := repo.ProcessRelayStatusPolicy(ctx); err == nil {
			t.Fatal("rejected enable reported success")
		}
		calls := fake.calls
		fake.rejectEnable = false
		run(repo)
		if fake.calls != calls {
			t.Fatal("failed enable ignored backoff")
		}
		exec(`UPDATE relay_driver_operations SET created_at=now()-interval '20 minutes' WHERE relay_driver_id='status_retry' AND source='fleet_status' AND state='confirmed'`)
		exec(`UPDATE relay_driver_operations SET created_at=now()-interval '16 minutes' WHERE relay_driver_id='status_retry' AND source='fleet_status' AND state='failed'`)
		run(repo)
		if !fake.assignments["status_retry"].Enabled || fake.calls != calls+1 {
			t.Fatal("known failed enable did not retry after backoff")
		}
	})
	t.Run("reconciliation disabled and missing policy are inert", func(t *testing.T) {
		repo, fake, locals := setup("status_guard")
		repo.ConfigureRelayStatusPolicy(true)
		board(locals["status_guard"], "HOME")
		exec(`UPDATE fleet_status_control SET enabled=false`)
		reads := fake.reads
		run(repo)
		if fake.calls != 0 || fake.reads != reads {
			t.Fatal("disabled fleet reconciliation contacted provider")
		}
		exec(`UPDATE fleet_status_control SET enabled=true`)
		delete(fake.assignments, "status_guard")
		if err := repo.RefreshRelayDrivers(ctx); err != nil {
			t.Fatal(err)
		}
		run(repo)
		if fake.calls != 0 {
			t.Fatal("missing policy caused mutation")
		}
		if got := preview(repo, "status_guard"); got.Action != "no_assignment" {
			t.Fatalf("missing assignment: %+v", got)
		}
	})
	t.Run("replacement assignment follows current ON status", func(t *testing.T) {
		repo, fake, locals := setup("status_replaced")
		repo.ConfigureRelayStatusPolicy(true)
		board(locals["status_replaced"], "HOME")
		run(repo)
		fake.assignments["status_replaced"].ID = "pa_replacement_same_policy"
		board(locals["status_replaced"], "READY")
		calls := fake.calls
		run(repo)
		if fake.calls != calls+1 || !fake.assignments["status_replaced"].Enabled {
			t.Fatal("replacement assignment did not follow READY status")
		}
	})
	t.Run("manual same state disable yields to current ON status", func(t *testing.T) {
		repo, fake, locals := setup("status_manual_again")
		repo.ConfigureRelayStatusPolicy(true)
		board(locals["status_manual_again"], "HOME")
		run(repo)
		if err := repo.RefreshRelayDrivers(ctx); err != nil {
			t.Fatal(err)
		}
		change := RelayDriverChange{RequestID: id(`SELECT gen_random_uuid()::text`), Action: "disable", RelayDriverID: "status_manual_again", Revision: relayRosterEntry(fake.drivers["status_manual_again"], fake.assignments["status_manual_again"]).Revision}
		if err := repo.ChangeRelayDriver(ctx, change, actor); err != nil && !errors.Is(err, ErrRelayDriverConflict) {
			t.Fatal(err)
		}
		board(locals["status_manual_again"], "READY")
		calls := fake.calls
		run(repo)
		if fake.calls != calls+1 || !fake.assignments["status_manual_again"].Enabled {
			t.Fatal("manual same-state disable was not reconciled to READY")
		}
		// A successful manual OFF of an enabled assignment is also reconciled.
		change.RequestID = id(`SELECT gen_random_uuid()::text`)
		change.Revision = relayRosterEntry(fake.drivers["status_manual_again"], fake.assignments["status_manual_again"]).Revision
		if err := repo.ChangeRelayDriver(ctx, change, actor); err != nil {
			t.Fatal(err)
		}
		if fake.assignments["status_manual_again"].Enabled {
			t.Fatal("manual disable did not execute")
		}
		calls = fake.calls
		run(repo)
		if fake.calls != calls+1 || !fake.assignments["status_manual_again"].Enabled {
			t.Fatal("manual OFF overrode current READY status")
		}
	})
	t.Run("stale status and relink candidate send nothing", func(t *testing.T) {
		repo, fake, locals := setup("status_stale")
		repo.ConfigureRelayStatusPolicy(true)
		board(locals["status_stale"], "HOME")
		candidate := preview(repo, "status_stale")
		board(locals["status_stale"], "READY")
		apply := func() {
			t.Helper()
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			if err = repo.applyRelayStatusPolicy(ctx, conn, candidate); err != nil {
				t.Fatal(err)
			}
		}
		apply()
		if fake.calls != 0 {
			t.Fatal("stale status sent mutation")
		}
		board(locals["status_stale"], "HOME")
		candidate = preview(repo, "status_stale")
		replacement := id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Status Relink','status relink','cpm',0.7) RETURNING id`)
		exec(`UPDATE relay_driver_links SET driver_id=$1 WHERE relay_environment='staging' AND relay_driver_id='status_stale'`, replacement)
		apply()
		if fake.calls != 0 {
			t.Fatal("stale link sent mutation")
		}
		exec(`UPDATE relay_driver_links SET driver_id=$1 WHERE relay_environment='staging' AND relay_driver_id='status_stale'`, locals["status_stale"])
		run(repo)
		exec(`UPDATE relay_driver_links SET driver_id=$1 WHERE relay_environment='staging' AND relay_driver_id='status_stale'`, replacement)
		board(replacement, "READY")
		calls := fake.calls
		run(repo)
		if fake.calls != calls+1 || !fake.assignments["status_stale"].Enabled {
			t.Fatal("relinked assignment did not follow current driver READY status")
		}
	})
	for _, change := range []string{"status", "link"} {
		t.Run("concurrent "+change+" change during initial provider read aborts receipt", func(t *testing.T) {
			name := "status_race_" + change
			repo, fake, locals := setup(name)
			repo.ConfigureRelayStatusPolicy(true)
			exec(`UPDATE drivers SET availability_status='home' WHERE id=$1`, locals[name])
			other, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close(ctx)
			replacement := ""
			if change == "link" {
				replacement = id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Race Relink','race relink','cpm',0.7) RETURNING id`)
			}
			fake.beforeAssignment = func() error {
				if change == "status" {
					_, err := other.Exec(ctx, `UPDATE drivers SET availability_status='rest' WHERE id=$1`, locals[name])
					return err
				}
				_, err := other.Exec(ctx, `UPDATE relay_driver_links SET driver_id=$1 WHERE relay_environment='staging' AND relay_driver_id=$2`, replacement, name)
				return err
			}
			run(repo)
			if fake.calls != 0 {
				t.Fatal("concurrent change still sent provider mutation")
			}
			if got := id(`SELECT count(*)::text FROM relay_driver_operations WHERE relay_driver_id=$1 AND state='failed' AND status_context->>'aborted'='true'`, name); got != "1" {
				t.Fatal("missing final-fence aborted receipt", got)
			}
			if got := id(`SELECT count(*)::text FROM relay_driver_operations WHERE relay_driver_id=$1 AND state='pending'`, name); got != "0" {
				t.Fatal("unsent race left pending hold")
			}
			if got := preview(repo, name); got.Action == "uncertain_hold" || got.Action == "failure_backoff" {
				t.Fatalf("unsent race blocked future work: %+v", got)
			}
		})
	}
	t.Run("native status writer waits for final provider mutation", func(t *testing.T) {
		repo, fake, locals := setup("status_locked_put")
		repo.ConfigureRelayStatusPolicy(true)
		exec(`UPDATE drivers SET availability_status='home' WHERE id=$1`, locals["status_locked_put"])
		other, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
		if err != nil {
			t.Fatal(err)
		}
		defer other.Close(ctx)
		entered, release := make(chan struct{}), make(chan struct{})
		fake.beforeEnable = func() error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		workDone := make(chan error, 1)
		go func() { workDone <- repo.ProcessRelayStatusPolicy(ctx) }()
		select {
		case <-entered:
		case err := <-workDone:
			t.Fatalf("worker ended before PUT: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		updateStarted, updateDone := make(chan struct{}), make(chan error, 1)
		go func() {
			close(updateStarted)
			_, err := other.Exec(ctx, `UPDATE drivers SET availability_status='rest' WHERE id=$1`, locals["status_locked_put"])
			updateDone <- err
		}()
		<-updateStarted
		var early bool
		select {
		case err := <-updateDone:
			early = true
			if err != nil {
				t.Error(err)
			}
		case <-time.After(150 * time.Millisecond):
		}
		close(release)
		if err := <-workDone; err != nil {
			t.Fatal(err)
		}
		if !early {
			if err := <-updateDone; err != nil {
				t.Fatal(err)
			}
		} else {
			t.Fatal("native status writer bypassed final mutation's fleet lock")
		}
		if fake.calls != 1 || fake.assignments["status_locked_put"].Enabled {
			t.Fatal("blocked PUT did not finish exactly once")
		}
		if got := id(`SELECT count(*)::text FROM relay_driver_operations WHERE relay_driver_id='status_locked_put' AND state='confirmed'`); got != "1" {
			t.Fatal("final PUT lacked confirmed receipt")
		}
		fake.beforeEnable = nil
		run(repo)
		if fake.calls != 2 || !fake.assignments["status_locked_put"].Enabled {
			t.Fatal("next sweep failed to converge to waiting status update")
		}
	})
	t.Run("terminated and inactive stay untouched while rehire follows status", func(t *testing.T) {
		repo, fake, locals := setup("status_rehire", "status_inactive")
		repo.ConfigureRelayStatusPolicy(true)
		board(locals["status_rehire"], "HOME")
		board(locals["status_inactive"], "HOME")
		exec(`UPDATE drivers SET active=false WHERE id=$1`, locals["status_inactive"])
		run(repo)
		if fake.calls != 1 || fake.assignments["status_rehire"].Enabled || !fake.assignments["status_inactive"].Enabled {
			t.Fatal("active home or inactive policy handled incorrectly")
		}
		exec(`UPDATE drivers SET status='terminated',active=false WHERE id=$1`, locals["status_rehire"])
		board(locals["status_rehire"], "READY")
		reads, calls := fake.reads, fake.calls
		run(repo)
		if fake.calls != calls || fake.reads != reads {
			t.Fatal("status worker contacted provider for terminated or inactive driver")
		}
		exec(`UPDATE drivers SET status='active',active=true WHERE id=$1`, locals["status_rehire"])
		board(locals["status_rehire"], "READY")
		run(repo)
		if fake.calls != calls+1 || !fake.assignments["status_rehire"].Enabled {
			t.Fatal("rehired active driver did not follow READY status")
		}
		delete(fake.assignments, "status_rehire")
		delete(fake.drivers, "status_rehire")
		if err := repo.RefreshRelayDrivers(ctx); err != nil {
			t.Fatal(err)
		}
		calls = fake.calls
		run(repo)
		if fake.calls != calls || len(fake.drivers) != 1 {
			t.Fatal("rehire recreated a removed provider account")
		}
	})
}
