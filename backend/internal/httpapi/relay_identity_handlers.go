package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type relayIdentityStore interface {
	RelayIdentityTasks(context.Context, repository.Pagination, string) (repository.Page[repository.RelayIdentityTask], error)
	ReviewRelayIdentity(context.Context, string, string, string, string) (int64, error)
}

func registerRelayIdentityRoutes(r chi.Router, logger *slog.Logger, repo relayIdentityStore) {
	r.Get("/tasks/relay-identities", func(w http.ResponseWriter, r *http.Request) {
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := repo.RelayIdentityTasks(r.Context(), pagination, strings.TrimSpace(r.URL.Query().Get("search")))
		if err != nil {
			logger.Error("list Relay tasks", "error", err)
			writeAPIError(w, 500, "Relay review tasks could not be loaded")
			return
		}
		writeJSON(w, 200, result)
	})
	r.Post("/tasks/relay-identities/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authSessionFromContext(r.Context())
		if !ok {
			writeAPIError(w, 401, "sign in to review Relay identities")
			return
		}
		var request struct {
			DriverID string `json:"driverId"`
			Action   string `json:"action"`
		}
		id := chi.URLParam(r, "id")
		if err := decodeJSON(r, &request); err != nil || !isUUID(id) || !isUUID(request.DriverID) || (request.Action != "link" && request.Action != "reject") {
			writeAPIError(w, 400, "provide a valid account, driver and link or reject action")
			return
		}
		updated, err := repo.ReviewRelayIdentity(r.Context(), id, request.DriverID, request.Action, session.User.ID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				writeAPIError(w, 404, "Relay account or driver no longer exists")
				return
			}
			if errors.Is(err, repository.ErrRelayReviewConflict) {
				writeAPIError(w, 409, err.Error())
				return
			}
			logger.Error("review Relay identity", "error", err)
			writeAPIError(w, 500, "Relay account could not be reviewed")
			return
		}
		writeJSON(w, 200, map[string]int64{"transactionsLinked": updated})
	})
}
