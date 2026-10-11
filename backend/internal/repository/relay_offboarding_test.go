package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/relay"
	"testing"
)

func testRelayOffboarding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	fake := &relayRosterFake{drivers: map[string]relay.Driver{}, assignments: map[string]*relay.PolicyAssignment{}}
	repo := NewFuelRepository(pool)
	repo.ConfigureRelayDrivers(fake, "staging")
	locals := map[string]string{}
	for _, name := range []string{"recent", "overdue", "no_policy", "rehired", "uncertain"} {
		locals[name] = id(`INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES($1,$1,'cpm',0.7) RETURNING id`, "Offboard "+name)
		fake.drivers[name] = relay.Driver{ID: name, FirstName: name, LastName: "Offboard"}
		if name != "no_policy" {
			fake.assignments[name] = &relay.PolicyAssignment{ID: "pa_" + name, PolicyID: "plc_company", Enabled: true}
		}
	}
	fake.drivers["unlinked"] = relay.Driver{ID: "unlinked", FirstName: "Unlinked", LastName: "Offboard"}
	fake.assignments["unlinked"] = &relay.PolicyAssignment{ID: "pa_unlinked", Enabled: true}
	if err := repo.RefreshRelayDrivers(ctx); err != nil {
		t.Fatal(err)
	}
	for name, local := range locals {
		exec(`UPDATE relay_driver_links SET driver_id=$1 WHERE relay_environment='staging' AND relay_driver_id=$2`, local, name)
	}
	for _, name := range []string{"recent", "overdue", "no_policy"} {
		days := 10
		if name == "recent" {
			days = 9
		}
		exec(`UPDATE drivers SET status='terminated',active=false,termination_date=(now() AT TIME ZONE 'America/New_York')::date-$2::int WHERE id=$1`, locals[name], days)
	}
	if err := repo.ProcessRelayOffboarding(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 4 || fake.assignments["recent"].Enabled || fake.drivers["overdue"].ID != "" || fake.drivers["no_policy"].ID != "" || !fake.assignments["unlinked"].Enabled || !fake.assignments["rehired"].Enabled {
		t.Fatalf("wrong offboarding actions: %d %+v", fake.calls, fake.drivers)
	}
	if got := id(`SELECT count(*)::text FROM relay_driver_operations WHERE environment='staging' AND source='termination' AND actor_id IS NULL AND termination_date IS NOT NULL AND state='confirmed'`); got != "4" {
		t.Fatal("missing system audit receipts", got)
	}
	if err := repo.ProcessRelayOffboarding(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 4 {
		t.Fatal("repeated completed actions")
	}
	exec(`UPDATE drivers SET status='active',active=true WHERE id=$1`, locals["recent"])
	fake.assignments["recent"].Enabled = true
	if err := repo.ProcessRelayOffboarding(ctx); err != nil {
		t.Fatal(err)
	}
	if !fake.assignments["recent"].Enabled {
		t.Fatal("rehire disabled")
	}
	exec(`UPDATE drivers SET status='terminated',active=false,termination_date=(now() AT TIME ZONE 'America/New_York')::date-20 WHERE id=$1`, locals["uncertain"])
	fake.uncertain = true
	if err := repo.ProcessRelayOffboarding(ctx); err == nil {
		t.Fatal("missing uncertain failure")
	}
	calls := fake.calls
	if err := repo.ProcessRelayOffboarding(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.calls != calls {
		t.Fatal("uncertain automation replayed")
	}
	fake.uncertain = false
	// Bulk actions independently validate each revision and retain durable receipts.
	request := id(`SELECT gen_random_uuid()::text`)
	changes := []RelayDriverChange{{RequestID: request, Action: "disable", RelayDriverID: "rehired", Revision: relayRosterEntry(fake.drivers["rehired"], fake.assignments["rehired"]).Revision}, {RequestID: id(`SELECT gen_random_uuid()::text`), Action: "disable", RelayDriverID: "recent", Revision: "stale"}}
	result, err := repo.ChangeRelayDrivers(ctx, changes, actor)
	if err != nil || len(result.Results) != 2 || result.Results[0].Error != "" || result.Results[1].Error == "" || fake.assignments["rehired"].Enabled || !fake.assignments["recent"].Enabled {
		t.Fatalf("bulk: %+v %v", result, err)
	}
	calls = fake.calls
	if _, err = repo.ChangeRelayDrivers(ctx, changes, actor); err != nil {
		t.Fatal(err)
	}
	if fake.calls != calls {
		t.Fatal("bulk retry repeated success")
	}
}
