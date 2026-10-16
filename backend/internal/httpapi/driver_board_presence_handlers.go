package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func registerBoardPresenceRoutes(r chi.Router, logger *slog.Logger, repo *repository.DriverBoardRepository) {
	fail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, repository.ErrDriverBoardConflict) {
			writeAPIError(w, 409, "This cell or tab is no longer available")
			return
		}
		logger.Error("board presence failed", "error", err)
		writeAPIError(w, 503, "Live presence is temporarily unavailable")
	}
	r.Get("/driver-board/presence", func(w http.ResponseWriter, r *http.Request) {
		rows, err := repo.Presence(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, rows)
	})
	r.Put("/driver-board/presence", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			ClientID string `json:"clientId"`
			DriverID string `json:"driverId"`
			Column   string `json:"column"`
		}
		if err := decodeJSON(r, &p); err != nil || !isUUID(p.ClientID) || !isUUID(p.DriverID) {
			writeAPIError(w, 400, "Valid tab and driver IDs are required")
			return
		}
		switch p.Column {
		case "currentLoad", "driver", "truck", "trailerNumber", "originalGross", "driverGross", "phone", "status", "destination", "notes", "eta", "loadPlan", "homeTime":
		default:
			writeAPIError(w, 400, "Invalid board column")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeAPIError(w, 401, "Session required")
			return
		}
		if err = repo.SetPresence(r.Context(), p.ClientID, session.User.ID, hashToken(cookie.Value), p.DriverID, p.Column); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Delete("/driver-board/presence/{clientId}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "clientId")
		if !isUUID(id) {
			writeAPIError(w, 400, "Invalid tab ID")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeAPIError(w, 401, "Session required")
			return
		}
		if err = repo.ClearPresence(r.Context(), id, session.User.ID, hashToken(cookie.Value)); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
}
