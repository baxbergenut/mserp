package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/repository"
	"mserp/internal/weighmytruck"
)

func TestWeighMyTruckDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_WMT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_WMT_TEST_DATABASE_URL to a local disposable _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("local disposable test database required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("wmt_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			execute := func(sql string, args ...any) {
				t.Helper()
				if _, err := admin.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			execute(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() { admin.Exec(ctx, `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`) }()
			if mode == "fresh" {
				execute(string(source))
			} else {
				before, _, ok := strings.Cut(string(source), "-- WeighMyTruck (migration 073).")
				if !ok {
					t.Fatal("missing boundary")
				}
				execute(before)
				m, e := os.ReadFile("../../sql/073_weighmytruck.sql")
				if e != nil {
					t.Fatal(e)
				}
				execute(string(m))
			}
			execute(`GRANT SELECT,UPDATE ON drivers TO mserp_app`)
			pc := cfg.Copy()
			pc.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			pc.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, pc)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			ids := []string{"07300000-0000-0000-0000-000000000001", "07300000-0000-0000-0000-000000000002", "07300000-0000-0000-0000-000000000003"}
			for i, name := range []string{"Added Driver", "Available Driver", "Terminated Driver"} {
				execute(`INSERT INTO drivers(id,full_name,normalized_name,email,phone,pay_type,pay_rate) VALUES($1,$2,$3,$4,$5,'cpm',0)`, ids[i], name, strings.ToLower(name), fmt.Sprintf("driver%d@example.com", i), fmt.Sprintf("555123000%d", i))
			}
			execute(`UPDATE drivers SET status='terminated',termination_date=current_date WHERE id=$1`, ids[2])
			var calls atomic.Int32
			var response atomic.Int32
			response.Store(200)
			var removedEmail string
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					w.Write([]byte(`{"access_token":"fixture-token","expires_in":3600}`))
					return
				}
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("missing bearer")
				}
				if strings.HasSuffix(r.URL.Path, "RemoveDriver") {
					if json.NewDecoder(r.Body).Decode(&removedEmail) != nil {
						t.Error("invalid removal shape")
					}
				}
				w.WriteHeader(int(response.Load()))
			}))
			defer provider.Close()
			repo := repository.NewWeighMyTruckRepository(pool, weighmytruck.NewClient(weighmytruck.Options{ClientID: "id", ClientSecret: "secret", TokenURL: provider.URL + "/token", APIURL: provider.URL, Scope: "scope", CompanyName: "Fixture"}))
			csv := "First Name,Last Name,Email,Phone,Driver Code\nAdded,Driver,driver0@example.com,5551230000,007\nTerminated,Driver,driver2@example.com,5551230002,\nUnmatched,Driver,unmatched@example.com,5551230099,\n"
			imported, err := repo.ImportWeighMyTruck(ctx, strings.NewReader(csv))
			if err != nil || imported.Total != 3 || imported.Linked != 2 || imported.Unlinked != 1 {
				t.Fatalf("import: %+v %v", imported, err)
			}
			again, err := repo.ImportWeighMyTruck(ctx, strings.NewReader(csv))
			if err != nil || !again.AlreadyImported {
				t.Fatal("import must be idempotent")
			}
			if _, err = repo.ImportWeighMyTruck(ctx, strings.NewReader(csv+"New,Driver,new@example.com,5551230098,\n")); err == nil {
				t.Fatal("import overwrote initialized tracker")
			}
			permissions := []string{"weighmytruck.read", "weighmytruck.write"}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			auth := newAuthHandler(logger, nil, AuthOptions{})
			router := chi.NewRouter()
			router.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					session := repository.AuthSession{User: repository.AuthUser{ID: "tester", Username: "Test user", Permissions: permissions}, CSRFToken: "fixture-csrf"}
					next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, session)))
				})
			})
			router.Use(auth.requireCSRF, requirePermission)
			registerWeighMyTruckRoutes(router, logger, repo)
			router.Get("/auth/session", auth.session)
			request := func(method, path, body, csrf string, status int) {
				t.Helper()
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("X-CSRF-Token", csrf)
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				if res.Code != status {
					t.Fatalf("%s %s: got %d want %d: %s", method, path, res.Code, status, res.Body.String())
				}
			}
			list := func() repository.WMTList {
				t.Helper()
				v, e := repo.List(ctx)
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			find := func(driver string) repository.WMTEntry {
				t.Helper()
				for _, e := range list().Items {
					if e.DriverID == driver {
						return e
					}
				}
				t.Fatal("missing driver")
				return repository.WMTEntry{}
			}
			change := func(e repository.WMTEntry, add bool, status int) {
				t.Helper()
				b, _ := json.Marshal(repository.WMTChange{DriverID: e.DriverID, MembershipID: e.ID, Version: e.Version, Add: add})
				request("POST", "/weighmytruck/change", string(b), "fixture-csrf", status)
			}
			if find(ids[2]).Warning == "" {
				t.Fatal("terminated enrollment needs warning")
			}
			request("POST", "/weighmytruck/change", `{}`, "", 403)
			permissions = []string{"weighmytruck.read"}
			request("POST", "/weighmytruck/change", `{}`, "fixture-csrf", 403)
			permissions = []string{}
			request("GET", "/weighmytruck", "", "", 403)
			permissions = []string{"weighmytruck.read", "weighmytruck.write"}
			available := find(ids[1])
			change(available, true, 200)
			change(available, true, 409)
			if calls.Load() != 1 {
				t.Fatal("duplicate add reached provider")
			}
			execute(`UPDATE drivers SET email='changed@example.com' WHERE id=$1`, ids[1])
			change(find(ids[1]), false, 200)
			if removedEmail != "driver1@example.com" {
				t.Fatal("removed wrong email")
			}
			change(find(ids[2]), true, 400)
			response.Store(500)
			change(find(ids[1]), true, 502)
			uncertain := find(ids[1])
			if uncertain.State != "review" {
				t.Fatal("uncertain result lost")
			}
			before := calls.Load()
			change(uncertain, true, 409)
			if calls.Load() != before {
				t.Fatal("uncertain mutation replayed")
			}
			verify, _ := json.Marshal(repository.WMTVerification{Version: uncertain.Version, Enrolled: false, Reason: "Verified absent on fleet website"})
			request("POST", "/weighmytruck/"+uncertain.ID+"/verify", string(verify), "fixture-csrf", 200)
			response.Store(401)
			change(find(ids[1]), true, 502)
			if find(ids[1]).State != "confirmed" || find(ids[1]).Enrolled {
				t.Fatal("auth failure changed membership")
			}
			response.Store(200)
			// Crash recovery stays blocked until the bounded network operation has expired.
			execute(`UPDATE weighmytruck_memberships SET state='pending' WHERE driver_id=$1`, ids[1])
			pending := find(ids[1])
			if pending.CanVerify {
				t.Fatal("active operation can be verified")
			}
			verify, _ = json.Marshal(repository.WMTVerification{Version: pending.Version, Reason: "premature"})
			request("POST", "/weighmytruck/"+pending.ID+"/verify", string(verify), "fixture-csrf", 409)
			execute(`UPDATE weighmytruck_memberships SET updated_at=now()-interval '3 minutes' WHERE driver_id=$1`, ids[1])
			request("POST", "/weighmytruck/"+pending.ID+"/verify", string(verify), "fixture-csrf", 200)
			if mode == "fresh" && os.Getenv("MSERP_WMT_BROWSER_TEST") == "1" {
				// The add picker must exclude non-active drivers even when unadded.
				execute(`INSERT INTO drivers(full_name,normalized_name,email,phone,pay_type,pay_rate,status,termination_date)
 VALUES('Home Driver','home driver','home@example.com','5551230081','cpm',0,'home',NULL),
 ('Vacation Driver','vacation driver','vacation@example.com','5551230082','cpm',0,'vacation',NULL),
 ('Former Driver','former driver','former@example.com','5551230083','cpm',0,'terminated',current_date)`)
				server := httptest.NewServer(router)
				defer server.Close()
				cmd := exec.Command("node", "../../../frontend/scripts/test-weighmytruck-e2e.mjs")
				cmd.Env = append(os.Environ(), "MSERP_WMT_TEST_API="+server.URL)
				out, e := cmd.CombinedOutput()
				if e != nil {
					t.Fatalf("browser: %v\n%s", e, out)
				}
				t.Log(string(out))
			}
		})
	}
}
