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

type tollHandler struct {
	logger *slog.Logger
	repo   *repository.TollRepository
	job    *jobs.SyncTollsJob
}

func registerTollRoutes(
	r chi.Router,
	logger *slog.Logger,
	job *jobs.SyncTollsJob,
	repo *repository.TollRepository,
) {
	handler := tollHandler{logger: logger, repo: repo, job: job}
	r.Get("/tolls", handler.listTolls)
	r.Get("/toll-dashboard", handler.tollDashboard)
	r.Post("/jobs/sync-tolls", handler.syncTolls)
}

func (handler tollHandler) tollDashboard(w http.ResponseWriter, r *http.Request) {
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
		handler.logger.Error("load reporting timezone failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "Failed to load toll overview.")
		return
	}
	now := time.Now().In(location)
	if dateFrom == nil {
		value := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
		dateFrom = &value
	}
	if dateTo == nil {
		value := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		dateTo = &value
	}
	if dateFrom.After(*dateTo) {
		writeAPIError(w, http.StatusBadRequest, "dateFrom cannot be after dateTo")
		return
	}
	if dateTo.After(dateFrom.AddDate(5, 0, 0)) {
		writeAPIError(w, http.StatusBadRequest, "Select a date range of five years or less")
		return
	}
	dashboard, err := handler.repo.GetDashboard(r.Context(), *dateFrom, *dateTo)
	if err != nil {
		handler.logger.Error("load toll dashboard failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "Failed to load toll overview.")
		return
	}
	writeJSON(w, http.StatusOK, dashboard)
}

func (handler tollHandler) listTolls(w http.ResponseWriter, r *http.Request) {
	if wantsPagination(r) {
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		postFrom, err := parseOptionalDate(r.URL.Query().Get("postFrom"), "postFrom")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		postTo, err := parseOptionalDate(r.URL.Query().Get("postTo"), "postTo")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		value, err := handler.repo.ListTollsPage(r.Context(), repository.TollPageQuery{
			Pagination: pagination, Search: strings.TrimSpace(r.URL.Query().Get("search")),
			Unit: r.URL.Query().Get("unit"), Agency: r.URL.Query().Get("agency"),
			PostFrom: postFrom, PostTo: postTo,
		})
		if err != nil {
			handler.logger.Error("list paginated tolls failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "the tolls could not be loaded")
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	values, err := handler.repo.ListTolls(r.Context())
	if err != nil {
		handler.logger.Error("list tolls failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "the tolls could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (handler tollHandler) syncTolls(w http.ResponseWriter, r *http.Request) {
	result, err := handler.job.Run(r.Context())
	if err != nil {
		handler.logger.Error("sync PrePass tolls failed", "error", err)
		writeAPIError(w, http.StatusBadGateway, "PrePass toll sync failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
