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

func TestBoardBrowserTimingPhases(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewTextHandler(&output, nil))
	logBoardClientTimings(logger, "gross-save=250/50/210/224/230~complete,status-load=10/2/5/11/7,status-save=240/60/210,gross-open=190,status-load=30/40/90,unknown=2,status-open=180/bad/150")
	log := output.String()
	if strings.Count(log, "board browser timing") != 3 || strings.Count(log, "completed_edit=true") != 1 || !strings.Contains(log, "response_to_commit_ms=14 commit_to_frame_ms=6 frame_interval_ms=20") || !strings.Contains(log, "operation=status-save duration_ms=240 before_request_ms=60 request_ms=150 response_to_paint_ms=30") ||
		!strings.Contains(log, "operation=gross-open duration_ms=190") {
		t.Fatal(log)
	}
}

func TestBoardDocumentOpeningTiming(t *testing.T) {
	var output strings.Builder
	logBoardClientTimings(slog.New(slog.NewTextHandler(&output, nil)),
		"status-document-open=324,gross-document-open=290,status-document-open=120001")
	log := output.String()
	if strings.Count(log, "board browser timing") != 2 || !strings.Contains(log, "operation=status-document-open duration_ms=324") ||
		!strings.Contains(log, "operation=gross-document-open duration_ms=290") {
		t.Fatal(log)
	}
}

func TestBoardOpeningNetworkTiming(t *testing.T) {
	var output strings.Builder
	logger := slog.New(slog.NewTextHandler(&output, nil))
	logBoardClientTimings(logger, "status-open=300/0/240/270/275~5/10/200/230/130/85000,gross-open=300/0/240/270/275~5/10/245/250/130/85000,status-save=300/0/240/270/275~5/10/200/230/130/85000")
	log := output.String()
	if strings.Count(log, "board browser timing") != 3 || strings.Count(log, "ttfb_ms=") != 1 ||
		!strings.Contains(log, "before_fetch_ms=5 fetch_to_request_ms=5 ttfb_ms=190 response_transfer_ms=30 body_to_view_ms=10 server_headers_ms=130 encoded_body_bytes=85000") {
		t.Fatal(log)
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
