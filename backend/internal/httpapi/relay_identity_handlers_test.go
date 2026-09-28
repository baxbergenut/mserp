package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type fakeRelayReview struct {
	called bool
	err    error
	user   string
}

func (f *fakeRelayReview) RelayIdentityTasks(context.Context, repository.Pagination, string) (repository.Page[repository.RelayIdentityTask], error) {
	return repository.Page[repository.RelayIdentityTask]{}, f.err
}
func (f *fakeRelayReview) ReviewRelayIdentity(_ context.Context, _ string, _ string, _ string, user string) (int64, error) {
	f.called = true
	f.user = user
	return 4, f.err
}
func TestRelayReviewValidation(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000001"
	for _, tc := range []struct {
		name, body    string
		authenticated bool
		err           error
		status        int
		called        bool
	}{
		{"signed out", `{"driverId":"` + id + `","action":"link"}`, false, nil, 401, false},
		{"bad driver", `{"driverId":"bad","action":"link"}`, true, nil, 400, false},
		{"bad action", `{"driverId":"` + id + `","action":"merge"}`, true, nil, 400, false},
		{"conflict", `{"driverId":"` + id + `","action":"link"}`, true, repository.ErrRelayReviewConflict, 409, true},
		{"missing", `{"driverId":"` + id + `","action":"link"}`, true, repository.ErrNotFound, 404, true},
		{"link", `{"driverId":"` + id + `","action":"link"}`, true, nil, 200, true},
		{"reject", `{"driverId":"` + id + `","action":"reject"}`, true, nil, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeRelayReview{err: tc.err}
			router := chi.NewRouter()
			registerRelayIdentityRoutes(router, slog.New(slog.NewTextHandler(io.Discard, nil)), store)
			request := httptest.NewRequest("POST", "/tasks/relay-identities/"+id+"/review", strings.NewReader(tc.body))
			if tc.authenticated {
				request = request.WithContext(context.WithValue(request.Context(), authContextKey{}, repository.AuthSession{User: repository.AuthUser{ID: id}}))
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status || store.called != tc.called {
				t.Fatalf("status=%d called=%v body=%s", response.Code, store.called, response.Body.String())
			}
			if store.called && store.user != id {
				t.Fatal("reviewer not propagated")
			}
		})
	}
}
