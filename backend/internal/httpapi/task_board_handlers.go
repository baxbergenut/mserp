package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func registerTaskBoardRoutes(r chi.Router, logger *slog.Logger, repo *repository.CustomTaskRepository) {
	fail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, 404, "This task is no longer available")
			return
		}
		logger.Error("task board operation failed", "error", err)
		writeAPIError(w, 500, "Tasks could not be loaded or saved")
	}
	r.Get("/tasks", func(w http.ResponseWriter, r *http.Request) {
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "open"
		}
		if status != "all" && status != "open" && status != "in_process" && status != "completed" {
			writeAPIError(w, 400, "status must be open, in_process, completed or all")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		result, err := repo.Board(r.Context(), pagination, strings.TrimSpace(r.URL.Query().Get("search")), status, slices.Contains(session.User.Permissions, "fleet.read"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, result)
	})
	r.Get("/tasks/count", func(w http.ResponseWriter, r *http.Request) {
		session, _ := authSessionFromContext(r.Context())
		count, err := repo.OpenCount(r.Context(), slices.Contains(session.User.Permissions, "fleet.read"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]int{"count": count})
	})
	r.Post("/tasks/offboarding/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			writeAPIError(w, 400, "Provide a valid task ID")
			return
		}
		var input struct {
			Equipment  bool `json:"equipment"`
			Access     bool `json:"access"`
			Settlement bool `json:"settlement"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if err := decodeJSON(r, &input); err != nil || !input.Equipment || !input.Access || !input.Settlement {
			writeAPIError(w, 400, "Confirm equipment, external access and final settlement review")
			return
		}
		if err := repo.ConfirmOffboarding(r.Context(), id); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
}
