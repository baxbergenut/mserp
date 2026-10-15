package fleetscope

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const syncCompany = "11111111-1111-4111-8111-111111111111"
const syncEpoch = "22222222-2222-4222-8222-222222222222"

func TestSyncContractBundle(t *testing.T) {
	var manifest struct {
		WireSchemaSHA256 string `json:"wireSchemaSHA256"`
	}
	if e := json.Unmarshal(Protocol, &manifest); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(WireSchema)
	if hex.EncodeToString(h[:]) != manifest.WireSchemaSHA256 {
		t.Fatal("schema changed without updating the contract manifest")
	}
	var vector struct{ Secret, Method, Target, Timestamp, Nonce, KeyID, Environment, CompanyID, Body, Signature string }
	b, e := os.ReadFile("testdata/signature.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &vector); e != nil {
		t.Fatal(e)
	}
	if got := SyncSignature(vector.Secret, vector.Method, vector.Target, vector.Timestamp, vector.Nonce, vector.KeyID, vector.Environment, vector.CompanyID, []byte(vector.Body)); got != vector.Signature {
		t.Fatalf("golden signature mismatch: %s", got)
	}
}

func TestSyncWireFixtures(t *testing.T) {
	id := "33333333-3333-4333-8333-333333333333"
	remote := "44444444-4444-4444-8444-444444444444"
	owner := "55555555-5555-4555-8555-555555555555"
	vin, makeName, model, plate, state, year := "1FUJGLDR9CSBP0001", "Freightliner", "Cascadia", "ABC123", "IL", 2022
	truck := TruckProjection{MSERPTruckID: id, UnitNumber: "001", VIN: &vin, Year: &year, Make: &makeName, Model: &model, LicensePlate: &plate, LicenseState: &state, OperationalActive: true, OperationalStatus: "available", Ownership: Ownership{Kind: "investor", MSERPOwnerID: owner, DisplayName: "Example Investor"}}
	stamp, _ := time.Parse(time.RFC3339, "2026-10-15T12:00:00Z")
	fixtures := []struct {
		Request  Operation `json:"request"`
		Response Result    `json:"response"`
	}{}
	for _, kind := range []string{"truck.link", "truck.upsert", "driver.link", "assignments.apply", "truck.retire"} {
		op := Operation{Version: 1, OperationID: id, CompanyID: syncCompany, Environment: "staging", Epoch: syncEpoch, OccurredAt: stamp, Kind: kind, SourceRevision: 1, MSERPID: id, FleetScopeID: remote, ExpectedRevision: "truck-r1", SnapshotID: owner}
		result := Result{OperationID: id, Status: "applied", CompanyID: syncCompany, Environment: "staging", Epoch: syncEpoch, FleetScopeID: remote, Revision: "truck-r2", AssignmentRevision: "crew-r1", SourceRevision: 1}
		switch kind {
		case "truck.link":
			op.Truck = &truck
		case "truck.upsert":
			op.Truck = &truck
			op.ExpectedRevision = ""
			op.FleetScopeID = ""
			op.Provisioning = &Provisioning{Color: "White", TruckClass: "Class 8", GVWRLbs: 80000}
		case "driver.link":
			op.ExpectedRevision = "driver-r1"
			result.Revision = "driver-r2"
			result.AssignmentRevision = ""
		case "assignments.apply":
			op.MSERPID = ""
			op.FleetScopeID = ""
			op.ExpectedRevision = ""
			op.SourceRevision = 0
			op.AssignmentSequence = 1
			op.Crews = []Crew{{MSERPTruckID: id, FleetScopeVehicleID: remote, ExpectedRevision: "crew-r1", Primary: &DriverRef{MSERPDriverID: owner, FleetScopeDriverID: syncEpoch}}}
			result.SourceRevision = 0
			result.AssignmentSequence = 1
			result.FleetScopeID = ""
			result.Revision = ""
			result.AssignmentRevision = ""
			result.Crews = append([]Crew{}, op.Crews...)
			result.Crews[0].ExpectedRevision = "crew-r2"
		case "truck.retire":
			op.SnapshotID = ""
		}
		fixtures = append(fixtures, struct {
			Request  Operation `json:"request"`
			Response Result    `json:"response"`
		}{op, result})
	}
	b, e := json.MarshalIndent(fixtures, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	b = append(b, '\n')
	if os.Getenv("UPDATE_FLEETSCOPE_FIXTURES") == "1" {
		if e = os.WriteFile("testdata/operations.json", b, 0644); e != nil {
			t.Fatal(e)
		}
	}
	saved, e := os.ReadFile("testdata/operations.json")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(saved, b) {
		t.Fatal("wire fixtures differ: review contract changes before UPDATE_FLEETSCOPE_FIXTURES=1")
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if e = json.Unmarshal(WireSchema, &schema); e != nil {
		t.Fatal(e)
	}
	for _, v := range []any{Operation{}, Result{}, Capabilities{}, Snapshot{}, RemoteTruck{}, RemoteDriver{}, Ownership{}, TruckProjection{}, Provisioning{}, Crew{}, DriverRef{}} {
		typ := reflect.TypeOf(v)
		props := schema.Defs[typ.Name()].Properties
		if len(props) != typ.NumField() {
			t.Fatalf("schema field count differs: %s", typ.Name())
		}
		for i := 0; i < typ.NumField(); i++ {
			key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if _, ok := props[key]; !ok {
				t.Fatalf("missing schema field %s.%s", typ.Name(), key)
			}
		}
	}
}

func TestSyncConfiguration(t *testing.T) {
	if err := (SyncOptions{}).Validate(); err != nil {
		t.Fatal(err)
	}
	good := SyncOptions{Mode: "reconcile", URL: "https://fleet.example/api/integrations/mserp/v1", CompanyID: syncCompany, Environment: "staging", KeyID: "live-1", Secret: strings.Repeat("a", 32), BootstrapKeyID: "review-1", BootstrapSecret: strings.Repeat("b", 32)}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://fleet.example/integrations/mserp/v1", "https://x:password@fleet.example/integrations/mserp/v1", "https://fleet.example/integrations/mserp/v1?x=1", "https://fleet.example/integrations/mserp/v1/", "https://fleet.example/elsewhere"} {
		o := good
		o.URL = bad
		if o.Validate() == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	o := good
	o.BootstrapSecret = o.Secret
	if o.Validate() == nil {
		t.Fatal("accepted shared secret")
	}
	o = good
	o.CompanyID = "MS Express"
	if o.Validate() == nil {
		t.Fatal("accepted company name")
	}
	o = good
	o.Environment = ""
	if o.Validate() == nil {
		t.Fatal("accepted missing environment")
	}
}

func TestOwnershipSemantics(t *testing.T) {
	driver := NewID()
	o := Ownership{Kind: "investor", MSERPOwnerID: NewID(), DisplayName: "Independent Owner"}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	o.Kind = "lease"
	if o.Validate() == nil {
		t.Fatal("lease is not ownership")
	}
	o.Kind = "driver"
	if o.Validate() == nil {
		t.Fatal("missing owner driver accepted")
	}
	o.MSERPOwnerDriverID = &driver
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	o.Kind = "company"
	if o.Validate() == nil {
		t.Fatal("company with driver accepted")
	}
}

func TestSignedSyncClient(t *testing.T) {
	opts := SyncOptions{Mode: "reconcile", CompanyID: syncCompany, Environment: "staging", KeyID: "live-1", Secret: strings.Repeat("a", 32), BootstrapKeyID: "review-1", BootstrapSecret: strings.Repeat("b", 32)}
	var seen []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key := r.Header.Get("X-MSERP-Key-ID")
		secret := opts.Secret
		if key == opts.BootstrapKeyID {
			secret = opts.BootstrapSecret
		}
		expected := SyncSignature(secret, r.Method, r.URL.RequestURI(), r.Header.Get("X-MSERP-Timestamp"), r.Header.Get("X-MSERP-Nonce"), key, opts.Environment, opts.CompanyID, body)
		if r.Header.Get("X-MSERP-Signature") != expected || r.Header.Get("X-MSERP-Contract") != ContractHash() {
			t.Error("invalid signed request")
		}
		seen = append(seen, r.Header.Get("X-MSERP-Nonce"))
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "capabilities") {
			json.NewEncoder(w).Encode(Capabilities{1, ContractHash(), OwnershipCapability, syncCompany, "staging", syncEpoch, "reconcile"})
			return
		}
		var op Operation
		json.Unmarshal(body, &op)
		if key != opts.BootstrapKeyID {
			t.Error("bootstrap key not used")
		}
		json.NewEncoder(w).Encode(Result{OperationID: op.OperationID, Status: "duplicate", CompanyID: op.CompanyID, Environment: op.Environment, Epoch: op.Epoch, SourceRevision: op.SourceRevision})
	}))
	defer server.Close()
	opts.URL = server.URL + "/integrations/mserp/v1"
	client := NewSyncClient(opts)
	client.HTTP = server.Client()
	if _, e := client.Capabilities(context.Background()); e != nil {
		t.Fatal(e)
	}
	op := Operation{Version: 1, Kind: "truck.upsert", OperationID: NewID(), CompanyID: syncCompany, Environment: "staging", Epoch: syncEpoch, SourceRevision: 1}
	body, _ := json.Marshal(op)
	for i := 0; i < 2; i++ {
		if _, e := client.Deliver(context.Background(), op, body, true); e != nil {
			t.Fatal(e)
		}
	}
	if seen[1] == seen[2] {
		t.Fatal("replay must get a fresh nonce")
	}
	altered := append([]byte{}, body...)
	altered = append(altered, ' ')
	if SyncSignature(opts.Secret, "POST", "/x", "1", syncEpoch, "key", "staging", syncCompany, body) == SyncSignature(opts.Secret, "POST", "/x", "1", syncEpoch, "key", "staging", syncCompany, altered) {
		t.Fatal("signature ignored raw bytes")
	}
}

