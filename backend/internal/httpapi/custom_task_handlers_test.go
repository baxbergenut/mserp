package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type fakeCustomTasks struct {
	called               bool
	input                repository.CustomTaskInput
	user, search, status string
	completed            bool
	err                  error
}

func (f *fakeCustomTasks) List(_ context.Context, _ repository.Pagination, search, status string) (repository.Page[repository.CustomTask], error) {
	f.called, f.search, f.status = true, search, status
	return repository.Page[repository.CustomTask]{Items: []repository.CustomTask{}}, f.err
}
func (f *fakeCustomTasks) Create(_ context.Context, input repository.CustomTaskInput, user string) (repository.CustomTask, error) {
	f.called, f.input, f.user = true, input, user
	return repository.CustomTask{Title: input.Title, Notes: input.Notes}, f.err
}
func (f *fakeCustomTasks) Update(_ context.Context, _ string, input repository.CustomTaskInput) (repository.CustomTask, error) {
	f.called, f.input = true, input
	return repository.CustomTask{}, f.err
}
func (f *fakeCustomTasks) SetCompleted(_ context.Context, _ string, completed bool) (repository.CustomTask, error) {
	f.called, f.completed = true, completed
	return repository.CustomTask{}, f.err
}
func (f *fakeCustomTasks) Delete(context.Context, string) error { f.called = true; return f.err }

func TestCustomTaskRoutes(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000001"
	const path = "/tasks/custom/" + id
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := newAuthHandler(logger, &fakeAuthStore{sessions: map[string]repository.AuthSession{
		hashToken("test-session"): {User: repository.AuthUser{ID: id}, CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)},
	}}, AuthOptions{})
	for _, tc := range []struct {
		name, method, path, body string
		err                      error
		want                     int
		called                   bool
	}{
		{name: "list", method: "GET", path: "/tasks/custom?search=%20truck%20", want: 200, called: true},
		{name: "bad status", method: "GET", path: "/tasks/custom?status=invalid", want: 400},
		{name: "bad page", method: "GET", path: "/tasks/custom?pageSize=101", want: 400},
		{name: "create", method: "POST", path: "/tasks/custom", body: `{"title":"  Call shop  ","notes":" details "}`, want: 201, called: true},
		{name: "empty", method: "POST", path: "/tasks/custom", body: `{"title":"  "}`, want: 400},
		{name: "long title", method: "POST", path: "/tasks/custom", body: `{"title":"` + strings.Repeat("a", 201) + `"}`, want: 400},
		{name: "long notes", method: "POST", path: "/tasks/custom", body: `{"title":"test","notes":"` + strings.Repeat("a", 5001) + `"}`, want: 400},
		{name: "unknown field", method: "POST", path: "/tasks/custom", body: `{"title":"test","createdBy":"fake"}`, want: 400},
		{name: "null character", method: "POST", path: "/tasks/custom", body: `{"title":"bad\u0000"}`, want: 400},
		{name: "two objects", method: "POST", path: "/tasks/custom", body: `{"title":"test"} {}`, want: 400},
		{name: "unicode title", method: "POST", path: "/tasks/custom", body: `{"title":"` + strings.Repeat("界", 200) + `"}`, want: 201, called: true},
		{name: "edit", method: "PUT", path: path, body: `{"title":"Revised","notes":"note"}`, want: 200, called: true},
		{name: "bad id", method: "PUT", path: "/tasks/custom/bad", body: `{"title":"test"}`, want: 400},
		{name: "complete", method: "PATCH", path: path, body: `{"completed":true}`, want: 200, called: true},
		{name: "reopen", method: "PATCH", path: path, body: `{"completed":false}`, want: 200, called: true},
		{name: "missing status", method: "PATCH", path: path, body: `{}`, want: 400},
		{name: "null status", method: "PATCH", path: path, body: `{"completed":null}`, want: 400},
		{name: "delete", method: "DELETE", path: path, want: 204, called: true},
		{name: "missing task", method: "DELETE", path: path, err: repository.ErrNotFound, want: 404, called: true},
		{name: "database error", method: "POST", path: "/tasks/custom", body: `{"title":"test"}`, err: errors.New("private database detail"), want: 500, called: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeCustomTasks{err: tc.err}
			router := chi.NewRouter()
			router.Use(auth.requireSession, auth.requireCSRF)
			registerCustomTaskRoutes(router, logger, store)
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
			request.Header.Set("X-CSRF-Token", "csrf")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want || store.called != tc.called {
				t.Fatalf("status=%d called=%v body=%s", response.Code, store.called, response.Body.String())
			}
			if tc.name == "create" && (store.input.Title != "Call shop" || store.input.Notes != "details" || store.user != id) {
				t.Fatalf("incorrect input or creator: %+v", store)
			}
			if tc.name == "list" && (store.search != "truck" || store.status != "open") {
				t.Fatalf("incorrect filters: %+v", store)
			}
			if tc.name == "complete" && !store.completed {
				t.Fatal("completion not applied")
			}
			if strings.Contains(response.Body.String(), "private database detail") {
				t.Fatal("database detail exposed")
			}
		})
	}
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		for _, signedIn := range []bool{false, true} {
			if method == "GET" && signedIn {
				continue
			}
			t.Run(method+" authentication", func(t *testing.T) {
				store := &fakeCustomTasks{}
				router := chi.NewRouter()
				router.Use(auth.requireSession, auth.requireCSRF)
				registerCustomTaskRoutes(router, logger, store)
				target := path
				if method == "GET" || method == "POST" {
					target = "/tasks/custom"
				}
				request := httptest.NewRequest(method, target, strings.NewReader(`{"title":"test"}`))
				want := 401
				if signedIn {
					request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "test-session"})
					want = 403
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != want || store.called {
					t.Fatalf("status=%d called=%v", response.Code, store.called)
				}
			})
		}
	}
}
