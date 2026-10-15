package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/relay"
)

type relayRosterFake struct {
	drivers     map[string]relay.Driver
	assignments map[string]*relay.PolicyAssignment
	calls       int
	uncertain   bool
	readFailure bool
}

func (f *relayRosterFake) Drivers(context.Context) ([]relay.Driver, error) {
	result := []relay.Driver{}
	for _, d := range f.drivers {
		result = append(result, d)
	}
	return result, nil
}
func (f *relayRosterFake) Driver(_ context.Context, id string) (relay.Driver, error) {
	d, ok := f.drivers[id]
	if !ok {
		return d, &relay.APIError{Status: 404}
	}
	return d, nil
}
func (f *relayRosterFake) Policies(context.Context) ([]relay.DriverPolicy, error) {
	return []relay.DriverPolicy{{ID: "plc_company", Name: "MS Express Company - ELD", ELDEnabled: true}, {ID: "plc_owner", Name: "MS Express O/O - ELD", ELDEnabled: true}, {ID: "plc_non", Name: "Non ELD Policy"}, {ID: "plc_flinn", Name: "Flinn Corp Company - ELD"}}, nil
}
func (f *relayRosterFake) Assignment(_ context.Context, id string) (*relay.PolicyAssignment, error) {
	if f.readFailure {
		return nil, &relay.APIError{Status: 503}
	}
	return f.assignments[id], nil
}
func (f *relayRosterFake) CreateDriver(_ context.Context, in relay.DriverInput) (relay.Driver, error) {
	f.calls++
	if f.uncertain {
		return relay.Driver{}, &relay.APIError{Uncertain: true}
	}
	d := relay.Driver{ID: fmt.Sprintf("dr_new%d", f.calls), FirstName: in.FirstName, LastName: in.LastName, Phone: in.Phone, Email: in.Email, DataFields: in.DataFields}
	f.drivers[d.ID] = d
	return d, nil
}
func (f *relayRosterFake) UpdateDriver(_ context.Context, id string, in relay.DriverInput) error {
	f.calls++
	d := f.drivers[id]
	d.FirstName = in.FirstName
	d.LastName = in.LastName
	d.Email = in.Email
	d.DataFields = in.DataFields
	d.UpdatedAt = fmt.Sprint(f.calls)
	f.drivers[id] = d
	return nil
}
func (f *relayRosterFake) RemoveDriver(_ context.Context, id string) error {
	f.calls++
	if f.uncertain {
		return &relay.APIError{Uncertain: true}
	}
	delete(f.drivers, id)
	delete(f.assignments, id)
	return nil
}
func (f *relayRosterFake) AssignPolicy(_ context.Context, id, policy string) error {
	f.calls++
	f.assignments[id] = &relay.PolicyAssignment{ID: "pa_new", PolicyID: policy, PolicyName: policy, Enabled: true}
	return nil
}
func (f *relayRosterFake) EnablePolicy(_ context.Context, id string, on bool) error {
	f.calls++
	if f.uncertain {
		return &relay.APIError{Uncertain: true}
	}
	f.assignments[id].Enabled = on
	return nil
}
func (f *relayRosterFake) UnassignPolicy(_ context.Context, id string) error {
	f.calls++
	delete(f.assignments, id)
	return nil
}

func TestRelayDriverDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_RELAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_RELAY_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local disposable test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("relay_roster_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			source, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			if mode == "migration" {
				before, _, ok := strings.Cut(string(source), "-- Relay driver management (migration 075).")
				if !ok {
					t.Fatal("missing migration boundary")
				}
				exec(before)
				for _, file := range []string{"075_relay_drivers.sql", "076_relay_offboarding.sql", "077_prepass_truck_links.sql", "078_driver_teams.sql", "079_driver_service_setup.sql", "080_driver_address_consolidation.sql", "081_truck_recurring_charges.sql", "082_prepass_fleet.sql", "083_prepass_fleet_toll_links.sql"} {
					migration, e := os.ReadFile("../../sql/" + file)
					if e != nil {
						t.Fatal(e)
					}
					exec(string(migration))
				}
				exec(`ALTER TABLE system_task_assignments ADD COLUMN assignee_role_ids uuid[] NOT NULL DEFAULT '{}'; ALTER TABLE system_task_records ADD COLUMN maintenance_truck_id uuid; ALTER TABLE system_task_records ADD COLUMN maintenance_service text NOT NULL DEFAULT ''`)
			} else {
				exec(string(source))
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + quoted + ` TO mserp_app`)
			pc := cfg.Copy()
			pc.MaxConns = 1
			pc.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			pc.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, pc)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			id := func(sql string, args ...any) string {
				t.Helper()
				var s string
				if e := pool.QueryRow(ctx, sql, args...).Scan(&s); e != nil {
					t.Fatal(e)
				}
				return s
			}
			actor := id(`INSERT INTO app_users(username,password_hash) VALUES('relay-test-user','$2test') RETURNING id`)
			driverID := id(`INSERT INTO drivers(full_name,normalized_name,phone,pay_type,pay_rate) VALUES('Relay Driver','relay driver','5551234567','cpm',0.75) RETURNING id`)
			second := id(`INSERT INTO drivers(full_name,normalized_name,phone,pay_type,pay_rate) VALUES('New Driver','new driver','5557654321','cpm',0.75) RETURNING id`)
			fake := &relayRosterFake{drivers: map[string]relay.Driver{"dr_one": {ID: "dr_one", FirstName: "Relay", LastName: "Driver", Phone: "+15551234567", IntegrationID: "1234567890123456", DataFields: []relay.DriverDataField{{Name: "Driver Pin", Value: "secret123"}, {Name: "Truck #", Value: "101"}}}}, assignments: map[string]*relay.PolicyAssignment{"dr_one": {ID: "pa_one", PolicyID: "plc_company", PolicyName: "MS Express Company - ELD", Enabled: true, Limits: []relay.PolicyLimit{{Usage: "15", Remaining: "85"}}}}}
			repo := NewFuelRepository(pool)
			repo.ConfigureRelayDrivers(fake, "production")
			if e = repo.RefreshRelayDrivers(ctx); e != nil {
				t.Fatal(e)
			}
			list, e := repo.RelayDrivers(ctx)
			if e != nil || len(list.Items) != 1 || len(list.Policies) != 3 || list.Items[0].DriverID != "" {
				t.Fatalf("roster: %+v %v", list, e)
			}
			encoded, _ := json.Marshal(list)
			if strings.Contains(string(encoded), "secret123") {
				t.Fatal("PIN leaked")
			}
			// Roster discovery never auto-links on name, phone or integration ID.
			if id(`SELECT count(*)::text FROM relay_driver_links WHERE driver_id IS NOT NULL`) != "0" {
				t.Fatal("auto-linked")
			}
			privateCtx := WithTaskViewer(ctx, actor, false)
			purchase := relay.Transaction{TransactionID: "fixture-purchase", Prompts: []relay.Prompt{}, CreatedAt: time.Now(), CurrencyCode: "USD", TotalAmountPaid: "20.00", TotalRetailPrice: "21.00", TotalAmountSaved: "1.00", Driver: relay.TransactionDriver{ID: "dr_one", FirstName: "Relay", LastName: "Driver", Phone: "+15551234567"}}
			if e = repo.UpsertDay(ctx, "production", time.Now(), []relay.Transaction{purchase}, time.Now(), false); e != nil {
				t.Fatal(e)
			}
			if id(`SELECT coalesce(driver_id::text,'unlinked') FROM fuel_transactions WHERE relay_transaction_id='fixture-purchase'`) != "unlinked" {
				t.Fatal("import inferred a driver")
			}
			if _, e = repo.LinkRelayRosterDriver(privateCtx, "dr_one", driverID, actor); !errors.Is(e, ErrNotFound) {
				t.Fatalf("bypassed task privacy: %v", e)
			}
			if _, e = repo.LinkRelayRosterDriver(ctx, "dr_one", driverID, actor); e != nil {
				t.Fatal(e)
			}
			if id(`SELECT driver_id::text FROM fuel_transactions WHERE relay_transaction_id='fixture-purchase'`) != driverID {
				t.Fatal("link did not backfill historical purchase")
			}
			exec(`UPDATE drivers SET status='terminated',active=false WHERE id=$1`, driverID)
			checkRelayCount := func(want int) {
				t.Helper()
				got, err := repo.TerminatedRelayCount(ctx)
				if err != nil || got != want {
					t.Fatalf("Relay count=%d want=%d err=%v", got, want, err)
				}
			}
			checkRelayCount(1)
			staging := NewFuelRepository(pool)
			staging.ConfigureRelayDrivers(fake, "staging")
			if n, err := staging.TerminatedRelayCount(ctx); err != nil || n != 0 {
				t.Fatal("count crossed environment boundary", err)
			}
			list, e = repo.RelayDrivers(ctx)
			if e != nil || list.Items[0].Status != "terminated" || !strings.Contains(list.Items[0].Warning, "Terminated") {
				t.Fatalf("termination warning: %+v %v", list, e)
			}
			revision := list.Items[0].Revision
			nextID := func() string { return id(`SELECT gen_random_uuid()::text`) }
			input := RelayDriverChange{RequestID: nextID(), Action: "disable", RelayDriverID: "dr_one", Revision: revision}
			if e = repo.ChangeRelayDriver(ctx, input, actor); e != nil {
				t.Fatal(e)
			}
			calls := fake.calls
			if fake.assignments["dr_one"].Enabled || fake.assignments["dr_one"].Limits[0].Usage != "15" {
				t.Fatal("disable reset usage")
			}
			checkRelayCount(1) // Disabling fuel access is not removing the account.
			if e = repo.ChangeRelayDriver(ctx, input, actor); e != nil || fake.calls != calls {
				t.Fatal("receipt replay called Relay")
			}
			input.RequestID = nextID()
			input.Action = "enable"
			if e = repo.ChangeRelayDriver(ctx, input, actor); !errors.Is(e, ErrRelayDriverConflict) || fake.calls != calls {
				t.Fatalf("stale action accepted: %v", e)
			}
			list, _ = repo.RelayDrivers(ctx)
			input.Revision = list.Items[0].Revision
			input.RequestID = nextID()
			input.Action = "assign"
			input.PolicyID = "plc_flinn"
			if e = repo.ChangeRelayDriver(ctx, input, actor); e == nil || fake.calls != calls {
				t.Fatal("Flinn policy allowed")
			}
			input.PolicyID = "plc_non"
			input.RequestID = nextID()
			if e = repo.ChangeRelayDriver(ctx, input, actor); e != nil {
				t.Fatal(e)
			}
			if fake.drivers["dr_one"].DataFields[0].Value != "secret123" {
				t.Fatal("blank PIN did not preserve existing PIN")
			}
			// Failed refresh never replaces the last complete snapshot.
			fake.readFailure = true
			if e = repo.RefreshRelayDrivers(ctx); e == nil {
				t.Fatal("failed refresh accepted")
			}
			fake.readFailure = false
			list, e = repo.RelayDrivers(ctx)
			if e != nil || len(list.Items) != 1 {
				t.Fatal("lost cache on failure")
			}
			input = RelayDriverChange{RequestID: nextID(), Action: "remove", RelayDriverID: "dr_one", Revision: list.Items[0].Revision}
			fake.uncertain = true
			if e = repo.ChangeRelayDriver(ctx, input, actor); e == nil {
				t.Fatal("uncertain remove accepted")
			}
			calls = fake.calls
			fake.uncertain = false
			input.RequestID = nextID()
			if e = repo.ChangeRelayDriver(ctx, input, actor); !errors.Is(e, ErrRelayDriverConflict) || fake.calls != calls {
				t.Fatal("replayed uncertain mutation")
			}
			pending := id(`SELECT id::text FROM relay_driver_operations WHERE state='pending'`)
			if e = repo.VerifyRelayOperation(ctx, pending, actor); !errors.Is(e, ErrRelayDriverConflict) {
				t.Fatal("verified in-flight request")
			}
			exec(`UPDATE relay_driver_operations SET created_at=now()-interval '3 minutes' WHERE id=$1`, pending)
			if e = repo.VerifyRelayOperation(ctx, pending, actor); e != nil {
				t.Fatal(e)
			}
			if e = repo.ChangeRelayDriver(ctx, input, actor); e != nil {
				t.Fatal(e)
			}
			if id(`SELECT driver_id::text FROM relay_driver_links WHERE relay_driver_id='dr_one'`) != ""+driverID {
				t.Fatal("removal deleted identity history")
			}
			checkRelayCount(0) // Historical links survive removal but no longer count.
			if id(`SELECT driver_id::text FROM fuel_transactions WHERE relay_transaction_id='fixture-purchase'`) != driverID {
				t.Fatal("removal changed historical purchase")
			}
			add := RelayDriverChange{RequestID: nextID(), Action: "add", DriverID: second, FirstName: "New", LastName: "Driver", Phone: "+15557654321"}
			if e = repo.ChangeRelayDriver(ctx, add, actor); e != nil {
				t.Fatal(e)
			}
			list, e = repo.RelayDrivers(ctx)
			if e != nil || len(list.Items) != 1 || list.Items[0].DriverID != second || list.Items[0].Assignment != nil {
				t.Fatalf("create/link: %+v %v", list, e)
			}
			calls = fake.calls
			add.RequestID = nextID()
			if e = repo.ChangeRelayDriver(ctx, add, actor); e == nil || calls != fake.calls {
				t.Fatal("duplicate driver added")
			}
			entry := list.Items[0]
			input = RelayDriverChange{RequestID: nextID(), Action: "assign", RelayDriverID: entry.Driver.ID, Revision: entry.Revision, PolicyID: "plc_non"}
			if e = repo.ChangeRelayDriver(ctx, input, actor); e == nil {
				t.Fatal("non ELD missing PIN accepted")
			}
			input.DriverPIN = "7890"
			input.TruckNumber = "202"
			if e = repo.ChangeRelayDriver(ctx, input, actor); e != nil {
				t.Fatal(e)
			}
			list, _ = repo.RelayDrivers(ctx)
			encoded, _ = json.Marshal(list)
			if strings.Contains(string(encoded), "7890") {
				t.Fatal("new PIN leaked")
			}
			testSidebarCounts(t, ctx, pool, driverID, second)
			testRelayOffboarding(t, ctx, pool, actor)
			testPrePassTruckLinks(t, ctx, pool, actor)
			testDriverServices(t, ctx, pool, actor)
		})
	}
}
