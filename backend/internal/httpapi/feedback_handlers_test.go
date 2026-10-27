package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/repository"
)

func TestFeedbackValidationAndPolicies(t *testing.T) {
	for _, tc := range []struct{ method, path string }{{"GET", "/feedback"}, {"POST", "/feedback"}, {"GET", "/feedback/tags"}, {"GET", "/feedback/messages"}, {"GET", "/feedback/messages/count"}, {"PUT", "/feedback/messages/id/read"}, {"GET", "/feedback/id"}, {"PUT", "/feedback/id"}, {"PUT", "/feedback/id/vote"}, {"POST", "/feedback/id/comments"}, {"GET", "/feedback/attachments/id"}} {
		if got := routePermission(tc.method, tc.path); got != "authenticated" {
			t.Fatalf("%v: %s", tc, got)
		}
	}
	if routePermission("DELETE", "/feedback/id") != "" {
		t.Fatal("unregistered delete allowed")
	}
	good := repository.FeedbackInput{Title: " title ", Body: " Details ", Tags: []string{"UI", "bug", "ui"}}
	if err := normalizeFeedback(&good); err != nil || strings.Join(good.Tags, ",") != "bug,ui" || good.Title != "title" {
		t.Fatal(good, err)
	}
	for _, bad := range []repository.FeedbackInput{{Title: " ", Body: "details"}, {Title: "title", Body: " "}, {Title: strings.Repeat("a", 201), Body: "details"}, {Title: "x", Body: strings.Repeat("a", 10001)}, {Title: "x", Body: "bad\x00body"}, {Title: "x", Body: "details", Tags: []string{"<script>"}}, {Title: "x", Body: "details", Tags: []string{""}}, {Title: "x", Body: "details", Tags: make([]string, 9)}} {
		if normalizeFeedback(&bad) == nil {
			t.Fatalf("accepted invalid input %+v", bad)
		}
	}
}

