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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/repository"
)

func TestUserProfilesDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable local MSERP_ACCESS_TEST_DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "127.0.0.1") {
		t.Fatal("local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/165_user_profiles_birthdays.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(string(source), "\r\n", "\n")
	change := strings.TrimSpace(strings.ReplaceAll(string(migration), "\r\n", "\n"))
	if !strings.Contains(normalized, change) {
		t.Fatal("fresh schema missing profile migration")
	}
	for _, mode := range []string{"fresh", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("user_profiles_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := db.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + quoted + ";SET search_path TO " + quoted + ",public")
			defer db.Exec(context.Background(), "SET search_path TO public;DROP SCHEMA "+quoted+" CASCADE")
			if mode == "fresh" {
				exec(string(source))
			} else {
				exec(strings.Replace(normalized, change, "", 1))
				exec(`INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Unsplit Legacy Name','legacy@example.com',crypt('fixture-only-password',gen_salt('bf')),id FROM app_roles WHERE system_role`)
				exec(string(migration))
				var preserved bool
				if err := db.QueryRow(ctx, `SELECT username='Unsplit Legacy Name' AND first_name='' AND last_name='' AND date_of_birth IS NULL AND phone='' FROM app_users WHERE email='legacy@example.com'`).Scan(&preserved); err != nil || !preserved {
					t.Fatalf("legacy profile not preserved: %v", err)
				}
			}
			exec(`INSERT INTO app_roles(name,permissions) VALUES('Profile manager',ARRAY['access.manage']),('Birthday user',ARRAY[]::text[])`)
			var adminID, managerID, userID, userRole string
			if err := db.QueryRow(ctx, `INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Admin','admin@example.com',crypt('fixture-only-password',gen_salt('bf')),id FROM app_roles WHERE system_role RETURNING id::text`).Scan(&adminID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(ctx, `INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Manager','manager@example.com',crypt('fixture-only-password',gen_salt('bf')),id FROM app_roles WHERE name='Profile manager' RETURNING id::text`).Scan(&managerID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(ctx, `SELECT id::text FROM app_roles WHERE name='Birthday user'`).Scan(&userRole); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(ctx, `INSERT INTO app_users(username,email,password_hash,role_id) VALUES('Alex Smith','alex@example.com',crypt('fixture-only-password',gen_salt('bf')),$1) RETURNING id::text`, userRole).Scan(&userID); err != nil {
				t.Fatal(err)
			}
			exec("GRANT USAGE ON SCHEMA " + quoted + " TO mserp_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO mserp_app")
			appCfg := cfg.Copy()
			appCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, "SET ROLE mserp_app")
				return err
			}
			appCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			pool, err := pgxpool.NewWithConfig(ctx, appCfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repo := repository.NewAuthRepository(pool)
			h := newAuthHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), repo, AuthOptions{})
			h.now = func() time.Time { return time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC) }
			router := chi.NewRouter()
			router.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					actor := r.Header.Get("X-Test-Actor")
					user := repository.AuthUser{ID: actor, Administrator: actor == adminID}
					if actor == adminID || actor == managerID {
						user.Permissions = []string{"access.manage"}
					}
					next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, repository.AuthSession{User: user, CSRFToken: "csrf"})))
				})
			})
			router.Use(h.requireCSRF, requirePermission)
			registerAccessRoutes(router, h, repo)
			router.Post("/auth/birthday", h.birthday)
			call := func(method, path, actor string, body any, want int) *httptest.ResponseRecorder {
				t.Helper()
				raw, _ := json.Marshal(body)
				req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
				req.Header.Set("X-Test-Actor", actor)
				req.Header.Set("X-CSRF-Token", "csrf")
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				if res.Code != want {
					t.Fatalf("%s %s got %d want %d: %s", method, path, res.Code, want, res.Body.String())
				}
				return res
			}
			dob, phone := "2000-10-29", "+1 (555) 123-4567"
			input := repository.ManagedUser{FirstName: "Alex", LastName: "Smith", Email: "alex@example.com", RoleID: userRole, Active: true, Version: 1, DateOfBirth: &dob, Phone: &phone}
			call("PUT", "/settings/users/"+userID, adminID, input, 200)
			adminData := call("GET", "/settings/access", adminID, nil, 200).Body.String()
			if !strings.Contains(adminData, dob) || !strings.Contains(adminData, phone) {
				t.Fatal("administrator private fields missing")
			}
			managerData := call("GET", "/settings/access", managerID, nil, 200).Body.String()
			if strings.Contains(managerData, "dateOfBirth") || strings.Contains(managerData, "phone") || strings.Contains(managerData, dob) {
				t.Fatal("private profile leaked to access manager")
			}
			call("PUT", "/settings/users/"+userID, managerID, input, 403)
			input.DateOfBirth, input.Phone = nil, nil
			input.Version = 2
			input.FirstName = "Alexandra"
			call("PUT", "/settings/users/"+userID, managerID, input, 200)
			call("PUT", "/settings/users/"+userID, managerID, input, 409)
			var privatePreserved, auditSafe bool
			if err := db.QueryRow(ctx, `SELECT date_of_birth::text=$2 AND phone=$3 FROM app_users WHERE id=$1`, userID, dob, phone).Scan(&privatePreserved); err != nil || !privatePreserved {
				t.Fatalf("private fields changed: %v", err)
			}
			if err := db.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM access_audit WHERE details::text LIKE '%dateOfBirth%' OR details::text LIKE '%phone%')`).Scan(&auditSafe); err != nil || !auditSafe {
				t.Fatalf("audit leaked private data: %v", err)
			}
			call("GET", "/settings/access", userID, nil, 403)
			greeting := func() repository.BirthdayGreeting {
				var g repository.BirthdayGreeting
				res := call("POST", "/auth/birthday", userID, nil, 200)
				if err := json.Unmarshal(res.Body.Bytes(), &g); err != nil {
					t.Fatal(err)
				}
				return g
			}
			h.now = func() time.Time { return time.Date(2026, 10, 28, 18, 59, 59, 0, time.UTC) }
			if preview := greeting(); preview.Celebrate || preview.CountdownAt == nil || preview.CountdownAt.In(birthdayLocation).Format("2006-01-02 15:04:05") != "2026-10-29 00:00:00" {
				t.Fatal("birthday started before local midnight")
			}
			h.now = func() time.Time { return time.Date(2026, 10, 28, 19, 0, 0, 0, time.UTC) }
			if g := greeting(); !g.Celebrate || g.Name != "Alexandra" {
				t.Fatalf("missing local birthday greeting: %+v", g)
			}
			if greeting().Celebrate {
				t.Fatal("repeat greeting")
			}
			exec(`UPDATE app_users SET birthday_greeted_year=NULL WHERE id=$1`, userID)
			var wins atomic.Int32
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					g, e := repo.ClaimBirthday(ctx, userID, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
					if e != nil {
						t.Error(e)
					}
					if g.Celebrate {
						wins.Add(1)
					}
				}()
			}
			wg.Wait()
			if wins.Load() != 1 {
				t.Fatalf("concurrent greetings: %d", wins.Load())
			}
			exec(`UPDATE app_users SET date_of_birth='2000-02-29',birthday_greeted_year=NULL WHERE id=$1`, userID)
			for _, day := range []time.Time{time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC), time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)} {
				g, e := repo.ClaimBirthday(ctx, userID, day)
				if e != nil || g.Celebrate {
					t.Fatalf("leap birthday fallback: %+v %v", g, e)
				}
			}
			g, e := repo.ClaimBirthday(ctx, userID, time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC))
			if e != nil || !g.Celebrate {
				t.Fatalf("leap birthday missing: %+v %v", g, e)
			}
			exec(`UPDATE app_users SET date_of_birth=NULL,birthday_greeted_year=NULL WHERE id=$1`, userID)
			if greeting().Celebrate {
				t.Fatal("missing DOB celebrated")
			}
			input.Version = 3
			empty := ""
			input.Phone = &empty
			input.DateOfBirth = &empty
			call("PUT", "/settings/users/"+userID, adminID, input, 200)
			var cleared bool
			if err := db.QueryRow(ctx, `SELECT date_of_birth IS NULL AND phone='' FROM app_users WHERE id=$1`, userID).Scan(&cleared); err != nil || !cleared {
				t.Fatalf("private clear failed: %v", err)
			}
		})
	}
}
