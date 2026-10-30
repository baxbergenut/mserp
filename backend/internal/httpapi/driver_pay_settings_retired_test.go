package httpapi

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDriverPaySettingsRetired(t *testing.T) {
	router := chi.NewRouter()
	registerDriverPayRoutes(router, slog.Default(), nil, nil)
	for _, method := range []string{"GET", "PUT"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, "/settings/driver-pay", strings.NewReader(`{"layoverDailyAmount":"100","version":1}`)))
		if recorder.Code != 404 {
			t.Fatalf("%s retired settings: got %d", method, recorder.Code)
		}
	}
}
