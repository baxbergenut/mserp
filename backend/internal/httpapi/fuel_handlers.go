package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"mserp/internal/jobs"
	"mserp/internal/repository"
)

func registerFuelRoutes(
	r chi.Router,
	logger *slog.Logger,
	job *jobs.SyncFuelJob,
	repo *repository.FuelRepository,
) {
	r.Get("/fuel-dashboard", func(w http.ResponseWriter, r *http.Request) {
		dateFrom, err := parseOptionalDate(r.URL.Query().Get("dateFrom"), "dateFrom")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		dateTo, err := parseOptionalDate(r.URL.Query().Get("dateTo"), "dateTo")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		location, err := time.LoadLocation("America/New_York")
		if err != nil {
			logger.Error("load reporting timezone failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "Failed to load fuel dashboard.")
			return
		}
		today := time.Now().In(location)
		if dateFrom == nil {
			value := time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, location)
			dateFrom = &value
		}
		if dateTo == nil {
			value := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
			dateTo = &value
		}
		if dateFrom.After(*dateTo) {
			writeAPIError(w, http.StatusBadRequest, "dateFrom cannot be after dateTo")
			return
		}
		dashboard, err := repo.GetDashboard(r.Context(), repository.FuelDashboardQuery{
			Year: today.Year(), MapDateFrom: *dateFrom, MapDateTo: *dateTo,
		})
		if err != nil {
			logger.Error("load fuel dashboard failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "Failed to load fuel dashboard.")
			return
		}
		writeJSON(w, http.StatusOK, dashboard)
	})

	r.Get("/fuel-transactions", func(w http.ResponseWriter, r *http.Request) {
		if wantsPagination(r) {
			pagination, err := parsePagination(r)
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			dateFrom, err := parseOptionalDate(r.URL.Query().Get("dateFrom"), "dateFrom")
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			dateTo, err := parseOptionalDate(r.URL.Query().Get("dateTo"), "dateTo")
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			transactions, err := repo.ListTransactionsPage(r.Context(), repository.FuelPageQuery{
				Pagination: pagination, Search: strings.TrimSpace(r.URL.Query().Get("search")),
				Driver: r.URL.Query().Get("driver"), State: r.URL.Query().Get("state"),
				Category: r.URL.Query().Get("category"), DateFrom: dateFrom, DateTo: dateTo,
			})
			if err != nil {
				logger.Error("list paginated fuel transactions failed", "error", err)
				writeAPIError(w, http.StatusInternalServerError, "Failed to load fuel transactions.")
				return
			}
			writeJSON(w, http.StatusOK, transactions)
			return
		}
		transactions, err := repo.ListTransactions(r.Context())
		if err != nil {
			logger.Error("list fuel transactions failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "Failed to load fuel transactions.")
			return
		}
		writeJSON(w, http.StatusOK, transactions)
	})

	r.Post("/jobs/sync-fuel", func(w http.ResponseWriter, r *http.Request) {
		result, err := job.Run(r.Context())
		if err != nil {
			logger.Error("sync Relay fuel failed", "error", err)
			writeAPIError(w, http.StatusBadGateway, "Relay fuel sync failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