// Real sessions, routes and storage in isolated fresh/migrated schemas, as mserp_app.
func TestFeedbackDatabase(t *testing.T) {
	dsn := os.Getenv("MSERP_FEEDBACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MSERP_FEEDBACK_TEST_DATABASE_URL to a disposable local _test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("use a disposable local _test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := admin.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public`)
	source, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../sql/155_feedback.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(string(source), "\r\n", "\n")
	cut := strings.LastIndex(normalized, "BEGIN;\nCREATE TABLE feedback_posts")
	if cut < 0 {
		t.Fatal("feedback migration missing from init.sql")
	}
	for _, mode := range []string{"fresh", "migration"} {
		t.Run(mode, func(t *testing.T) {
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := admin.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			schema := fmt.Sprintf("feedback_%d", time.Now().UnixNano())
			quoted := pgx.Identifier{schema}.Sanitize()
			exec(`CREATE SCHEMA ` + quoted + `; SET search_path TO ` + quoted + `,public; GRANT USAGE ON SCHEMA ` + quoted + ` TO mserp_app`)
			defer func() {
				_, _ = admin.Exec(context.Background(), `SET search_path TO public; DROP SCHEMA `+quoted+` CASCADE`)
			}()
			if mode == "migration" {
				exec(normalized[:cut])
				exec(`INSERT INTO app_users(username,password_hash) VALUES('Legacy feedback user','$2test')`)
				exec(string(migration))
				exec(`INSERT INTO feedback_posts(id,author_name,title,body,status,submission_hash) VALUES('15500000-0000-0000-0000-000000000099','Legacy','Legacy completed','History','completed','legacy')`)
				paths, e := filepath.Glob("../../sql/[0-9][0-9][0-9]_*.sql")
				if e != nil {
					t.Fatal(e)
				}
				for _, path := range paths {
					if filepath.Base(path)[:3] > "155" {
						body, e := os.ReadFile(path)
						if e != nil {
							t.Fatal(e)
						}
						exec(string(body))
					}
				}
				var historicalMessages int
				if e := admin.QueryRow(ctx, `SELECT count(*) FROM feedback_messages`).Scan(&historicalMessages); e != nil || historicalMessages != 0 {
					t.Fatal("migration must not notify historical completions", e)
				}
				exec(`DELETE FROM feedback_posts WHERE id='15500000-0000-0000-0000-000000000099'`)
			} else {
				exec(normalized)
			}
			for _, table := range []string{"feedback_posts", "feedback_votes", "feedback_comments", "feedback_attachments", "feedback_status_history", "feedback_messages"} {
				var owner string
				if e := admin.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname=$1 AND tablename=$2`, schema, table).Scan(&owner); e != nil || owner != "mserp_app" {
					t.Fatal("runtime table ownership", table, owner, e)
				}
			}
			exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON app_users,app_roles,auth_sessions TO mserp_app`)
			app := cfg.Copy()
			app.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			app.ConnConfig.RuntimeParams["role"] = "mserp_app"
			pool, e := pgxpool.NewWithConfig(ctx, app)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			var role, managerRole string
			if e = admin.QueryRow(ctx, `INSERT INTO app_roles(name,permissions) VALUES('No permissions','{}') RETURNING id::text`).Scan(&role); e != nil {
				t.Fatal(e)
			}
			if e = admin.QueryRow(ctx, `INSERT INTO app_roles(name,permissions) VALUES('Access manager',ARRAY['access.manage']) RETURNING id::text`).Scan(&managerRole); e != nil {
				t.Fatal(e)
			}
			users := map[string]string{}
			for _, name := range []string{"admin", "author", "other", "manager"} {
				selected := role
				if name == "admin" {
					if e = admin.QueryRow(ctx, `SELECT id::text FROM app_roles WHERE system_role`).Scan(&selected); e != nil {
						t.Fatal(e)
					}
				}
				if name == "manager" {
					selected = managerRole
				}
				var id string
				if e = admin.QueryRow(ctx, `INSERT INTO app_users(username,email,password_hash,role_id) VALUES($1,$2,'$2test',$3) RETURNING id::text`, name, name+"@example.test", selected).Scan(&id); e != nil {
					t.Fatal(e)
				}
				users[name] = id
				exec(`INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,repeat('c',43),now()+interval '1 hour')`, id, hashToken(name))
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			auth := newAuthHandler(logger, repository.NewAuthRepository(pool), AuthOptions{SessionTTL: time.Hour})
			router := chi.NewRouter()
			router.Use(auth.requireSession, auth.requireCSRF, requirePermission)
			registerFeedbackRoutes(router, logger, repository.NewFeedbackRepository(pool))
			call := func(user, method, path string, body []byte, contentType string, csrf bool, status int) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(method, path, bytes.NewReader(body))
				if user != "" {
					req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: user})
				}
				req.Header.Set("Content-Type", contentType)
				if csrf {
					req.Header.Set("X-CSRF-Token", strings.Repeat("c", 43))
				}
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				if res.Code != status {
					t.Fatalf("%s %s as %s: %d %s", method, path, user, res.Code, res.Body.String())
				}
				return res
			}
			jsonCall := func(user, method, path string, body any, status int) *httptest.ResponseRecorder {
				t.Helper()
				data, _ := json.Marshal(body)
				return call(user, method, path, data, "application/json", true, status)
			}
			id := "15500000-0000-0000-0000-000000000001"
			input := repository.FeedbackInput{ID: id, Title: "Paste screenshots", Body: "Open Feedback. Paste a screenshot. Expected: image is attached.", Tags: []string{"bug", "UI"}}
			png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aY9sAAAAASUVORK5CYII=")
			multipartBody := func(in repository.FeedbackInput, fileCount int, fileData []byte) ([]byte, string) {
				var b bytes.Buffer
				w := multipart.NewWriter(&b)
				data, _ := json.Marshal(in)
				_ = w.WriteField("post", string(data))
				for i := 0; i < fileCount; i++ {
					part, _ := w.CreateFormFile("files", fmt.Sprintf("screenshot-%d.png", i))
					_, _ = part.Write(fileData)
				}
				_ = w.Close()
				return b.Bytes(), w.FormDataContentType()
			}
			body, kind := multipartBody(input, 1, png)
			call("", "GET", "/feedback", nil, "", false, 401)
			call("author", "POST", "/feedback", body, kind, false, 403)
			call("author", "POST", "/feedback", body, kind, true, 201)
			call("author", "POST", "/feedback", body, kind, true, 201) // retry cannot duplicate post or attachment
			call("other", "POST", "/feedback", body, kind, true, 409)
			changed := input
			changed.Body = "Different"
			b, k := multipartBody(changed, 1, png)
			call("author", "POST", "/feedback", b, k, true, 409)
			for _, user := range []string{"author", "other", "admin", "manager"} {
				res := jsonCall(user, "GET", "/feedback?page=1&pageSize=25&tag=ui", nil, 200)
				var p repository.Page[repository.FeedbackPost]
				_ = json.Unmarshal(res.Body.Bytes(), &p)
				if p.Total != 1 || len(p.Items[0].Attachments) != 1 {
					t.Fatal("shared feed or retry failed", res.Body.String())
				}
			}
			b, k = multipartBody(repository.FeedbackInput{ID: "15500000-0000-0000-0000-000000000002", Title: "Too many files", Body: "Details"}, 6, png)
			call("author", "POST", "/feedback", b, k, true, 400)
			b, k = multipartBody(repository.FeedbackInput{ID: "15500000-0000-0000-0000-000000000002", Title: "Empty file", Body: "Details"}, 1, nil)
			call("author", "POST", "/feedback", b, k, true, 400)
			b, k = multipartBody(repository.FeedbackInput{ID: "15500000-0000-0000-0000-000000000002", Title: "Too large", Body: "Details"}, 1, make([]byte, (10<<20)+1))
			call("author", "POST", "/feedback", b, k, true, 400)
			call("other", "PUT", "/feedback/"+id+"/vote", []byte(`{"voted":true}`), "application/json", false, 403)
			for i := 0; i < 2; i++ {
				jsonCall("other", "PUT", "/feedback/"+id+"/vote", map[string]bool{"voted": true}, 204)
			}
			jsonCall("author", "PUT", "/feedback/"+id+"/vote", map[string]bool{"voted": true}, 204)
			get := func(user string) repository.FeedbackDetail {
				t.Helper()
				res := jsonCall(user, "GET", "/feedback/"+id, nil, 200)
				var d repository.FeedbackDetail
				if e := json.Unmarshal(res.Body.Bytes(), &d); e != nil {
					t.Fatal(e)
				}
				return d
			}
			detail := get("other")
			if detail.Votes != 2 || !detail.Voted || len(detail.Attachments) != 1 {
				t.Fatal("vote uniqueness or attachments", detail)
			}
			if get("admin").Voted {
				t.Fatal("vote state leaked across viewers")
			}
			for i := 0; i < 2; i++ {
				jsonCall("other", "PUT", "/feedback/"+id+"/vote", map[string]bool{"voted": false}, 204)
			}
			if get("other").Votes != 1 {
				t.Fatal("unvote not idempotent")
			}
			comment := map[string]string{"id": "15500000-0000-0000-0000-000000000010", "body": "Also occurs on mobile."}
			for i := 0; i < 2; i++ {
				jsonCall("other", "POST", "/feedback/"+id+"/comments", comment, 204)
			}
			jsonCall("author", "POST", "/feedback/"+id+"/comments", comment, 409)
			if d := get("author"); d.CommentCount != 1 || len(d.Comments) != 1 || d.Comments[0].AuthorName != "other" {
				t.Fatal("shared comments or retry", d)
			}
			edit := repository.FeedbackInput{Title: input.Title, Body: "Updated repro details", Tags: []string{"ux"}, Status: "open", Version: 1}
			jsonCall("other", "PUT", "/feedback/"+id, edit, 403)
			jsonCall("author", "PUT", "/feedback/"+id, edit, 204)
			jsonCall("author", "PUT", "/feedback/"+id, edit, 409)
			edit.Version = 2
			edit.Status = "in_progress"
			jsonCall("author", "PUT", "/feedback/"+id, edit, 403)
			jsonCall("manager", "PUT", "/feedback/"+id, edit, 403)
			for _, status := range []string{"in_progress", "completed", "discarded", "open"} {
				edit.Status = status
				jsonCall("admin", "PUT", "/feedback/"+id, edit, 204)
				edit.Version++
			}
			if d := get("other"); len(d.History) != 4 || d.History[0].ActorName != "admin" || d.Status != "open" {
				t.Fatal("status audit", d)
			}
			call("", "GET", "/feedback/messages", nil, "", false, 401)
			var messages repository.Page[repository.FeedbackMessage]
			messageResponse := jsonCall("author", "GET", "/feedback/messages", nil, 200)
			if e := json.Unmarshal(messageResponse.Body.Bytes(), &messages); e != nil || messages.Total != 1 || messages.Items[0].PostID != id || messages.Items[0].Title != edit.Title || messages.Items[0].CompletedBy != "admin" || messages.Items[0].ReadAt != nil {
				t.Fatal("completion message not delivered to author", messageResponse.Body.String(), e)
			}
			for _, viewer := range []string{"other", "admin", "manager"} {
				res := jsonCall(viewer, "GET", "/feedback/messages?recipientId="+users["author"], nil, 200)
				if !strings.Contains(res.Body.String(), `"total":0`) {
					t.Fatal("private completion message leaked", viewer, res.Body.String())
				}
				jsonCall(viewer, "PUT", "/feedback/messages/"+messages.Items[0].ID+"/read", nil, 404)
			}
			res := jsonCall("author", "GET", "/feedback/messages/count", nil, 200)
			if !strings.Contains(res.Body.String(), `"count":1`) {
				t.Fatal("unread count", res.Body.String())
			}
			call("author", "PUT", "/feedback/messages/"+messages.Items[0].ID+"/read", nil, "", false, 403)
			for i := 0; i < 2; i++ {
				jsonCall("author", "PUT", "/feedback/messages/"+messages.Items[0].ID+"/read", nil, 204)
			}
			res = jsonCall("author", "GET", "/feedback/messages/count", nil, 200)
			if !strings.Contains(res.Body.String(), `"count":0`) {
				t.Fatal("mark read not idempotent", res.Body.String())
			}
			edit.Status = "discarded"
			jsonCall("admin", "PUT", "/feedback/"+id, edit, 204)
			for _, path := range []string{"/feedback", "/feedback?search=repro&tag=ux&sort=top"} {
				res := jsonCall("other", "GET", path, nil, 200)
				if !strings.Contains(res.Body.String(), `"total":0`) {
					t.Fatal("discarded feedback leaked into feed", res.Body.String())
				}
			}
			res = jsonCall("other", "GET", "/feedback?status=discarded", nil, 200)
			if !strings.Contains(res.Body.String(), `"total":1`) {
				t.Fatal("discarded feedback missing from Kanban", res.Body.String())
			}
			edit.Version++
			edit.Status = "open"
			jsonCall("admin", "PUT", "/feedback/"+id, edit, 204)
			res = jsonCall("other", "GET", "/feedback?status=completed&search=repro", nil, 200)
			if !strings.Contains(res.Body.String(), `"total":0`) {
				t.Fatal("status filtering")
			}
			res = jsonCall("other", "GET", "/feedback?status=open&search=repro&tag=ux&sort=top", nil, 200)
			if !strings.Contains(res.Body.String(), `"total":1`) {
				t.Fatal("search and tag filtering", res.Body.String())
			}
			edit.Version++
			edit.Status = "completed"
			jsonCall("admin", "PUT", "/feedback/"+id, edit, 204)
			edit.Version++
			jsonCall("admin", "PUT", "/feedback/"+id, edit, 204) // Saving an already-completed post must not notify again.
			jsonCall("admin", "PUT", "/feedback/"+id, edit, 409) // An uncertain/stale retry must not notify again.
			res = jsonCall("author", "GET", "/feedback/messages", nil, 200)
			if e := json.Unmarshal(res.Body.Bytes(), &messages); e != nil || messages.Total != 2 {
				t.Fatal("completion message duplication", res.Body.String(), e)
			}
			edit.Version++
			edit.Status = "open"
			jsonCall("admin", "PUT", "/feedback/"+id, edit, 204)
			jsonCall("author", "GET", "/tasks", nil, 403) // Personal updates never grant operational task access.
			jsonCall("other", "GET", "/feedback?status=invalid", nil, 400)
			jsonCall("other", "GET", "/feedback/tags", nil, 200)
			jsonCall("other", "GET", "/feedback/not-uuid", nil, 400)
			jsonCall("other", "GET", "/feedback/15500000-0000-0000-0000-000000000099", nil, 404)
			attachment := "/feedback/attachments/" + detail.Attachments[0].ID
			call("", "GET", attachment, nil, "", false, 401)
			res = call("other", "GET", attachment, nil, "", false, 200)
			if !bytes.Equal(res.Body.Bytes(), png) || !strings.HasPrefix(res.Header().Get("Content-Disposition"), "inline") || res.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("image not safely served")
			}
			res = call("other", "GET", attachment+"?download=1", nil, "", false, 200)
			if !strings.HasPrefix(res.Header().Get("Content-Disposition"), "attachment") {
				t.Fatal("download ignored")
			}
			// HTML pretending to be an image must remain a sandboxed download.
			unsafe := repository.FeedbackInput{ID: "15500000-0000-0000-0000-000000000002", Title: "Unsafe file", Body: "Test"}
			b, k = multipartBody(unsafe, 1, []byte("<html><script>alert(1)</script></html>"))
			call("author", "POST", "/feedback", b, k, true, 201)
			var unsafeDetail repository.FeedbackDetail
			res = jsonCall("other", "GET", "/feedback/"+unsafe.ID, nil, 200)
			_ = json.Unmarshal(res.Body.Bytes(), &unsafeDetail)
			res = call("other", "GET", "/feedback/attachments/"+unsafeDetail.Attachments[0].ID, nil, "", false, 200)
			if !strings.HasPrefix(res.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(res.Header().Get("Content-Security-Policy"), "sandbox") {
				t.Fatal("active content could render")
			}
			jsonCall("other", "GET", "/feedback?page=99&pageSize=1", nil, 200)
			exec(`UPDATE app_users SET active=false WHERE id=$1`, users["other"])
			call("other", "GET", attachment, nil, "", false, 401)
		})
	}
}