func TestSyncRejectsWrongScopeAndBadSnapshots(t *testing.T) {
	mode := "scope"
	id := NewID()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "scope" {
			json.NewEncoder(w).Encode(Capabilities{1, ContractHash(), OwnershipCapability, NewID(), "staging", syncEpoch, "live"})
			return
		}
		snapshot := Snapshot{ID: id, CompanyID: syncCompany, Environment: "staging", Epoch: syncEpoch, CapturedAt: time.Now().UTC(), Drivers: []RemoteDriver{}, Trucks: []RemoteTruck{}}
		if mode == "duplicate" {
			snapshot.Drivers = []RemoteDriver{{ID: id, Revision: "1"}, {ID: id, Revision: "1"}}
		}
		if mode == "stale" {
			snapshot.CapturedAt = time.Now().Add(-time.Hour)
		}
		if mode == "missing-driver" {
			snapshot.Trucks = []RemoteTruck{{ID: NewID(), Revision: "1", AssignmentRevision: "1", PrimaryID: NewID()}}
		}
		json.NewEncoder(w).Encode(snapshot)
	}))
	defer srv.Close()
	c := NewSyncClient(SyncOptions{Mode: "live", URL: srv.URL, CompanyID: syncCompany, Environment: "staging", KeyID: "key", Secret: strings.Repeat("x", 32)})
	c.HTTP = srv.Client()
	if _, err := c.Capabilities(context.Background()); err == nil {
		t.Fatal("wrong tenant accepted")
	}
	for _, m := range []string{"duplicate", "stale", "missing-driver"} {
		mode = m
		if _, err := c.Snapshot(context.Background()); err == nil {
			t.Fatalf("accepted %s", m)
		}
	}
}

func TestSyncRejectsRedirectAndQueuedReceipt(t *testing.T) {
	status := 302
	followed := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed = true
		}
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(status)
		io.WriteString(w, `{"code":"test_error"}`)
	}))
	defer srv.Close()
	c := NewSyncClient(SyncOptions{Mode: "live", URL: srv.URL, CompanyID: syncCompany, Environment: "staging", KeyID: "test", Secret: strings.Repeat("x", 32)})
	redirect := c.HTTP.CheckRedirect
	c.HTTP = srv.Client()
	c.HTTP.CheckRedirect = redirect
	for _, s := range []int{302, 202} {
		status = s
		if _, e := c.Capabilities(context.Background()); e == nil {
			t.Fatalf("accepted HTTP %d", s)
		}
	}
	if followed {
		t.Fatal("sent signed credentials through redirect")
	}
	op := Operation{Kind: "driver.link", CompanyID: NewID(), Environment: "staging"}
	if _, e := c.Deliver(context.Background(), op, []byte("{}"), false); e == nil {
		t.Fatal("wrong-company operation accepted")
	}
}
