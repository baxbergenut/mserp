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
