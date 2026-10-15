package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/fleetscope"
)

func TestFleetScopeSyncDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_FLEETSCOPE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated local _test database")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("disposable local _test database required")
	}
	ctx := context.Background()
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	baseline, e := os.ReadFile("../../sql/init.sql")
	if e != nil {
		t.Fatal(e)
	}
	migration, e := os.ReadFile("../../sql/103_fleetscope_fleet_sync.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, incremental := range []bool{false, true} {
		t.Run(fmt.Sprintf("incremental=%v", incremental), func(t *testing.T) {
			schema := fmt.Sprintf("fleetscope_sync_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			if _, e := admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
				t.Fatal(e)
			}
			defer admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
			config := cfg.Copy()
			config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			pool, e := pgxpool.NewWithConfig(ctx, config)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			sql := string(baseline)
			if incremental {
				sql = strings.Split(sql, "-- FleetScope outbound fleet sync (103)")[0]
			}
			if _, e = pool.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
			if incremental {
				if _, e = pool.Exec(ctx, string(migration)); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = pool.Exec(ctx, "GRANT USAGE ON SCHEMA "+quoted+" TO mserp_app"); e != nil {
				t.Fatal(e)
			}
			// Production already grants the runtime role access to source fleet tables.
			if _, e = pool.Exec(ctx, "GRANT SELECT,INSERT,UPDATE,DELETE ON drivers,trucks,investors,truck_driver_assignments,driver_teams,truck_ownership_history,fleetscope_driver_intake,fleetscope_webhook_receipts TO mserp_app"); e != nil {
				t.Fatal(e)
			}
			config.ConnConfig.RuntimeParams["role"] = "mserp_app"
			app, e := pgxpool.NewWithConfig(ctx, config)
			if e != nil {
				t.Fatal(e)
			}
			defer app.Close()
			testSyncLifecycle(t, app, pool)
		})
	}
}

