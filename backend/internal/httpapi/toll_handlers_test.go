package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTollDashboardRejectsInvalidDates(t *testing.T) {
	for _, query := range []string{
		"dateFrom=invalid", "dateTo=2026-02-30",
		"dateFrom=2026-03-01&dateTo=2026-02-01",
		"dateFrom=2020-01-01&dateTo=2026-01-01",
	} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			tollHandler{}.tollDashboard(response, httptest.NewRequest(http.MethodGet, "/toll-dashboard?"+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}
