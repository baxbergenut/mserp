package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func TestGrossBoardBalanceResetAuthorization(t *testing.T) {
	router := chi.NewRouter()
	router.Use(requirePermission)
	registerGrossBoardRoutes(router, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	for _, tc := range []struct {
		name string
		user repository.AuthUser
		want int
	}{
		{"administrator", repository.AuthUser{Administrator: true, Permissions: repository.PermissionKeys()}, http.StatusBadRequest},
		{"accountant", repository.AuthUser{RoleName: "Accountant", Permissions: []string{"board.read"}}, http.StatusBadRequest},
		{"accountant without board access", repository.AuthUser{RoleName: "Accountant"}, http.StatusForbidden},
		{"dispatcher with edit access", repository.AuthUser{RoleName: "Dispatcher", Permissions: []string{"board.read", "board.write"}}, http.StatusForbidden},
		{"custom accounting permission", repository.AuthUser{RoleName: "Custom", Permissions: []string{"board.read", "expenses.accounting.verify"}}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A malformed amount reaches validation only for authorized users.
			request := httptest.NewRequest(http.MethodPost, "/gross-board/balance/reset", strings.NewReader(`{"balance":"1.234"}`))
			request = request.WithContext(context.WithValue(request.Context(), authContextKey{}, repository.AuthSession{User: tc.user}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.want, response.Body)
			}
		})
	}
}
