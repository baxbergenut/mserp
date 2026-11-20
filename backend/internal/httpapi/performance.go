package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"mserp/internal/db"
)

type timedWriter struct {
	http.ResponseWriter
	start   time.Time
	metrics *db.Metrics
	wrote   bool
}

func (w *timedWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		phases, _ := w.metrics.Details()
		lockWait := phases["financial_lock_wait"] + phases["receipt_lock_wait"] + phases["resource_lock_wait"]
		w.Header().Set("Server-Timing", fmt.Sprintf("app;dur=%.2f, db;dur=%.2f, pool;dur=%.2f, lock;dur=%.2f, queries;desc=\"%d\", payroll_calculations;desc=\"%d\"", float64(time.Since(w.start).Microseconds())/1000, float64(w.metrics.QueryNanos.Load())/1e6, float64(w.metrics.AcquireNanos.Load())/1e6, lockWait, w.metrics.Queries.Load(), w.metrics.Events()["payroll_calculation"]))
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *timedWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *timedWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *timedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Browser timings ride on the next ordinary request and carry no identifiers.
// Only bounded numeric observations and known operation names reach the log.
func logBoardClientTimings(logger *slog.Logger, raw string) {
	if raw == "" || len(raw) > 512 {
		return
	}
	samples := strings.Split(raw, ",")
	if len(samples) > 12 {
		return
	}
	for _, sample := range samples {
		name, value, ok := strings.Cut(sample, "=")
		if !ok {
			continue
		}
		switch name {
		case "status-open", "status-save", "status-load", "gross-open", "gross-save":
		default:
			continue
		}
		value, network, _ := strings.Cut(value, "~")
		total, phases, hasPhases := strings.Cut(value, "/")
		ms, err := strconv.Atoi(total)
		if err != nil || ms < 0 || ms > 120000 {
			continue
		}
		if !hasPhases {
			logger.Info("board browser timing", "operation", name, "duration_ms", ms)
			continue
		}
		parts := strings.Split(phases, "/")
		if len(parts) != 2 && len(parts) != 4 {
			continue
		}
		points := make([]int, 0, len(parts))
		previous := 0
		for _, part := range parts {
			point, err := strconv.Atoi(part)
			if err != nil || point < previous || point > ms {
				break
			}
			points = append(points, point)
			previous = point
		}
		if len(points) != len(parts) {
			continue
		}
		fields := []any{"operation", name, "duration_ms", ms, "before_request_ms", points[0], "request_ms", points[1] - points[0], "response_to_paint_ms", ms - points[1]}
		if len(points) == 4 {
			fields = append(fields, "response_to_commit_ms", points[2]-points[1], "commit_to_frame_ms", points[3]-points[2], "frame_interval_ms", ms-points[3])
		}
		if name == "status-open" || name == "gross-open" {
			fields = appendOpeningNetwork(fields, network, points[1])
		}
		logger.Info("board browser timing", fields...)
	}
}

// Times are relative to the same opening as the React phases. TTFB includes
// the server's headers-ready duration. Values from other requests are omitted.
func appendOpeningNetwork(fields []any, network string, received int) []any {
	parts := strings.Split(network, "/")
	if len(parts) != 6 {
		return fields
	}
	var n [6]int
	for i, part := range parts {
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 {
			return fields
		}
		n[i] = v
	}
	if n[0] > n[1] || n[1] > n[2] || n[2] > n[3] || n[3] > received || n[4] > 120000 || n[5] > 100<<20 {
		return fields
	}
	return append(fields, "before_fetch_ms", n[0], "fetch_to_request_ms", n[1]-n[0],
		"ttfb_ms", n[2]-n[1], "response_transfer_ms", n[3]-n[2], "body_to_view_ms", received-n[3],
		"server_headers_ms", n[4], "encoded_body_bytes", n[5])
}

func performance(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, m := db.WithMetrics(r.Context())
			start := time.Now()
			counted := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(&timedWriter{ResponseWriter: counted, start: start, metrics: m}, r.WithContext(ctx))
			route := chi.RouteContext(r.Context()).RoutePattern()
			if counted.Status() >= 200 && counted.Status() < 300 && (strings.HasPrefix(route, "/driver-board") || strings.HasPrefix(route, "/gross-board")) {
				logBoardClientTimings(logger, r.Header.Get("X-Clustie-Board-Timing"))
			}
			phases, _ := m.Details()
			scenario := "default"
			if strings.HasPrefix(route, "/driver-pay") || strings.HasPrefix(route, "/investor-pay") {
				scenario = payrollPerformanceScenario(r)
			}
			if route == "/dashboard/summary" {
				switch r.URL.Query().Get("view") {
				case "current":
					scenario = "current"
				case "weekly":
					scenario = "weekly"
				}
				if r.URL.Query().Get("sections") != "" {
					scenario += "_primary"
				}
			}
			logger.Info("http performance", "scenario", scenario, "phases_ms", phases, "events", m.Events(), "method", r.Method, "route", route, "status", counted.Status(), "duration_ms", time.Since(start).Milliseconds(), "db_calls", m.Queries.Load(), "pool_acquires", m.Acquires.Load(), "pool_hold_ms", float64(m.HoldNanos.Load())/1e6, "batch_sends", m.Batches.Load(), "db_ms", float64(m.QueryNanos.Load())/1e6, "pool_ms", float64(m.AcquireNanos.Load())/1e6, "response_bytes", counted.BytesWritten())
		})
	}
}

func payrollPerformanceScenario(r *http.Request) string {
	q := r.URL.Query()
	if r.Method == http.MethodPost {
		if strings.HasSuffix(r.URL.Path, "/finalize") || strings.HasSuffix(r.URL.Path, "/reopen") {
			return "settlement"
		}
		return "source_action"
	}
	if r.Method == http.MethodPut {
		return "save"
	}
	if strings.Contains(r.URL.Path, "/statements/") {
		return "statement_detail"
	}
	if q.Get("summary") == "1" {
		if q.Get("detailIds") != "" {
			return "summary_with_details"
		}
		return "summary"
	}
	if q.Get("statementId") != "" {
		return "statement_detail"
	}
	return "full_report"
}
