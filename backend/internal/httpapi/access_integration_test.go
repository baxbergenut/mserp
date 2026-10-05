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
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"mserp/internal/repository"
)

func TestAccessDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ACCESS_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("a disposable local _test database is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.Close(ctx)
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("access_test_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, query, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() {
				_, _ = admin.Exec(context.Background(), `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			}()
			source, e := os.ReadFile("../../sql/init.sql")
			if e != nil {
				t.Fatal(e)
			}
			password := "correct horse battery staple"
			hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
			if mode == "migration" {
				before, _, ok := strings.Cut(string(source), "-- Access control (migration 044).")
				if !ok {
					t.Fatal("missing migration boundary")
				}
				exec(before + "COMMIT;")
				exec(`INSERT INTO app_users(username,password_hash) VALUES('legacy',$1)`, string(hash))
				exec(`INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) SELECT id,repeat('a',64),repeat('b',43),now()+interval '1 day' FROM app_users`)
				m, e := os.ReadFile("../../sql/044_access_control.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(m))
				var count int
				if e = admin.QueryRow(ctx, `SELECT count(*) FROM auth_sessions`).Scan(&count); e != nil || count != 0 {
					t.Fatal("legacy sessions survived migration")
				}
			} else {
				exec(string(source))
			}
			// Grant legacy tables only; new runtime tables must belong to mserp_app.
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON app_users,auth_sessions TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			repo := repository.NewAuthRepository(pool)
			var id, roleID string
			if e = admin.QueryRow(ctx, `SELECT id::text FROM app_roles WHERE system_role`).Scan(&roleID); e != nil {
				t.Fatal(e)
			}
			if e = admin.QueryRow(ctx, `INSERT INTO app_users(username,email,role_id,password_hash) VALUES('Admin','admin@example.com',$1,$2) RETURNING id::text`, roleID, string(hash)).Scan(&id); e != nil {
				t.Fatal(e)
			}
			h := newAuthHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), repo, AuthOptions{SessionTTL: time.Hour, CookieSecure: true})
			router := chi.NewRouter()
			router.Post("/auth/login", h.login)
			router.Group(func(r chi.Router) {
				r.Use(h.requireSession, h.requireCSRF, requirePermission)
				registerAccessRoutes(r, h, repo)
				r.Get("/auth/session", h.session)
				r.Post("/auth/logout", h.logout)
				r.Post("/auth/password", h.changePassword)
				r.Get("/loads", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
				r.Post("/jobs/sync-loads", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
			})
			call := func(method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
				t.Helper()
				b, _ := json.Marshal(body)
				req := httptest.NewRequest(method, path, strings.NewReader(string(b)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-CSRF-Token", csrf)
				if cookie != nil {
					req.AddCookie(cookie)
				}
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				return res
			}
			status := func(res *httptest.ResponseRecorder, want int) {
				t.Helper()
				if res.Code != want {
					t.Fatalf("status %d, want %d: %s", res.Code, want, res.Body.String())
				}
			}
			login := func(email, pass string, trust bool) *httptest.ResponseRecorder {
				return call("POST", "/auth/login", map[string]any{"email": email, "password": pass, "trustDevice": trust}, nil, "")
			}
			if mode == "migration" {
				legacy := login("legacy", password, false)
				status(legacy, 200)
				exec(`UPDATE app_users SET active=false WHERE username='legacy'`)
			}
			signed := login("ADMIN@example.com", password, false)
			status(signed, 200)
			var session sessionResponse
			_ = json.Unmarshal(signed.Body.Bytes(), &session)
			sessionCookie := signed.Result().Cookies()[0]
			if !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteStrictMode || sessionCookie.MaxAge > 3600 {
				t.Fatal("insecure/incorrect normal session cookie")
			}
			status(login("Admin", password, false), 401) // No username fallback once email is set.
			remembered := login("admin@example.com", password, true)
			status(remembered, 200)
			if age := remembered.Result().Cookies()[0].MaxAge; age < 2591998 || age > 2592000 {
				t.Fatal("remembered session is not 30 days")
			}
			status(call("GET", "/loads", nil, nil, ""), 401)
			status(call("GET", "/settings/access", nil, sessionCookie, ""), 200)
			status(call("POST", "/jobs/sync-loads", nil, sessionCookie, ""), 403)
			status(call("POST", "/jobs/sync-loads", nil, sessionCookie, session.CSRFToken), 204)
			status(call("PUT", "/settings/users/"+id, repository.ManagedUser{Username: "Admin", Email: "admin@example.com", RoleID: roleID, Version: 1, Active: false}, sessionCookie, session.CSRFToken), 409)
			status(call("PUT", "/settings/roles/"+roleID, repository.AccessRole{Name: "Changed", Version: 1}, sessionCookie, session.CSRFToken), 409)
			status(call("POST", "/settings/roles", repository.AccessRole{Name: "Invalid", Permissions: []string{"made.up"}}, sessionCookie, session.CSRFToken), 400)
			status(call("POST", "/settings/roles", repository.AccessRole{Name: "Viewer", Permissions: []string{"loads.read"}}, sessionCookie, session.CSRFToken), 204)
			data, e := repo.AccessData(ctx)
			if e != nil {
				t.Fatal(e)
			}
			var viewerRole string
			for _, r := range data.Roles {
				if r.Name == "Viewer" {
					viewerRole = r.ID
				}
			}
			status(call("POST", "/settings/users", repository.ManagedUser{Username: "No email", RoleID: viewerRole, Active: true, Password: password}, sessionCookie, session.CSRFToken), 400)
			status(call("POST", "/settings/users", repository.ManagedUser{Username: "No password", Email: "nopass@example.com", RoleID: viewerRole, Active: true}, sessionCookie, session.CSRFToken), 400)
			created := call("POST", "/settings/users", repository.ManagedUser{Username: "Viewer", Email: "VIEWER@example.com", RoleID: viewerRole, Active: true, Password: password}, sessionCookie, session.CSRFToken)
			status(created, 200)
			var createdID map[string]string
			_ = json.Unmarshal(created.Body.Bytes(), &createdID)
			viewerID := createdID["id"]
			status(call("POST", "/settings/users", repository.ManagedUser{Username: "Duplicate", Email: "viewer@example.com", RoleID: viewerRole, Active: true, Password: password}, sessionCookie, session.CSRFToken), 409)
			viewer := login("viewer@example.com", password, true)
			status(viewer, 200)
			viewerCookie := viewer.Result().Cookies()[0]
			var viewerSession sessionResponse
			_ = json.Unmarshal(viewer.Body.Bytes(), &viewerSession)
			status(call("GET", "/loads", nil, viewerCookie, ""), 204)
			status(call("GET", "/settings/access", nil, viewerCookie, ""), 403)
			status(call("POST", "/jobs/sync-loads", nil, viewerCookie, viewerSession.CSRFToken), 403)
			status(call("PUT", "/settings/roles/"+viewerRole, repository.AccessRole{Name: "Viewer", Permissions: []string{}, Version: 1}, sessionCookie, session.CSRFToken), 204)
			status(call("GET", "/loads", nil, viewerCookie, ""), 403)
			status(call("PUT", "/settings/roles/"+viewerRole, repository.AccessRole{Name: "Stale", Version: 1}, sessionCookie, session.CSRFToken), 409)
			resetPassword := "new correct horse battery staple"
			status(call("PUT", "/settings/users/"+viewerID, repository.ManagedUser{Username: "Viewer", Email: "viewer@example.com", RoleID: viewerRole, Active: true, Version: 1, Password: resetPassword}, sessionCookie, session.CSRFToken), 200)
			status(call("GET", "/auth/session", nil, viewerCookie, ""), 401)
			status(login("viewer@example.com", password, false), 401)
			viewer = login("viewer@example.com", resetPassword, false)
			status(viewer, 200)
			viewerCookie = viewer.Result().Cookies()[0]
			_ = json.Unmarshal(viewer.Body.Bytes(), &viewerSession)
			status(call("POST", "/auth/password", map[string]string{"currentPassword": "wrong", "newPassword": password}, viewerCookie, viewerSession.CSRFToken), 401)
			status(call("POST", "/auth/password", map[string]string{"currentPassword": resetPassword, "newPassword": password}, viewerCookie, viewerSession.CSRFToken), 204)
			status(call("GET", "/auth/session", nil, viewerCookie, ""), 401)
			latestViewer := login("viewer@example.com", password, false)
			status(latestViewer, 200)
			beforeRevoke, e := repo.FindUserByEmail(ctx, "admin@example.com")
			if e != nil {
				t.Fatal(e)
			}
			status(call("POST", "/settings/users/"+id+"/revoke", nil, sessionCookie, session.CSRFToken), 204)
			status(call("GET", "/auth/session", nil, sessionCookie, ""), 401)
			status(call("GET", "/auth/session", nil, remembered.Result().Cookies()[0], ""), 401)
			if e = repo.CreatePasswordSession(ctx, beforeRevoke, hashToken("stale-login"), strings.Repeat("c", 43), time.Now().Add(time.Hour)); e != repository.ErrAuthRecordNotFound {
				t.Fatalf("revocation raced stale login: %v", e)
			}
			// Session expiry is enforced in the database, independently of the browser.
			exec(`UPDATE auth_sessions SET expires_at=now()-interval '1 second'`)
			status(call("GET", "/auth/session", nil, latestViewer.Result().Cookies()[0], ""), 401)
			var count int
			if e = admin.QueryRow(ctx, `SELECT count(*) FROM access_audit WHERE details::text LIKE '%'||$1||'%' OR details::text LIKE '%$2%'`, password).Scan(&count); e != nil || count != 0 {
				t.Fatal("password material in audit")
			}
		})
	}
}

func TestPermissionCoverage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(logger, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, AuthOptions{}).(chi.Routes)
	public := map[string]bool{"/healthz": true, "/readyz": true, "/auth/login": true, "/integrations/fleetscope/driver-hired": true}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.ReplaceAll(route, "/*/", "/")
		route = strings.TrimSuffix(route, "/")
		if !public[route] && routePermission(method, route) == "" {
			t.Errorf("unmapped route: %s %s", method, route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path, want string }{{"GET", "/drivers/abc/pay-history", "payroll.read"}, {"POST", "/driver-pay/finalize", "payroll.finalize"}, {"GET", "/new-resource", ""}, {"POST", "/jobs/sync-fuel", "fuel.sync"}, {"POST", "/jobs/sync-eld", "driver_board.write"}, {"PUT", "/gross-board", "board.write"}} {
		if got := routePermission(tc.method, tc.path); got != tc.want {
			t.Errorf("%s: %s", tc.path, got)
		}
	}
}
