package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/repository"
)

// Notifications carry no financial data. Normal authorized snapshot reads still
// determine freshness; stream reconnects and page focus recover missed hints.
func registerPayrollLive(r chi.Router, pool *pgxpool.Pool, logger *slog.Logger, auth *authHandler, pay *repository.DriverPayRepository) func() {
	events := []*taskEvents{}
	for _, kind := range []string{"driver", "investor"} {
		event := newDatabaseEvents(pool, logger, "payroll_projection_visible", kind+"_pay.read")
		events = append(events, event)
		r.Get("/"+kind+"-pay/events", func(w http.ResponseWriter, r *http.Request) {
			if !pay.PreparedReadsEnabled() {
				writeAPIError(w, http.StatusNotFound, "Prepared payroll is not enabled")
				return
			}
			event.serve(auth)(w, r)
		})
	}
	return func() {
		for _, event := range events {
			event.shutdown()
		}
	}
}
