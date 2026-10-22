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
	"mserp/internal/prepass"
)

type prepassFleetFake struct {
	snapshot    prepass.FleetSnapshot
	calls       int
	failure     error
	readFailure bool
	ignoreWrite bool
}

func (f *prepassFleetFake) Fleet(context.Context) (prepass.FleetSnapshot, error) {
	if f.readFailure {
		return prepass.FleetSnapshot{}, errors.New("fixture read failure")
	}
	b, _ := json.Marshal(f.snapshot)
	var s prepass.FleetSnapshot
	_ = json.Unmarshal(b, &s)
	return s, nil
}
func (f *prepassFleetFake) ChangeVehicle(_ context.Context, action, account, vin string, in prepass.VehicleInput, device string) error {
	f.calls++
	if f.failure != nil {
		return f.failure
	}
	if f.ignoreWrite {
		return nil
	}
	if action == "add" {
		f.snapshot.Vehicles = append(f.snapshot.Vehicles, prepass.Vehicle{AccountNumber: account, VIN: vin})
	}
	for i := range f.snapshot.Vehicles {
		v := &f.snapshot.Vehicles[i]
		if v.AccountNumber != account || v.VIN != vin {
			continue
		}
		switch action {
		case "plate":
			v.TempPlate = in.LicensePlate.Temporary
			v.LicensePlateNumber = in.LicensePlate.Number
			v.LicensePlateState = in.LicensePlate.State
		case "add", "edit":
			v.VehicleNumber = in.VehicleNumber
			v.TempPlate = in.LicensePlate.Temporary
			v.LicensePlateNumber = in.LicensePlate.Number
			v.LicensePlateState = in.LicensePlate.State
			v.DeclaredWeight = in.DeclaredWeight
			v.IRPNumber = in.IRP.AccountNumber
			v.IRPState = in.IRP.State
			v.IFTANumber = in.IFTA.AccountNumber
			v.IFTAState = in.IFTA.State
			v.VRN = in.VRN
			v.HazmatHauler = in.HazmatHauler
		case "assign":
			v.TransponderNumber = device
			for j := range f.snapshot.Transponders {
				d := &f.snapshot.Transponders[j]
				if d.TransponderNumber == device {
					d.VIN = vin
					d.VehicleNumber = v.VehicleNumber
				}
			}
		case "remove":
			f.snapshot.Vehicles = append(f.snapshot.Vehicles[:i], f.snapshot.Vehicles[i+1:]...)
		}
		break
	}
	return nil
}
func TestPrePassDeviceAvailability(t *testing.T) {
	s := prepass.FleetSnapshot{Vehicles: []prepass.Vehicle{}, Transponders: []prepass.Transponder{{AccountNumber: "123", TransponderNumber: "777111222"}}}
	if !availablePrePassDevice(s, "123", "777111222") {
		t.Fatal("available device hidden")
	}
	if availablePrePassDevice(s, "456", "777111222") {
		t.Fatal("cross-account device")
	}
	s.Transponders[0].VehicleNumber = "003"
	if availablePrePassDevice(s, "123", "777111222") {
		t.Fatal("partial assignment ignored")
	}
	s.Transponders[0].VehicleNumber = ""
	s.Vehicles = append(s.Vehicles, prepass.Vehicle{TransponderNumber: "777111222"})
	if availablePrePassDevice(s, "123", "777111222") {
		t.Fatal("inconsistent rosters accepted")
	}
}
func TestPrePassFleetDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_PREPASS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_PREPASS_TEST_DATABASE_URL to a disposable local _test database")
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
			schema := fmt.Sprintf("prepass_fleet_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			init, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			if mode == "migration" {
				before, _, ok := strings.Cut(string(init), "-- PrePass fleet management (migration 082).")
				if !ok {
					t.Fatal("missing migration boundary")
				}
				exec(before)
				migration, e := os.ReadFile("../../sql/082_prepass_fleet.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(migration))
				migration, e = os.ReadFile("../../sql/083_prepass_fleet_toll_links.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(migration))
			} else {
				exec(string(init))
			}
			if mode == "migration" {
				migration, e := os.ReadFile("../../sql/120_prepass_assignment_queue.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(migration))
			}
			// Existing fleet tables need read/row-lock privileges; new tables must already
			// be owned by the application role via the migration, not a blanket grant.
			exec(`GRANT SELECT,UPDATE ON trucks TO mserp_app; GRANT SELECT ON ALL TABLES IN SCHEMA ` + quoted + ` TO mserp_app; GRANT INSERT,UPDATE ON tolls,prepass_toll_sync_days TO mserp_app`)
			pc := cfg.Copy()
			pc.MaxConns = 2
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
				if e := admin.QueryRow(ctx, sql, args...).Scan(&s); e != nil {
					t.Fatal(e)
				}
				return s
			}
			for _, table := range []string{"prepass_fleet_rosters", "prepass_fleet_links", "prepass_fleet_operations"} {
				if id(`SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename=$2`, schema, table) != "mserp_app" {
					t.Fatal("missing app ownership")
				}
			}
			actor := id(`INSERT INTO app_users(username,password_hash) VALUES('prepass-test','$2test') RETURNING id::text`)
			vin := "1XKADP9X8DJ364740"
			newVIN := "1XKADP9X8DJ364741"
			truck := id(`INSERT INTO trucks(unit_number,vin) VALUES('003',$1) RETURNING id::text`, vin)
			newTruck := id(`INSERT INTO trucks(unit_number,vin) VALUES('004',$1) RETURNING id::text`, newVIN)
			missingVINTruck := id(`INSERT INTO trucks(unit_number) VALUES('005') RETURNING id::text`)
			fake := &prepassFleetFake{snapshot: prepass.FleetSnapshot{Accounts: []string{"123"}, Vehicles: []prepass.Vehicle{{AccountNumber: "123", VIN: vin, VehicleNumber: "003", LicensePlateNumber: "ABC123", LicensePlateState: "OH", DeclaredWeight: 80000, IRPNumber: "12345", IRPState: "OH", IFTANumber: "67890", IFTAState: "OH", VRN: "791"}}, Transponders: []prepass.Transponder{{AccountNumber: "123", TransponderNumber: "777111222"}}}}
			repo := NewTollRepository(pool)
			repo.ConfigurePrePassFleet(fake, "production")
			list := func() PrePassFleetList {
				t.Helper()
				l, e := repo.PrePassFleet(ctx)
				if e != nil {
					t.Fatal(e)
				}
				return l
			}
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			reqID := func() string { return id(`SELECT gen_random_uuid()::text`) }
			choice := func(tid string) PrePassFleetTruckChoice {
				for _, c := range list().Trucks {
					if c.ID == tid {
						return c
					}
				}
				t.Fatal("missing truck")
				return PrePassFleetTruckChoice{}
			}
			request := func(action string) PrePassFleetChange {
				v := list().Vehicles[0]
				return PrePassFleetChange{RequestID: reqID(), Action: action, AccountNumber: "123", VIN: vin, Revision: v.Revision, Vehicle: v.Vehicle.Input()}
			}
			must(repo.RefreshPrePassFleet(ctx))
			l := list()
			if len(l.Vehicles) != 1 || l.Vehicles[0].TruckID != "" || len(l.Vehicles[0].Suggestions) != 1 {
				t.Fatal("roster inferred a link or lost suggestions")
			}
			link := request("link")
			link.TollFrom = "2026-10-01"
			link.TruckID = truck
			link.TruckUpdatedAt = choice(truck).UpdatedAt.Format(time.RFC3339Nano)
			staleTruck := link
			staleTruck.TruckUpdatedAt = choice(truck).UpdatedAt.Add(-time.Second).Format(time.RFC3339Nano)
			if !errors.Is(repo.ChangePrePassFleet(ctx, staleTruck, actor), ErrPrePassFleetConflict) || fake.calls != 0 {
				t.Fatal("changed local truck selection accepted")
			}
			preview, err := repo.PreviewPrePassFleetLink(ctx, link)
			must(err)
			link.TollRevision = preview.Revision
			must(repo.ChangePrePassFleet(ctx, link, actor))
			must(repo.ChangePrePassFleet(ctx, link, actor))
			if list().Vehicles[0].TruckID != truck || fake.calls != 0 {
				t.Fatal("local link called provider or failed")
			}
			t.Run("resolve plates", func(t *testing.T) {
				makePlate := func(source string) PrePassFleetChange {
					in := request("edit")
					in.PlateSource, in.TruckID = source, truck
					in.TruckUpdatedAt = choice(truck).UpdatedAt.Format(time.RFC3339Nano)
					in.Vehicle = prepass.VehicleInput{} // No registration fields needed.
					return in
				}
				exec(`UPDATE trucks SET license_plate='LOCAL12',license_state='IN',updated_at=clock_timestamp() WHERE id=$1`, truck)
				stale := makePlate("mserp")
				exec(`UPDATE trucks SET updated_at=clock_timestamp() WHERE id=$1`, truck)
				if !errors.Is(repo.ChangePrePassFleet(ctx, stale, actor), ErrPrePassFleetConflict) || fake.calls != 0 {
					t.Fatal("stale plate selection reached provider")
				}
				wrong := makePlate("prepass")
				wrong.TruckID = newTruck
				if repo.ChangePrePassFleet(ctx, wrong, actor) == nil || fake.calls != 0 {
					t.Fatal("unlinked truck accepted")
				}
				in := makePlate("mserp")
				must(repo.ChangePrePassFleet(ctx, in, actor))
				must(repo.ChangePrePassFleet(ctx, in, actor))
				if fake.calls != 1 || fake.snapshot.Vehicles[0].LicensePlateNumber != "LOCAL12" || fake.snapshot.Vehicles[0].LicensePlateState != "IN" || fake.snapshot.Vehicles[0].IRPNumber != "12345" {
					t.Fatal("MSERP source, unrelated fields or replay incorrect")
				}
				if id(`SELECT before_vehicle->'TruckPlate'->>'number' FROM prepass_fleet_operations WHERE id=$1`, in.RequestID) != "LOCAL12" {
					t.Fatal("missing local plate audit")
				}
				fake.snapshot.Vehicles[0].LicensePlateNumber, fake.snapshot.Vehicles[0].LicensePlateState = "VENDOR1", "IL"
				must(repo.RefreshPrePassFleet(ctx))
				in = makePlate("prepass")
				in.FleetServiceConfirmed = false
				// Even a provider write rejection must not affect a local acceptance.
				fake.failure = &prepass.FleetAPIError{Status: 403}
				must(repo.ChangePrePassFleet(ctx, in, actor))
				must(repo.ChangePrePassFleet(ctx, in, actor))
				fake.failure = nil
				if fake.calls != 1 {
					t.Fatal("copying PrePass plate sent a vendor mutation")
				}
				if choice(truck).Plate != "VENDOR1" || choice(truck).PlateState != "IL" || list().Vehicles[0].Warning == "Plate differs from Clustie" {
					t.Fatal("PrePass source did not update local profile")
				}
				exec(`UPDATE trucks SET license_plate='BAD',updated_at=clock_timestamp() WHERE id=$1`, truck)
				calls := fake.calls
				if repo.ChangePrePassFleet(ctx, makePlate("mserp"), actor) == nil || fake.calls != calls {
					t.Fatal("invalid source plate sent to provider")
				}
				exec(`UPDATE trucks SET license_plate='LOCAL99',updated_at=clock_timestamp() WHERE id=$1`, truck)
				in = makePlate("mserp")
				fake.ignoreWrite = true
				if repo.ChangePrePassFleet(ctx, in, actor) == nil || choice(truck).Plate != "LOCAL99" || id(`SELECT state FROM prepass_fleet_operations WHERE id=$1`, in.RequestID) != "pending" {
					t.Fatal("unverified plate was confirmed")
				}
				if repo.ChangePrePassFleet(ctx, in, actor) == nil || fake.calls != calls+1 {
					t.Fatal("uncertain plate request replayed")
				}
				fake.ignoreWrite = false
				exec(`UPDATE prepass_fleet_operations SET created_at=now()-interval '3 minutes' WHERE id=$1`, in.RequestID)
				must(repo.VerifyPrePassFleetOperation(ctx, in.RequestID, actor))
				if choice(truck).Plate != "LOCAL99" {
					t.Fatal("review silently overwrote local profile")
				}
				// Return the fixture to the baseline for existing lifecycle assertions.
				fake.snapshot.Vehicles[0].LicensePlateNumber, fake.snapshot.Vehicles[0].LicensePlateState = "ABC123", "OH"
				fake.calls = 0
				must(repo.RefreshPrePassFleet(ctx))
			})
			stale := link
			stale.RequestID = reqID()
			if !errors.Is(repo.ChangePrePassFleet(ctx, stale, actor), ErrPrePassFleetConflict) {
				t.Fatal("stale link revision accepted")
			}
			edit := request("edit")
			edit.Vehicle.VehicleNumber = "003A"
			must(repo.ChangePrePassFleet(ctx, edit, actor))
			must(repo.ChangePrePassFleet(ctx, edit, actor))
			if fake.calls != 1 {
				t.Fatal("repeated successful write")
			}
			edit.Vehicle.VehicleNumber = "003B"
			if !errors.Is(repo.ChangePrePassFleet(ctx, edit, actor), ErrPrePassFleetConflict) {
				t.Fatal("request ID reused with changed payload")
			}
			assign := request("assign")
			assign.TransponderNumber = "777111222"
			fake.snapshot.Transponders[0].VIN = newVIN
			if repo.ChangePrePassFleet(ctx, assign, actor) == nil || fake.calls != 1 {
				t.Fatal("stale available device was assigned")
			}
			fake.snapshot.Transponders[0].VIN = ""
			must(repo.ChangePrePassFleet(ctx, assign, actor))
			if fake.calls != 2 {
				t.Fatal("missing assignment")
			}
			must(repo.ChangePrePassFleet(ctx, assign, actor))
			if fake.calls != 2 {
				t.Fatal("assignment replayed")
			}
			t.Run("queued assignments survive restart without replay", func(t *testing.T) {
				raw, _ := json.Marshal(fake.snapshot)
				calls := fake.calls
				var ids []string
				defer func() {
					fake.ignoreWrite = false
					fake.readFailure = false
					fake.calls = calls
					_ = json.Unmarshal(raw, &fake.snapshot)
					exec(`DELETE FROM prepass_fleet_operations WHERE id=ANY($1::uuid[])`, ids)
					must(repo.RefreshPrePassFleet(ctx))
				}()
				fake.ignoreWrite = true // HTTP success acknowledges the vendor queue.
				var requests []PrePassFleetChange
				for i, device := range []string{"777111444", "777111555"} {
					v := fake.snapshot.Vehicles[0]
					v.VIN = fmt.Sprintf("1XKADP9X8DJ36474%d", i+2)
					v.VehicleNumber = fmt.Sprintf("Q%d", i)
					v.TransponderNumber = ""
					fake.snapshot.Vehicles = append(fake.snapshot.Vehicles, v)
					fake.snapshot.Transponders = append(fake.snapshot.Transponders, prepass.Transponder{AccountNumber: "123", TransponderNumber: device})
					must(repo.RefreshPrePassFleet(ctx))
					in := PrePassFleetChange{RequestID: reqID(), Action: "assign", AccountNumber: "123", VIN: v.VIN, Revision: prepassRevision(v, ""), TransponderNumber: device}
					ids = append(ids, in.RequestID)
					requests = append(requests, in)
					must(repo.ChangePrePassFleet(ctx, in, actor))
					if id(`SELECT state FROM prepass_fleet_operations WHERE id=$1`, in.RequestID) != "queued" {
						t.Fatal("accepted assignment was not queued")
					}
					must(repo.ChangePrePassFleet(ctx, in, actor))
					blocked := in
					blocked.RequestID = reqID()
					if repo.ChangePrePassFleet(ctx, blocked, actor) == nil {
						t.Fatal("queued truck/device sent twice")
					}
				}
				if fake.calls != calls+2 {
					t.Fatal("queued request replayed or blocked an unrelated truck")
				}
				restarted := NewTollRepository(pool)
				restarted.ConfigurePrePassFleet(fake, "production")
				fake.readFailure = true
				if restarted.ReconcilePrePassAssignments(ctx) == nil {
					t.Fatal("failed read accepted")
				}
				fake.readFailure = false
				for _, in := range requests {
					for i := range fake.snapshot.Vehicles {
						if fake.snapshot.Vehicles[i].VIN == in.VIN {
							fake.snapshot.Vehicles[i].TransponderNumber = in.TransponderNumber
						}
					}
				}
				must(restarted.ReconcilePrePassAssignments(ctx))
				if id(`SELECT count(*)::text FROM prepass_fleet_operations WHERE id=ANY($1::uuid[]) AND state='queued'`, ids) != "2" {
					t.Fatal("one-sided roster confirmed an assignment")
				}
				for _, in := range requests {
					for i := range fake.snapshot.Transponders {
						if fake.snapshot.Transponders[i].TransponderNumber == in.TransponderNumber {
							fake.snapshot.Transponders[i].VIN = in.VIN
						}
					}
				}
				must(restarted.ReconcilePrePassAssignments(ctx))
				if id(`SELECT count(*)::text FROM prepass_fleet_operations WHERE id=ANY($1::uuid[]) AND state='confirmed'`, ids) != "2" || fake.calls != calls+2 {
					t.Fatal("background confirmation failed or resent mutations")
				}
			})
			testFleetTollLinking(t, ctx, admin, repo, fake, actor, truck, newTruck)
			checked := list().RefreshedAt
			fake.readFailure = true
			if repo.RefreshPrePassFleet(ctx) == nil {
				t.Fatal("failed refresh succeeded")
			}
			if !list().RefreshedAt.Equal(*checked) {
				t.Fatal("partial refresh replaced snapshot")
			}
			fake.readFailure = false
			// Another process owns the same environment lock.
			conn, done, e := repo.prepassLock(ctx)
			must(e)
			_ = conn
			if !errors.Is(repo.RefreshPrePassFleet(ctx), ErrPrePassFleetConflict) {
				t.Fatal("concurrent mutation lock bypassed")
			}
			done()
			remove := request("remove")
			fake.failure = &prepass.FleetAPIError{Uncertain: true}
			if repo.ChangePrePassFleet(ctx, remove, actor) == nil {
				t.Fatal("uncertain write reported success")
			}
			calls := fake.calls
			if !errors.Is(repo.ChangePrePassFleet(ctx, remove, actor), ErrPrePassFleetConflict) || fake.calls != calls {
				t.Fatal("uncertain write replayed")
			}
			must(repo.RefreshPrePassFleet(ctx))
			if list().Operations[0].State != "pending" {
				t.Fatal("refresh resolved uncertainty")
			}
			if !errors.Is(repo.VerifyPrePassFleetOperation(ctx, remove.RequestID, actor), ErrPrePassFleetConflict) {
				t.Fatal("premature recovery")
			}
			exec(`UPDATE prepass_fleet_operations SET created_at=now()-interval '3 minutes' WHERE id=$1`, remove.RequestID)
			must(repo.VerifyPrePassFleetOperation(ctx, remove.RequestID, actor))
			fake.failure = nil
			remove = request("remove")
			must(repo.ChangePrePassFleet(ctx, remove, actor))
			if len(list().Vehicles) != 0 || id(`SELECT count(*)::text FROM trucks WHERE id=$1`, truck) != "1" || id(`SELECT count(*)::text FROM prepass_fleet_links`) != "1" {
				t.Fatal("remote removal destroyed local truck/link")
			}
			add := PrePassFleetChange{RequestID: reqID(), Action: "add", AccountNumber: "123", VIN: newVIN, TruckID: newTruck, TruckUpdatedAt: choice(newTruck).UpdatedAt.Format(time.RFC3339Nano), Vehicle: edit.Vehicle}
			add.Vehicle.VehicleNumber = "004"
			fake.failure = &prepass.FleetAPIError{Status: 400}
			if repo.ChangePrePassFleet(ctx, add, actor) == nil {
				t.Fatal("rejection succeeded")
			}
			if id(`SELECT state FROM prepass_fleet_operations WHERE id=$1`, add.RequestID) != "failed" {
				t.Fatal("definite failure not recorded")
			}
			fake.failure = nil
			add.RequestID = reqID()
			must(repo.ChangePrePassFleet(ctx, add, actor))
			if list().Vehicles[0].TruckID != newTruck {
				t.Fatal("created truck not linked")
			}
			calls = fake.calls
			add.RequestID = reqID()
			if repo.ChangePrePassFleet(ctx, add, actor) == nil || fake.calls != calls {
				t.Fatal("duplicate VIN created")
			}
			// An incomplete local truck may supply its missing VIN for enrollment.
			// It must not use another local truck's identity or overwrite the profile.
			missing := add
			missing.RequestID = reqID()
			missing.TruckID = missingVINTruck
			missing.TruckUpdatedAt = choice(missingVINTruck).UpdatedAt.Format(time.RFC3339Nano)
			missing.VIN = vin // removed upstream, but still owned by another local truck
			missing.Vehicle.VehicleNumber = "005"
			calls = fake.calls
			if repo.ChangePrePassFleet(ctx, missing, actor) == nil || fake.calls != calls {
				t.Fatal("missing-VIN truck took another local truck's identity")
			}
			missing.VIN = "1XKADP9X8DJ364742"
			must(repo.ChangePrePassFleet(ctx, missing, actor))
			if id(`SELECT coalesce(vin,'') FROM trucks WHERE id=$1`, missingVINTruck) != "" {
				t.Fatal("enrollment changed the local truck profile")
			}
			if id(`SELECT truck_id::text FROM prepass_fleet_links WHERE vin=$1`, missing.VIN) != missingVINTruck {
				t.Fatal("missing-VIN enrollment was not linked")
			}
			// Enrollment with a supplied missing VIN retains its reviewed identity
			// for the subsequent dated toll link without modifying the profile.
			missingLink := PrePassFleetChange{RequestID: reqID(), Action: "link", AccountNumber: "123", VIN: missing.VIN, TruckID: missingVINTruck, TruckUpdatedAt: choice(missingVINTruck).UpdatedAt.Format(time.RFC3339Nano), TollFrom: "2026-10-12"}
			for _, e := range list().Vehicles {
				if e.Vehicle.VIN == missing.VIN {
					missingLink.Revision = e.Revision
				}
			}
			mp, me := repo.PreviewPrePassFleetLink(ctx, missingLink)
			must(me)
			missingLink.TollRevision = mp.Revision
			must(repo.ChangePrePassFleet(ctx, missingLink, actor))
			edit = request("edit")
			edit.VIN = newVIN
			edit.Vehicle.VehicleNumber = "006"
			fake.ignoreWrite = true
			if repo.ChangePrePassFleet(ctx, edit, actor) == nil {
				t.Fatal("accepted unverified write")
			}
			if id(`SELECT state FROM prepass_fleet_operations WHERE id=$1`, edit.RequestID) != "pending" {
				t.Fatal("unverified write lost pending receipt")
			}
			other := NewTollRepository(pool)
			other.ConfigurePrePassFleet(fake, "nonproduction")
			if len(mustPrePassList(t, other, ctx).Vehicles) != 0 {
				t.Fatal("environment roster leaked")
			}
		})
	}
}
func mustPrePassList(t *testing.T, r *TollRepository, ctx context.Context) PrePassFleetList {
	t.Helper()
	l, e := r.PrePassFleet(ctx)
	if e != nil {
		t.Fatal(e)
	}
	return l
}
