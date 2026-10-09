package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"mserp/internal/repository"
	"mserp/internal/weighmytruck"
)

func registerWeighMyTruckRoutes(r chi.Router, logger *slog.Logger, repo *repository.WeighMyTruckRepository) {
	writeError := func(w http.ResponseWriter, err error) {
		var invalid *repository.WMTValidationError
		var upstream *weighmytruck.Error
		switch {
		case errors.As(err, &invalid):
			writeAPIError(w, 400, err.Error())
		case errors.Is(err, repository.ErrWMTConflict):
			writeAPIError(w, 409, err.Error())
		case errors.Is(err, pgx.ErrNoRows):
			writeAPIError(w, 404, "WeighMyTruck driver not found")
		case errors.As(err, &upstream):
			writeAPIError(w, 502, err.Error())
		default:
			logger.Error("WeighMyTruck persistence failed")
			writeAPIError(w, 500, "WeighMyTruck could not be loaded or saved. Refresh before retrying.")
		}
	}
	r.Get("/weighmytruck", func(w http.ResponseWriter, r *http.Request) {
		result, err := repo.List(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Post("/weighmytruck/change", func(w http.ResponseWriter, r *http.Request) {
		var in repository.WMTChange
		if decodeJSON(r, &in) != nil || in.Version < 0 || (in.Add && !isUUID(in.DriverID)) || (!in.Add && !isUUID(in.MembershipID)) || (in.MembershipID != "" && !isUUID(in.MembershipID)) {
			writeAPIError(w, 400, "Invalid WeighMyTruck change")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.Change(r.Context(), in, session.User.ID); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	r.Post("/weighmytruck/{id}/verify", func(w http.ResponseWriter, r *http.Request) {
		var in repository.WMTVerification
		if decodeJSON(r, &in) != nil || !isUUID(chi.URLParam(r, "id")) || in.Version < 1 {
			writeAPIError(w, 400, "Invalid verification")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.Verify(r.Context(), chi.URLParam(r, "id"), in, session.User.ID); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	r.Post("/weighmytruck/{id}/link", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			DriverID string `json:"driverId"`
			Version  int    `json:"version"`
		}
		if decodeJSON(r, &in) != nil || !isUUID(chi.URLParam(r, "id")) || !isUUID(in.DriverID) || in.Version < 1 {
			writeAPIError(w, 400, "Invalid driver link")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.Link(r.Context(), chi.URLParam(r, "id"), in.DriverID, in.Version, session.User.ID); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
}