func testSyncLifecycle(t *testing.T, pool, admin *pgxpool.Pool) {
	ctx := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	company, epoch, actor := fleetscope.NewID(), fleetscope.NewID(), fleetscope.NewID()
	var companyOwner, investor, driver, truck string
	must(admin.QueryRow(ctx, `SELECT id::text FROM investors WHERE is_company`).Scan(&companyOwner))
	must(admin.QueryRow(ctx, `INSERT INTO investors(full_name) VALUES('Independent Investor') RETURNING id::text`).Scan(&investor))
	must(admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Test Driver','test driver','cpm',0.5) RETURNING id::text`).Scan(&driver))
	must(admin.QueryRow(ctx, `INSERT INTO trucks(unit_number,vin,year,make,model,license_plate,license_state,owner_id) VALUES('001','1FUJGLDR9CSBP0001',2022,'Freightliner','Cascadia','ABC123','IL',$1) RETURNING id::text`, investor).Scan(&truck))
	// Use the source assignment table rather than assuming truck.current_driver is authoritative.
	_, e := admin.Exec(ctx, `INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES($1,$2,now())`, truck, driver)
	must(e)
	opts := fleetscope.SyncOptions{Mode: "reconcile", CompanyID: company, Environment: "staging", KeyID: "live", Secret: strings.Repeat("a", 32), BootstrapKeyID: "review", BootstrapSecret: strings.Repeat("b", 32)}
	repo := NewFleetSyncRepository(pool, opts)
	local, e := readSyncLocal(ctx, pool, company)
	must(e)
	if len(local.Trucks) != 1 || local.Trucks[0].Truck.Ownership.Kind != "investor" || local.Trucks[0].Truck.Ownership.MSERPOwnerID != investor || local.Trucks[0].PrimaryID != driver {
		t.Fatalf("ownership and crew projection incorrect: %+v", local)
	}
	if issues := local.Trucks[0].Issues; len(issues) != 0 {
		t.Fatal(issues)
	}
	before := local.SourceRevision
	tx, e := admin.Begin(ctx)
	must(e)
	_, e = tx.Exec(ctx, `UPDATE investors SET full_name='Rolled back' WHERE id=$1`, investor)
	must(e)
	must(tx.Rollback(ctx))
	local, e = readSyncLocal(ctx, pool, company)
	must(e)
	if local.SourceRevision != before {
		t.Fatal("rolled back capture persisted")
	}
	remoteDriver, remoteTruck := fleetscope.NewID(), fleetscope.NewID()
	remoteRevision, assignmentRevision := "t1", "a1"
	remoteBinding, driverBinding := "", ""
	var projected *fleetscope.TruckProjection
	var primary string
	requests := 0
	failOnce := false
	applied := map[string]fleetscope.Result{}
	bodies := map[string]string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		key := r.Header.Get("X-MSERP-Key-ID")
		secret := opts.Secret
		if key == opts.BootstrapKeyID {
			secret = opts.BootstrapSecret
		}
		if r.Header.Get("X-MSERP-Signature") != fleetscope.SyncSignature(secret, r.Method, r.URL.RequestURI(), r.Header.Get("X-MSERP-Timestamp"), r.Header.Get("X-MSERP-Nonce"), key, "staging", company, body) {
			t.Error("bad request signature")
		}
		if strings.HasSuffix(r.URL.Path, "/capabilities") {
			json.NewEncoder(w).Encode(fleetscope.Capabilities{Version: 1, ContractHash: fleetscope.ContractHash(), Ownership: fleetscope.OwnershipCapability, CompanyID: company, Environment: "staging", Epoch: epoch, Mode: "live"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/snapshots") {
			owner := json.RawMessage(`null`)
			if projected != nil {
				owner, _ = json.Marshal(projected.Ownership)
			}
			json.NewEncoder(w).Encode(fleetscope.Snapshot{ID: fleetscope.NewID(), CompanyID: company, Environment: "staging", Epoch: epoch, CapturedAt: time.Now().UTC(), Drivers: []fleetscope.RemoteDriver{{ID: remoteDriver, Name: "Test Driver", Status: "active", Revision: "d1", MSERPID: driverBinding}}, Trucks: []fleetscope.RemoteTruck{{ID: remoteTruck, UnitNumber: "001", VIN: "1FUJGLDR9CSBP0001", Revision: remoteRevision, AssignmentRevision: assignmentRevision, MSERPID: remoteBinding, Ownership: owner, Projection: projected, PrimaryID: primary}}})
			return
		}
		var op fleetscope.Operation
		must(json.Unmarshal(body, &op))
		if old, ok := applied[op.OperationID]; ok {
			if bodies[op.OperationID] != string(body) {
				t.Error("replay bytes changed")
			}
			old.Status = "duplicate"
			json.NewEncoder(w).Encode(old)
			return
		}
		result := fleetscope.Result{OperationID: op.OperationID, Status: "applied", CompanyID: company, Environment: "staging", Epoch: epoch, SourceRevision: op.SourceRevision, AssignmentSequence: op.AssignmentSequence}
		switch op.Kind {
		case "driver.link":
			driverBinding = op.MSERPID
			result.FleetScopeID = remoteDriver
			result.Revision = "d1"
		case "truck.link", "truck.upsert":
			if op.ExpectedRevision != remoteRevision {
				w.WriteHeader(409)
				io.WriteString(w, `{"code":"revision_conflict"}`)
				return
			}
			projected = op.Truck
			remoteBinding = op.MSERPID
			remoteRevision = fmt.Sprintf("t%d", op.SourceRevision+1)
			result.FleetScopeID = remoteTruck
			result.Revision = remoteRevision
			result.AssignmentRevision = assignmentRevision
		case "assignments.apply":
			if len(op.Crews) != 1 || op.Crews[0].ExpectedRevision != assignmentRevision {
				w.WriteHeader(409)
				io.WriteString(w, `{"code":"revision_conflict"}`)
				return
			}
			assignmentRevision = fmt.Sprintf("a%d", op.AssignmentSequence+1)
			result.Crews = op.Crews
			result.Crews[0].ExpectedRevision = assignmentRevision
			primary = ""
			if result.Crews[0].Primary != nil {
				primary = result.Crews[0].Primary.FleetScopeDriverID
			}
		default:
			t.Errorf("unexpected operation %s", op.Kind)
		}
		bodies[op.OperationID] = string(body)
		applied[op.OperationID] = result
		if failOnce {
			failOnce = false
			w.WriteHeader(503)
			io.WriteString(w, `{"code":"lost_response"}`)
			return
		}
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	repo.client.Options.URL = server.URL + "/integrations/mserp/v1"
	repo.client.HTTP = server.Client()
	must(repo.Process(ctx))
	if requests != 0 {
		t.Fatal("off state made network calls")
	}
	must(repo.CaptureReview(ctx, actor))
	view, e := repo.View(ctx)
	must(e)
	input := FleetSyncReviewInput{SnapshotID: view.SnapshotID, SourceRevision: view.Local.SourceRevision, Kind: "truck", Action: "exclude", LocalID: truck, Reason: "test"}
	if repo.Review(ctx, input, actor) == nil {
		t.Fatal("assigned truck excluded")
	}
	input.Action = "link"
	input.RemoteID = remoteTruck
	input.SourceRevision--
	if repo.Review(ctx, input, actor) == nil {
		t.Fatal("stale review accepted")
	}
	input.SourceRevision++
	must(repo.Review(ctx, input, actor))
	if repo.Review(ctx, input, actor) == nil {
		t.Fatal("duplicate pending review accepted")
	}
	// Response loss after remote commit must replay exactly once, preserving original bytes.
	failOnce = true
	must(repo.Process(ctx))
	var id string
	must(pool.QueryRow(ctx, `SELECT id::text FROM fleetscope_sync_operations WHERE state='pending'`).Scan(&id))
	_, e = pool.Exec(ctx, `UPDATE fleetscope_sync_operations SET payload=convert_to('{}','UTF8') WHERE id=$1`, id)
	if e == nil {
		t.Fatal("immutable payload was changed")
	}
	_, e = pool.Exec(ctx, `UPDATE fleetscope_sync_operations SET next_attempt_at=now() WHERE id=$1`, id)
	must(e)
	must(repo.Process(ctx))
	view, e = repo.View(ctx)
	must(e)
	if view.Links[0].RemoteID == "" {
		t.Fatal("missing ack after replay")
	}
	if len(applied) != 1 {
		t.Fatal("duplicate remote apply")
	}
	must(repo.CaptureReview(ctx, actor))
	view, e = repo.View(ctx)
	must(e)
	if repo.PrepareAssignments(ctx, view.SnapshotID, view.Local.SourceRevision, actor) == nil {
		t.Fatal("assignment without driver link accepted")
	}
	input = FleetSyncReviewInput{SnapshotID: view.SnapshotID, SourceRevision: view.Local.SourceRevision, Kind: "driver", Action: "link", LocalID: driver, RemoteID: remoteDriver, Reason: "verified same person"}
	must(repo.Review(ctx, input, actor))
	var confirmed bool
	must(pool.QueryRow(ctx, `SELECT confirmed FROM fleetscope_driver_identity WHERE company_id=$1 AND driver_id=$2`, company, driver).Scan(&confirmed))
	if confirmed {
		t.Fatal("pending review granted lifecycle authority before acknowledgment")
	}
	_, e = pool.Exec(ctx, `INSERT INTO fleetscope_driver_identity(company_id,fleetscope_driver_id,driver_id,source) VALUES($1,$2,$3,'intake')`, company, fleetscope.NewID(), driver)
	if e == nil {
		t.Fatal("concurrent intake stole a reserved identity")
	}
	must(repo.Process(ctx))
	must(pool.QueryRow(ctx, `SELECT confirmed FROM fleetscope_driver_identity WHERE company_id=$1 AND driver_id=$2`, company, driver).Scan(&confirmed))
	if !confirmed {
		t.Fatal("acknowledged identity was not confirmed")
	}
	must(repo.CaptureReview(ctx, actor))
	view, e = repo.View(ctx)
	must(e)
	must(repo.PrepareAssignments(ctx, view.SnapshotID, view.Local.SourceRevision, actor))
	must(repo.Process(ctx))
	must(repo.CaptureReview(ctx, actor))
	view, e = repo.View(ctx)
	must(e)
	if repo.Activate(ctx, view.SnapshotID, view.Local.SourceRevision, view.Version, actor) == nil {
		t.Fatal("activated with reconcile configuration")
	}
	repo.client.Options.Mode = "live"
	must(repo.Activate(ctx, view.SnapshotID, view.Local.SourceRevision, view.Version, actor))
	must(repo.Process(ctx))
	view, e = repo.View(ctx)
	must(e)
	if view.Mode != "live" {
		t.Fatalf("valid readback paused: %s", view.LastError)
	}
	// Ownership update alone must not enqueue or mutate crews.
	_, e = admin.Exec(ctx, `UPDATE trucks SET owner_id=$1 WHERE id=$2`, companyOwner, truck)
	must(e)
	must(repo.Process(ctx))
	must(repo.Process(ctx))
	var assignmentCount int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM fleetscope_sync_operations WHERE kind='assignments.apply'`).Scan(&assignmentCount))
	if assignmentCount != 1 || primary != remoteDriver || projected.Ownership.Kind != "company" {
		t.Fatal("owner update changed crew or did not project")
	}
	// A pause with an automatic operation pending must have a reviewed recovery
	// path, without turning the whole live stream back on prematurely.
	_, e = admin.Exec(ctx, `UPDATE trucks SET unit_number='0001' WHERE id=$1`, truck)
	must(e)
	must(repo.project(ctx, epoch))
	view, e = repo.View(ctx)
	must(e)
	var pendingID string
	must(pool.QueryRow(ctx, `SELECT id::text FROM fleetscope_sync_operations WHERE state='pending' AND kind='truck.upsert'`).Scan(&pendingID))
	if repo.Supersede(ctx, pendingID, actor) == nil {
		t.Fatal("superseded unconfirmed live operation")
	}
	must(repo.Pause(ctx, view.Version, actor))
	must(repo.Retry(ctx, pendingID, actor))
	view, e = repo.View(ctx)
	must(e)
	must(repo.ResumeReview(ctx, view.Version, actor))
	must(repo.Process(ctx))
	if projected.UnitNumber != "0001" {
		t.Fatal("reviewed retry remained stuck after resume")
	}
	must(repo.CaptureReview(ctx, actor))
	view, e = repo.View(ctx)
	must(e)
	must(repo.Activate(ctx, view.SnapshotID, view.Local.SourceRevision, view.Version, actor))
	// External edits are quarantined, never automatically overwritten.
	remoteRevision = "unexpected"
	_, e = admin.Exec(ctx, `UPDATE fleetscope_sync_state SET last_verified_at=NULL`)
	must(e)
	must(repo.Process(ctx))
	view, e = repo.View(ctx)
	must(e)
	if view.Mode != "paused" || view.LastError != "receiver_drift_requires_review" {
		t.Fatalf("drift was not paused: %+v", view)
	}
	oldRequests := requests
	must(repo.Process(ctx))
	if requests != oldRequests {
		t.Fatal("paused worker contacted receiver")
	}
}

func TestDesiredFleetScopeCrews(t *testing.T) {
	d1, d2, t1, t2 := fleetscope.NewID(), fleetscope.NewID(), fleetscope.NewID(), fleetscope.NewID()
	local := FleetSyncLocal{Drivers: []FleetSyncDriver{{ID: d1, Status: "active"}, {ID: d2, Status: "active"}}, Trucks: []FleetSyncTruck{{Truck: fleetscope.TruckProjection{MSERPTruckID: t2, OperationalActive: true}, PrimaryID: d1, SecondaryID: d2}}}
	links := []FleetSyncLink{{Kind: "truck", LocalID: t1, RemoteID: fleetscope.NewID(), AssignmentRevision: "1"}, {Kind: "truck", LocalID: t2, RemoteID: fleetscope.NewID(), AssignmentRevision: "2"}, {Kind: "driver", LocalID: d1, RemoteID: fleetscope.NewID()}, {Kind: "driver", LocalID: d2, RemoteID: fleetscope.NewID()}}
	crews, e := desiredSyncCrews(local, links)
	if e != nil || len(crews) != 2 {
		t.Fatal(crews, e)
	}
	for _, c := range crews {
		if c.MSERPTruckID == t1 && c.Primary != nil {
			t.Fatal("deleted truck retained crew")
		}
		if c.MSERPTruckID == t2 && (c.Primary == nil || c.Secondary == nil) {
			t.Fatal("team lost")
		}
	}
	local.Drivers[1].Status = "terminated"
	if _, e = desiredSyncCrews(local, links); e == nil {
		t.Fatal("terminated team member accepted")
	}
	local.Drivers[1].Status = "active"
	links[1].RemoteID = ""
	if _, e = desiredSyncCrews(local, links); e == nil {
		t.Fatal("nonexistent truck accepted")
	}
}
