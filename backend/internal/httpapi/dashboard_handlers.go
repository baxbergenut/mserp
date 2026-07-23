package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"mserp/internal/repository"
)

func registerDashboardRoutes(
	r chi.Router,
	logger *slog.Logger,
	repo *repository.DashboardRepository,
) {
	r.Get("/financial-dashboard", func(w http.ResponseWriter, r *http.Request) {
		weekStart, err := parseOptionalDate(r.URL.Query().Get("weekStart"), "weekStart")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if weekStart != nil && weekStart.Weekday() != time.Monday {
			writeAPIError(w, http.StatusBadRequest, "weekStart must be a Monday")
			return
		}

		query := repository.FinancialDashboardQuery{}
		if weekStart != nil {
			query.DateFrom = weekStart
			dateTo := weekStart.AddDate(0, 0, 7)
			query.DateTo = &dateTo
		}

		dashboard, err := repo.GetFinancialDashboard(r.Context(), query)
		if err != nil {
			logger.Error("load financial dashboard failed", "period", strings.TrimSpace(r.URL.Query().Get("weekStart")), "error", err)
			writeAPIError(w, http.StatusInternalServerError, "The financial dashboard could not be loaded.")
			return
		}
		writeJSON(w, http.StatusOK, dashboard)
	})
}
