package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
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
		w.Header().Set("Server-Timing", fmt.Sprintf("app;dur=%.2f, db;dur=%.2f, pool;dur=%.2f, queries;desc=\"%d\"", float64(time.Since(w.start).Microseconds())/1000, float64(w.metrics.QueryNanos.Load())/1e6, float64(w.metrics.AcquireNanos.Load())/1e6, w.metrics.Queries.Load()))
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
func performance(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, m := db.WithMetrics(r.Context())
			start := time.Now()
			counted := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(&timedWriter{ResponseWriter: counted, start: start, metrics: m}, r.WithContext(ctx))
			route := chi.RouteContext(r.Context()).RoutePattern()
			logger.Info("http performance", "method", r.Method, "route", route, "status", counted.Status(), "duration_ms", time.Since(start).Milliseconds(), "db_calls", m.Queries.Load(), "db_ms", float64(m.QueryNanos.Load())/1e6, "pool_ms", float64(m.AcquireNanos.Load())/1e6, "response_bytes", counted.BytesWritten())
		})
	}
}
