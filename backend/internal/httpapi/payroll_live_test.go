package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func TestPayrollLiveDisabledAndPermissions(t *testing.T) {
	r := chi.NewRouter()
	closeLive := registerPayrollLive(r, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, repository.NewDriverPayRepository(nil))
	defer closeLive()
	for _, kind := range []string{"driver", "investor"} {
		path := "/" + kind + "-pay/events"
		if permission := routePermission(http.MethodGet, path); permission != kind+"_pay.read" {
			t.Fatal("incorrect stream permission", path, permission)
		}
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatal("disabled prepared read exposed stream", response.Code)
		}
	}
}
