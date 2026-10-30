package httpapi

import (
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPerformancePreservesStreaming(t *testing.T) {
	r := chi.NewRouter()
	r.Use(performance(slog.New(slog.NewTextHandler(io.Discard, nil))))
	r.Get("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: test\n\n"))
		w.(http.Flusher).Flush()
	})
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest("GET", "/events", nil))
	if !out.Flushed || !strings.Contains(out.Header().Get("Server-Timing"), "queries;") || out.Body.String() != "data: test\n\n" {
		t.Fatal("streaming/timing broken")
	}
}

func TestPayrollPerformanceScenarios(t *testing.T) {
	for _, tc := range []struct{ method, url, want string }{
		{"GET", "/driver-pay?summary=1", "summary"},
		{"GET", "/investor-pay?summary=1&detailIds=private-id", "summary_with_details"},
		{"GET", "/driver-pay?statementId=private-id", "statement_detail"},
		{"GET", "/driver-pay/statements/private-id", "statement_detail"},
		{"GET", "/driver-pay", "full_report"},
		{"PUT", "/investor-pay", "save"},
		{"POST", "/driver-pay/finalize", "settlement"},
		{"POST", "/investor-pay/reopen", "settlement"},
	} {
		if got := payrollPerformanceScenario(httptest.NewRequest(tc.method, tc.url, nil)); got != tc.want {
			t.Errorf("%s: %s", tc.url, got)
		}
	}
}
