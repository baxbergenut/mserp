package httpapi

import (
	"bytes"
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
	"mserp/internal/repository"
)

// Real sessions, API handlers and SQL run as mserp_app in a private schema.
func TestTaskAssignmentsDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_ACCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_ACCESS_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.Contains(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("disposable local _test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			schema := fmt.Sprintf("task_assignments_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
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
			migration, e := os.ReadFile("../../sql/054_task_assignments.sql")
			if e != nil {
				t.Fatal(e)
			}
			expenseAccessMigration, e := os.ReadFile("../../sql/056_expense_category_access.sql")
			if e != nil {
				t.Fatal(e)
			}
			normalized := strings.ReplaceAll(string(source), "\r\n", "\n")
			if mode == "migration" {
				before, _, ok := strings.Cut(normalized, strings.ReplaceAll(string(migration), "\r\n", "\n"))
				if !ok {
					t.Fatal("migration must match fresh schema")
				}
				exec(before)
				exec(`INSERT INTO custom_tasks(id,title,completed_at) VALUES('05400000-0000-0000-0000-000000000001','Legacy offboarding',now());
    INSERT INTO fleetscope_driver_terminations(company_id,fleetscope_driver_id,driver_name,termination_date,occurred_at,task_id)
    VALUES(gen_random_uuid(),gen_random_uuid(),'Legacy','2026-10-01',now(),'05400000-0000-0000-0000-000000000001')`)
				exec(string(migration))
				exec(string(expenseAccessMigration))
				themeMigration, e := os.ReadFile("../../sql/065_user_color_theme.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(themeMigration))
				boardMigration, e := os.ReadFile("../../sql/066_unified_tasks.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(boardMigration))
				processMigration, e := os.ReadFile("../../sql/067_task_in_process.sql")
				if e != nil {
					t.Fatal(e)
				}
				exec(string(processMigration))
				var kind string
				if e = admin.QueryRow(ctx, `SELECT system_task_kind FROM custom_tasks WHERE title='Legacy offboarding'`).Scan(&kind); e != nil || kind != "driver_offboarding" {
					t.Fatal("offboarding backfill", kind, e)
				}
			} else {
				exec(string(source))
			}
			// New assignments table relies on migration ownership; legacy tables need grants.
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON app_users,auth_sessions,drivers,trucks,dispatchers,files,truck_driver_assignments,relay_driver_links,fuel_transactions,fuel_transaction_items TO mserp_app`)
			var owner string
			if e = admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename='system_task_assignments'`, schema).Scan(&owner); e != nil || owner != "mserp_app" {
				t.Fatal("assignment ownership", owner, e)
			}
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			repo := repository.NewAuthRepository(pool)
			var roleID string
			if e = admin.QueryRow(ctx, `INSERT INTO app_roles(name,permissions) VALUES('Task team',ARRAY['tasks.read','tasks.write','fleet.read','fleet.write','access.manage']) RETURNING id::text`).Scan(&roleID); e != nil {
				t.Fatal(e)
			}
			users := map[string]string{}
			for _, name := range []string{"admin", "assigner", "assignee", "outsider"} {
				role := roleID
				if name == "admin" {
					if e = admin.QueryRow(ctx, `SELECT id::text FROM app_roles WHERE system_role`).Scan(&role); e != nil {
						t.Fatal(e)
					}
				}
				var id string
				if e = admin.QueryRow(ctx, `INSERT INTO app_users(username,email,password_hash,role_id) VALUES($1,$2,'$2test',$3) RETURNING id::text`, name, name+"@example.com", role).Scan(&id); e != nil {
					t.Fatal(e)
				}
				users[name] = id
				exec(`INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,repeat('c',43),now()+interval '1 hour')`, id, hashToken(name))
			}
			h := newAuthHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), repo, AuthOptions{SessionTTL: time.Hour})
			router := chi.NewRouter()
			router.Use(h.requireSession, h.requireCSRF, requirePermission)
			registerAccessRoutes(router, h, repo)
			registerCustomTaskRoutes(router, h.logger, repository.NewCustomTaskRepository(pool))
			registerTaskBoardRoutes(router, h.logger, repository.NewCustomTaskRepository(pool))
			registerDriverIntakeRoutes(router, h.logger, repository.NewFleetRepository(pool))
			registerRelayIdentityRoutes(router, h.logger, repository.NewFuelRepository(pool))
			call := func(user, method, path string, body any, status int) *httptest.ResponseRecorder {
				t.Helper()
				b, _ := json.Marshal(body)
				request := httptest.NewRequest(method, path, bytes.NewReader(b))
				request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: user})
				request.Header.Set("X-CSRF-Token", strings.Repeat("c", 43))
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != status {
					t.Fatalf("%s %s as %s: %d %s", method, path, user, response.Code, response.Body.String())
				}
				return response
			}
			list := func(user, path string) int {
				t.Helper()
				response := call(user, "GET", path, nil, 200)
				var page struct {
					Total int `json:"total"`
				}
				if e := json.Unmarshal(response.Body.Bytes(), &page); e != nil {
					t.Fatal(e)
				}
				return page.Total
			}
			created := call("assigner", "POST", "/tasks/custom", map[string]any{"title": "Private task", "assignedTo": users["assignee"]}, 201)
			var task repository.CustomTask
			if e = json.Unmarshal(created.Body.Bytes(), &task); e != nil {
				t.Fatal(e)
			}
			if task.AssignedBy == nil || *task.AssignedBy != users["assigner"] {
				t.Fatal("assigner not retained")
			}
			for _, user := range []string{"assigner", "assignee", "admin"} {
				if list(user, "/tasks/custom") != 1 {
					t.Fatal("private task missing for", user)
				}
			}
			if list("outsider", "/tasks/custom") != 0 {
				t.Fatal("private task leaked to user with access.manage")
			}
			if list("outsider", "/tasks") != 0 || list("assignee", "/tasks") != 1 {
				t.Fatal("unified task privacy")
			}
			var count struct {
				Count int `json:"count"`
			}
			json.Unmarshal(call("outsider", "GET", "/tasks/count", nil, 200).Body.Bytes(), &count)
			if count.Count != 0 {
				t.Fatal("private task count leaked")
			}
			path := "/tasks/custom/" + task.ID
			call("outsider", "PUT", path, map[string]any{"title": "Hijacked"}, 404)
			call("outsider", "PATCH", path, map[string]any{"completed": true}, 404)
			call("outsider", "DELETE", path, nil, 404)
			call("assignee", "PUT", path, map[string]any{"title": "Updated", "assignedTo": users["assignee"]}, 200)
			if list("assigner", "/tasks/custom") != 1 {
				t.Fatal("editing content changed assigner")
			}
			call("assigner", "PATCH", path, map[string]any{"status": "in_process"}, 200)
			if list("assignee", "/tasks?status=in_process") != 1 || list("assignee", "/tasks?status=open") != 0 {
				t.Fatal("in-process status not persisted or filtered")
			}
			json.Unmarshal(call("assignee", "GET", "/tasks/count", nil, 200).Body.Bytes(), &count)
			if count.Count != 1 {
				t.Fatal("in-process task missing from incomplete count")
			}
			call("assigner", "PATCH", path, map[string]any{"completed": true}, 200)
			var board repository.Page[repository.BoardTask]
			json.Unmarshal(call("assigner", "GET", "/tasks?status=completed", nil, 200).Body.Bytes(), &board)
			if len(board.Items) != 1 || board.Items[0].CompletedByName != "assigner" || board.Items[0].AssignerName != "assigner" || board.Items[0].AssigneeName != "assignee" {
				t.Fatalf("task audit: %+v", board)
			}
			if list("outsider", "/tasks/custom?status=all") != 0 {
				t.Fatal("completed task leaked")
			}
			call("assigner", "PATCH", path, map[string]any{"completed": false}, 200)
			call("assigner", "PUT", path, map[string]any{"title": "Reassigned", "assignedTo": users["outsider"]}, 200)
			if list("assignee", "/tasks/custom") != 0 || list("outsider", "/tasks/custom") != 1 {
				t.Fatal("reassignment visibility")
			}
			call("assigner", "PUT", path, map[string]any{"title": "Shared", "assignedTo": ""}, 200)
			if list("assignee", "/tasks/custom") != 1 {
				t.Fatal("unassigned task should be shared")
			}
			call("assigner", "DELETE", path, nil, 204)
			call("assignee", "POST", "/tasks/custom", map[string]any{"title": "Self assigned", "assignedTo": users["assignee"]}, 201)
			if list("assignee", "/tasks/custom") != 1 || list("outsider", "/tasks/custom") != 0 {
				t.Fatal("self assignment visibility")
			}
			// Each category defaults to Administrators only, then follows its Settings assignment.
			var intakeID, driverID, relayID string
			if e = admin.QueryRow(ctx, `INSERT INTO fleetscope_driver_intake(company_id,fleetscope_driver_id,driver_data,normalized_name,occurred_at) VALUES(gen_random_uuid(),gen_random_uuid(),'{"fullName":"New Hire"}','new hire',now()) RETURNING id::text`).Scan(&intakeID); e != nil {
				t.Fatal(e)
			}
			if e = admin.QueryRow(ctx, `INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate) VALUES('Existing','existing','cpm',0.75) RETURNING id::text`).Scan(&driverID); e != nil {
				t.Fatal(e)
			}
			if e = admin.QueryRow(ctx, `INSERT INTO relay_driver_links(relay_environment,relay_driver_id) VALUES('production','test-task-relay') RETURNING id::text`).Scan(&relayID); e != nil {
				t.Fatal(e)
			}
			exec(`INSERT INTO custom_tasks(title,system_task_kind) VALUES('Offboard test','driver_offboarding')`)
			if list("outsider", "/driver-intake") != 0 || list("outsider", "/driver-directory") != 1 || list("outsider", "/tasks/relay-identities") != 0 {
				t.Fatal("unassigned system tasks leaked")
			}
			call("outsider", "GET", "/driver-intake/"+intakeID, nil, 404)
			call("outsider", "POST", "/driver-intake/"+intakeID+"/complete", map[string]any{"linkDriverId": driverID}, 404)
			call("outsider", "POST", "/tasks/relay-identities/"+relayID+"/review", map[string]any{"driverId": driverID, "action": "link"}, 404)
			for _, kind := range []string{"driver_onboarding", "driver_offboarding", "relay_review"} {
				call("admin", "PUT", "/settings/system-tasks/"+kind, map[string]any{"assigneeId": users["assignee"], "version": 1}, 204)
				call("admin", "PUT", "/settings/system-tasks/"+kind, map[string]any{"assigneeId": users["outsider"], "version": 1}, 409)
			}
			if list("assignee", "/driver-intake") != 1 || list("assignee", "/driver-directory") != 2 || list("assignee", "/tasks/relay-identities") != 1 {
				t.Fatal("assigned system tasks missing")
			}
			call("assignee", "GET", "/driver-intake/"+intakeID, nil, 200)
			call("assignee", "POST", "/tasks/relay-identities/"+relayID+"/review", map[string]any{"driverId": driverID, "action": "link"}, 200)
			if list("assignee", "/tasks/relay-identities") != 0 {
				t.Fatal("assigned Relay review did not complete")
			}
			json.Unmarshal(call("assignee", "GET", "/tasks?status=completed&search=Relay", nil, 200).Body.Bytes(), &board)
			if len(board.Items) != 1 || board.Items[0].ID != relayID || board.Items[0].CompletedByName != "assignee" || board.Items[0].AssignerName != "System" {
				t.Fatalf("Relay completion history: %+v", board)
			}
			// System tasks cannot be dragged, edited or deleted through the custom API.
			var offboardingID string
			if e = admin.QueryRow(ctx, `SELECT id::text FROM custom_tasks WHERE title='Offboard test'`).Scan(&offboardingID); e != nil {
				t.Fatal(e)
			}
			for _, actor := range []string{"assignee", "admin"} {
				call(actor, "PATCH", "/tasks/custom/"+offboardingID, map[string]any{"completed": true}, 404)
				call(actor, "PATCH", "/tasks/custom/"+offboardingID, map[string]any{"status": "in_process"}, 404)
				call(actor, "PUT", "/tasks/custom/"+offboardingID, map[string]any{"title": "Changed"}, 404)
				call(actor, "DELETE", "/tasks/custom/"+offboardingID, nil, 404)
			}
			if list("outsider", "/tasks/custom") != 0 {
				t.Fatal("offboarding leaked")
			}
			if list("assignee", "/tasks/custom") != 2 {
				t.Fatal("offboarding missing")
			}
			// Reassignment takes effect on the next request, without issuing new sessions.
			call("admin", "PUT", "/settings/system-tasks/driver_onboarding", map[string]any{"assigneeId": users["outsider"], "version": 2}, 204)
			if list("assignee", "/driver-intake") != 0 || list("outsider", "/driver-intake") != 1 {
				t.Fatal("system reassignment not immediate")
			}
			call("assignee", "GET", "/driver-intake/"+intakeID, nil, 404)
			call("outsider", "POST", "/driver-intake/"+intakeID+"/complete", map[string]any{"linkDriverId": driverID}, 200)
			if list("outsider", "/driver-intake") != 0 {
				t.Fatal("assigned onboarding did not complete")
			}
			json.Unmarshal(call("outsider", "GET", "/tasks?status=completed", nil, 200).Body.Bytes(), &board)
			if len(board.Items) != 1 || board.Items[0].ID != intakeID || board.Items[0].CompletedByName != "outsider" {
				t.Fatalf("onboarding history: %+v", board)
			}
			checklist := map[string]bool{"equipment": true, "access": true, "settlement": true}
			call("outsider", "POST", "/tasks/offboarding/"+offboardingID+"/confirm", checklist, 404)
			call("assignee", "POST", "/tasks/offboarding/"+offboardingID+"/confirm", map[string]bool{"equipment": true}, 400)
			call("assignee", "POST", "/tasks/offboarding/"+offboardingID+"/confirm", checklist, 204)
			if list("assignee", "/tasks?status=completed&search=Offboard%20test") != 1 {
				t.Fatal("offboarding completion missing")
			}
			// Deleting source identities and renaming an actor must not erase work history.
			exec(`DELETE FROM fleetscope_driver_intake WHERE id=$1`, intakeID)
			if list("outsider", "/tasks?status=completed") != 1 {
				t.Fatal("source deletion erased completion")
			}
			exec(`UPDATE app_users SET username='Renamed' WHERE id=$1`, users["assignee"])
			json.Unmarshal(call("assignee", "GET", "/tasks?status=completed&search=Relay", nil, 200).Body.Bytes(), &board)
			if len(board.Items) != 1 || board.Items[0].CompletedByName != "assignee" {
				t.Fatal("completed actor snapshot lost")
			}
			exec(`UPDATE app_users SET active=false WHERE id=$1`, users["outsider"])
			call("admin", "PUT", "/settings/system-tasks/relay_review", map[string]any{"assigneeId": users["outsider"], "version": 2}, 409)
		})
	}
}
