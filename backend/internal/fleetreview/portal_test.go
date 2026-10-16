package fleetreview

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthenticationAndCSRF(t *testing.T) {
	dir := t.TempDir()
	if e := writePrivate(filepath.Join(dir, "state.json"), State{Version: 1, Rows: []Row{}}); e != nil {
		t.Fatal(e)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-only-strong-password"), bcrypt.MinCost)
	s, e := NewServer(context.Background(), nil, Config{Origin: "https://conflicts.example.test", Directory: dir, Username: "reviewer", PasswordHash: string(hash), Secure: true})
	if e != nil {
		t.Fatal(e)
	}
	h := s.Handler()
	request := func(method, path, body, origin, csrf string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/state", "", "", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	body := `{"username":"reviewer","password":"test-only-strong-password"}`
	if w := request("POST", "/api/login", body, "https://attacker.example", "", nil); w.Code != 403 {
		t.Fatal("foreign login origin accepted")
	}
	w := request("POST", "/api/login", body, "https://conflicts.example.test", "", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Domain != "" {
		t.Fatal("unsafe cookie")
	}
	var auth map[string]string
	json.Unmarshal(w.Body.Bytes(), &auth)
	if w := request("POST", "/api/logout", "{}", "https://conflicts.example.test", "", cookies[0]); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if w := request("GET", "/api/state", "", "", "", cookies[0]); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private state response", w.Code)
	}
	if w := request("POST", "/api/logout", "{}", "https://conflicts.example.test", auth["csrf"], cookies[0]); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/api/state", "", "", "", cookies[0]); w.Code != 401 {
		t.Fatal("revoked session accepted")
	}
	for i := 0; i < 10; i++ {
		request("POST", "/api/login", `{"username":"reviewer","password":"wrong"}`, "https://conflicts.example.test", "", nil)
	}
	if w := request("POST", "/api/login", body, "https://conflicts.example.test", "", nil); w.Code != 429 {
		t.Fatal("login throttle missing")
	}
}
func TestMatchingNeverConfirmsIdentity(t *testing.T) {
	source := Snapshot{Drivers: []Record{{"id": "a", "name": "Jane Mary Doe", "status": "active", "email": "j@example.test"}, {"id": "b", "name": "Other Person", "email": "j@example.test", "status": "terminated"}}, Trucks: []Record{}}
	remote := Snapshot{Drivers: []Record{{"id": "r", "name": "Doe Jane", "firstName": "Jane", "lastName": "Doe", "email": "j@example.test"}}, Trucks: []Record{}}
	p := Policy{RejectedPairs: [][2]string{{"b", "r"}}}
	s := initial(source, remote, p)
	if s.Rows[0].RemoteID != "r" || s.Rows[0].Reviewed {
		t.Fatal("candidate incorrectly confirmed")
	}
	if s.Rows[0].Values["firstName"] != "" {
		t.Fatal("silently shortened name")
	}
	if s.Rows[1].RemoteID != "" || s.Rows[1].Action != "defer" {
		t.Fatal("rejected identity reused")
	}
}

func TestInterruptedBatchRemainsRecoverableAfterRestart(t *testing.T) {
	dir := t.TempDir()
	batch := &Batch{ID: id(), Hash: "frozen-plan", State: "applying"}
	if err := writePrivate(filepath.Join(dir, "state.json"), State{Version: 5, Batch: batch}); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-only-strong-password"), bcrypt.MinCost)
	s, err := NewServer(context.Background(), nil, Config{Origin: "https://conflicts.example.test", Directory: dir, Username: "reviewer", PasswordHash: string(hash), Secure: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.applying || s.state.Batch.State != "needs_attention" || s.state.Batch.ID != batch.ID || s.state.Batch.Hash != batch.Hash || s.state.Version != 6 {
		t.Fatal("interrupted batch was not preserved for explicit recovery")
	}
	var saved State
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &saved); err != nil || saved.Batch.State != "needs_attention" {
		t.Fatal("recovery state not durable", err)
	}
}
func testPool(t *testing.T, key string) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		t.Skip("requires two disposable portal test databases")
	}
	u, e := url.Parse(raw)
	if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || !strings.HasSuffix(u.Path, "_portal_test") {
		t.Fatal("refusing a non-local/non-test database")
	}
	p, e := pgxpool.New(context.Background(), raw)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	return p
}
func TestDatabaseBatchRecoveryAndFences(t *testing.T) {
	ctx := context.Background()
	a := testPool(t, "MSERP_PORTAL_TEST_DATABASE_URL")
	b := testPool(t, "FLEETSCOPE_PORTAL_TEST_DATABASE_URL")
	company := id()
	d := &DB{Source: a, Receiver: b, CompanyID: company, CompanyName: "Portal Test", DOT: company[:8]}
	exec := func(p *pgxpool.Pool, q string, args ...any) {
		t.Helper()
		if _, e := p.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []*pgxpool.Pool{a, b} {
		exec(p, ReceiptSchema)
	}
	exec(b, `INSERT INTO companies(id,name,dot_number,email,phone_number,address) VALUES($1,'Portal Test',$2,'test@example.test','5555550100','Test')`, company, d.DOT)
	local, remote, truck, remoteTruck, investor := id(), id(), id(), id(), id()
	vin := "1FUJGLDR9CS" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	exec(a, `INSERT INTO drivers(id,full_name,normalized_name,email,phone,pay_type,pay_rate) VALUES($1,'Jane Doe '||$2,'jane doe '||$2,'old@example.test','5555550100','cpm',0.5)`, local, local[:8])
	exec(a, `INSERT INTO investors(id,full_name) VALUES($1,'Test Investor')`, investor)
	exec(a, `INSERT INTO trucks(id,unit_number,vin,year,make,model,license_plate,license_state) VALUES($1,$2,$3,2022,'Freightliner','Cascadia',$4,'NY')`, truck, "P"+truck[:8], vin, truck[:8])
	exec(b, `INSERT INTO drivers(id,company_id,first_name,last_name,email,phone_number,ssn,ssn_last4,ssn_digest,address,status) VALUES($1,$2,'Jane','Doe','old@example.test','5555550100','','','','','active')`, remote, company)
	exec(b, `INSERT INTO vehicles(id,company_id,unit_number,vin,year,make,model,license_plate,license_plate_state,color,truck_class,gvwr_lbs,ownership,status) VALUES($1,$2,$3,$4,2022,'Freightliner','Cascadia',$5,'NY','White','8',80000,'company','active')`, remoteTruck, company, "P"+truck[:8], vin, truck[:8])
	// Exercise production-style permissions, not only an administrator connection.
	role := "portal_test_" + strings.ReplaceAll(id(), "-", "")
	exec(a, `CREATE ROLE `+role+` NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE`)
	exec(a, `GRANT USAGE ON SCHEMA public,fleet_review TO `+role+`; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO `+role+`; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO `+role+`; GRANT SELECT,INSERT,UPDATE ON fleet_review.receipts TO `+role)
	exec(b, `GRANT USAGE ON SCHEMA public,fleet_review TO `+role+`; GRANT SELECT ON companies,drivers,vehicles,fleet_assignments,mserp_sync_state,mserp_sync_links,insurance_coverage_vehicles,insurance_policy_coverages,insurance_policies,files,trailers,cdls,medcards,mvrs,clearinghouse_queries,driver_application_documents,applicants,recruitment_processes,drug_test_orders,previous_employer_verifications,previous_employers,driver_consent_submissions,driver_compliance_stats TO `+role+`; GRANT INSERT,UPDATE ON drivers,vehicles,fleet_assignments,driver_compliance_stats TO `+role+`; GRANT UPDATE ON mserp_sync_state,mserp_sync_links TO `+role+`; GRANT SELECT,INSERT,UPDATE ON fleet_review.receipts TO `+role+`; GRANT INSERT ON entity_activities,mserp_sync_audit TO `+role+`; GRANT USAGE,SELECT ON SEQUENCE mserp_sync_audit_id_seq TO `+role)
	runtimePool := func(admin *pgxpool.Pool) *pgxpool.Pool {
		cfg := admin.Config().Copy()
		cfg.ConnConfig.RuntimeParams["role"] = role
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	d.Source, d.Receiver = runtimePool(a), runtimePool(b)
	t.Cleanup(func() {
		d.Source.Close()
		d.Receiver.Close()
		for _, p := range []*pgxpool.Pool{a, b} {
			if _, err := p.Exec(ctx, `DROP OWNED BY `+role); err != nil {
				t.Error(err)
			}
		}
		if _, err := a.Exec(ctx, `DROP ROLE `+role); err != nil {
			t.Error(err)
		}
	})
	makeState := func() State {
		t.Helper()
		x, y, e := d.Snapshot(ctx)
		if e != nil {
			t.Fatal(e)
		}
		s := initial(x, y, Policy{})
		for i := range s.Rows {
			r := &s.Rows[i]
			r.Reviewed = true
			r.Action = "defer"
			r.Note = "Unrelated test record"
			if r.LocalID == local {
				r.Action = "update"
				r.RemoteID = remote
				r.Values["firstName"] = "Jane"
				r.Values["lastName"] = "Doe " + local[:8]
				r.Values["email"] = "reviewed@example.test"
			}
			if r.LocalID == truck {
				r.Action = "update"
				r.RemoteID = remoteTruck
				r.Values["ownerId"] = investor
				r.Values["ownerDate"] = func() string {
					loc, _ := time.LoadLocation("America/New_York")
					return time.Now().In(loc).Format(time.DateOnly)
				}()
			}
		}
		s.Batch = &Batch{ID: id(), Hash: batchHash(s), State: "applying"}
		return s
	}
	s := makeState()
	if e := d.Apply(ctx, s, "test-reviewer"); e != nil {
		t.Fatal(e)
	}
	var name string
	var managed, hold bool
	if e := b.QueryRow(ctx, `SELECT mserp_owner_snapshot->>'displayName',mserp_managed,mserp_coverage_review FROM vehicles WHERE id=$1`, remoteTruck).Scan(&name, &managed, &hold); e != nil || name != "Test Investor" || !managed || !hold {
		t.Fatal("ownership/evidence projection", name, managed, hold, e)
	}
	if e := d.Apply(ctx, s, "test-reviewer"); e != nil {
		t.Fatal("receipt replay", e)
	}
	var receipts int
	b.QueryRow(ctx, `SELECT count(*) FROM fleet_review.receipts WHERE batch_id=$1`, s.Batch.ID).Scan(&receipts)
	if receipts != 1 {
		t.Fatal("duplicate receipt")
	}
	stale := makeState()
	exec(a, `UPDATE drivers SET phone='5555550111',updated_at=now() WHERE id=$1`, local)
	if e := d.Apply(ctx, stale, "test-reviewer"); e == nil || !strings.Contains(e.Error(), "records changed") {
		t.Fatal("stale source accepted", e)
	}
	partial := makeState()
	for i := range partial.Rows {
		if partial.Rows[i].LocalID == local {
			partial.Rows[i].Values["email"] = "recover@example.test"
		}
	}
	partial.Batch.Hash = batchHash(partial)
	exec(b, `CREATE OR REPLACE FUNCTION fleet_review.fail_commit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated receiver commit failure'; END $$; CREATE CONSTRAINT TRIGGER fail_commit_test AFTER INSERT ON fleet_review.receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fleet_review.fail_commit_test()`)
	e := d.Apply(ctx, partial, "test-reviewer")
	exec(b, `DROP TRIGGER fail_commit_test ON fleet_review.receipts; DROP FUNCTION fleet_review.fail_commit_test()`)
	if e == nil {
		t.Fatal("commit failure not surfaced")
	}
	a.QueryRow(ctx, `SELECT count(*) FROM fleet_review.receipts WHERE batch_id=$1`, partial.Batch.ID).Scan(&receipts)
	if receipts != 1 {
		t.Fatal("source commit missing", e)
	}
	b.QueryRow(ctx, `SELECT count(*) FROM fleet_review.receipts WHERE batch_id=$1`, partial.Batch.ID).Scan(&receipts)
	if receipts != 0 {
		t.Fatal("receiver should roll back")
	}
	if e = d.Apply(ctx, partial, "test-reviewer"); e != nil {
		t.Fatal("partial recovery failed", e)
	}
	var email string
	b.QueryRow(ctx, `SELECT email FROM drivers WHERE id=$1`, remote).Scan(&email)
	if email != "recover@example.test" {
		t.Fatal("receiver not recovered")
	}
	wrong := *d
	wrong.DOT = "different"
	if _, _, e = wrong.Snapshot(ctx); e == nil {
		t.Fatal("company identity mismatch accepted")
	}
	duplicate := makeState()
	duplicate.Rows = append(duplicate.Rows, duplicate.Rows[0])
	if e = validateBatch(duplicate); e == nil {
		t.Fatal("duplicate selection accepted")
	}
	newDriver, newTruck := id(), id()
	exec(a, `INSERT INTO drivers(id,full_name,normalized_name,email,phone,pay_type,pay_rate) VALUES($1,'New '||$2,'new '||$2,'new@example.test','5555550123','cpm',0.5)`, newDriver, newDriver[:8])
	newVIN := "1FUJGLDR9CS" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	exec(a, `INSERT INTO trucks(id,unit_number,vin,year,make,model,license_plate,license_state) VALUES($1,$2,$3,2022,'Freightliner','Cascadia',$4,'NY')`, newTruck, "P"+newTruck[:8], newVIN, newTruck[:8])
	created := makeState()
	for i := range created.Rows {
		r := &created.Rows[i]
		if r.LocalID == newDriver {
			r.Action, r.RemoteID = "create", ""
			r.Values["firstName"], r.Values["lastName"] = "New", newDriver[:8]
		}
		if r.LocalID == newTruck {
			r.Action, r.RemoteID = "create", ""
			r.Values["color"], r.Values["truckClass"], r.Values["gvwr"] = "White", "8", "80000"
		}
	}
	created.Batch.Hash = batchHash(created)
	if e = d.Apply(ctx, created, "test-reviewer"); e != nil {
		t.Fatal("create missing records", e)
	}
	if e = d.Apply(ctx, created, "test-reviewer"); e != nil {
		t.Fatal("replay created records", e)
	}
	var count int
	if e = b.QueryRow(ctx, `SELECT count(*) FROM drivers WHERE company_id=$1 AND first_name='New' AND last_name=$2`, company, newDriver[:8]).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate/missing created driver", count, e)
	}
	if e = b.QueryRow(ctx, `SELECT count(*) FROM vehicles WHERE company_id=$1 AND vin=$2`, company, newVIN).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate/missing created truck", count, e)
	}
	if _, e = d.Receiver.Exec(ctx, `UPDATE fleet_review.receipts SET actor='changed' WHERE batch_id=$1`, created.Batch.ID); e == nil {
		t.Fatal("receipt mutation permitted")
	}
	var newRemoteDriver string
	if e = b.QueryRow(ctx, `SELECT identity_links->>$2 FROM fleet_review.receipts WHERE batch_id=$1`, created.Batch.ID, "driver:"+newDriver).Scan(&newRemoteDriver); e != nil {
		t.Fatal(e)
	}
	trailer := id()
	exec(b, `INSERT INTO trailers(id,company_id,vin,license_plate,license_plate_state,unit_number,color,make,model,year) VALUES($1,$2,$3,$4,'NY',$4,'White','Utility','Dry van',2022)`, trailer, company, "1UYVS2538BU"+fmt.Sprintf("%06d", time.Now().UnixNano()%1000000), trailer[:8])
	tx, e := b.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `SELECT set_config('mserp.sync_write','on',true)`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE vehicles SET mserp_coverage_review=false WHERE id=$1`, remoteTruck); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO fleet_assignments(company_id,vehicle_id,driver_id,team_driver_id,trailer_id) VALUES($1,$2,$3,$4,$5)`, company, remoteTruck, remote, newRemoteDriver, trailer); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	terminated := makeState()
	for i := range terminated.Rows {
		if terminated.Rows[i].LocalID == local {
			terminated.Rows[i].Values["status"] = "terminated"
			terminated.Rows[i].Values["terminationDate"] = time.Now().UTC().Format(time.DateOnly)
		}
	}
	terminated.Batch.Hash = batchHash(terminated)
	if e = d.Apply(ctx, terminated, "test-reviewer"); e != nil {
		t.Fatal("termination and crew release", e)
	}
	var remaining, retained string
	if e = b.QueryRow(ctx, `SELECT driver_id::text,trailer_id::text FROM fleet_assignments WHERE company_id=$1 AND vehicle_id=$2 AND ended_at IS NULL`, company, remoteTruck).Scan(&remaining, &retained); e != nil || remaining != newRemoteDriver || retained != trailer {
		t.Fatal("crew partner/trailer not preserved", remaining, retained, e)
	}
	exec(b, `INSERT INTO mserp_sync_state(company_id,environment,epoch,review_id,operator_reason) VALUES($1,'production',$2,$3,'test enrollment')`, company, id(), id())
	if e = d.CheckApplyReady(ctx); e == nil {
		t.Fatal("off but enrolled receiver accepted")
	}
}
