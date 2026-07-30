package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

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
	r.Post("/jobs/sync-tolls", handler.syncTolls)
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
