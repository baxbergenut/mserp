package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/prepass"
	"mserp/internal/repository"
)

var prepassAccount = regexp.MustCompile(`^[0-9]{1,30}$`)
var prepassRevision = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validatePrePassFleetChange(in *repository.PrePassFleetChange, link bool) bool {
	in.VIN = strings.ToUpper(strings.TrimSpace(in.VIN))
	if !isUUID(in.RequestID) || !prepassAccount.MatchString(in.AccountNumber) || !prepass.FleetVIN.MatchString(in.VIN) {
		return false
	}
	if link {
		if in.Action != "link" {
			return false
		}
		if _, err := time.Parse(time.DateOnly, in.TollFrom); err != nil {
			return false
		}
	} else if !slices.Contains([]string{"add", "edit", "remove", "assign"}, in.Action) {
		return false
	}
	if in.Action != "add" && !prepassRevision.MatchString(in.Revision) {
		return false
	}
	if in.PlateSource != "" && (in.Action != "edit" || !slices.Contains([]string{"mserp", "prepass"}, in.PlateSource)) {
		return false
	}
	if in.Action == "add" || in.Action == "link" || in.PlateSource != "" {
		if !isUUID(in.TruckID) || in.TruckUpdatedAt == "" {
			return false
		}
	} else if in.TruckID != "" || in.TruckUpdatedAt != "" {
		return false
	}
	if in.Action == "assign" {
		if !prepass.TransponderNumber.MatchString(in.TransponderNumber) {
			return false
		}
	} else if in.TransponderNumber != "" {
		return false
	}
	return true
}
func registerPrePassFleetRoutes(router chi.Router, logger *slog.Logger, repo *repository.TollRepository) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.PrePassFleetValidation
		var upstream *prepass.FleetAPIError
		switch {
		case errors.Is(err, repository.ErrPrePassFleetConflict):
			writeAPIError(w, 409, err.Error())
		case errors.As(err, &invalid):
			writeAPIError(w, 400, err.Error())
		case errors.Is(err, repository.ErrPrePassFleetNotConfigured):
			writeAPIError(w, 503, err.Error())
		case errors.Is(err, repository.ErrNotFound):
			writeAPIError(w, 404, "Clustie truck was not found")
		case errors.As(err, &upstream):
			writeAPIError(w, 502, err.Error())
		default:
			logger.Error("PrePass management failed", "error", err)
			writeAPIError(w, 500, "PrePass could not be loaded or saved. Refresh and review unresolved requests before retrying.")
		}
	}
	router.Get("/prepass-fleet", func(w http.ResponseWriter, r *http.Request) {
		data, err := repo.PrePassFleet(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, data)
	})
	router.Post("/prepass-fleet/refresh", func(w http.ResponseWriter, r *http.Request) {
		if err := repo.RefreshPrePassFleet(r.Context()); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"refreshed": true})
	})
	router.Post("/prepass-fleet/link/preview", func(w http.ResponseWriter, r *http.Request) {
		session, _ := authSessionFromContext(r.Context())
		if !slices.Contains(session.User.Permissions, "tolls.read") {
			writeAPIError(w, 403, "Tolls read permission is required")
			return
		}
		var in repository.PrePassFleetChange
		if decodeJSON(r, &in) != nil || !validatePrePassFleetChange(&in, true) {
			writeAPIError(w, 400, "Select a truck and toll linking start date.")
			return
		}
		out, err := repo.PreviewPrePassFleetLink(r.Context(), in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	change := func(link bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			session, _ := authSessionFromContext(r.Context())
			if !slices.Contains(session.User.Permissions, "tolls.read") {
				writeAPIError(w, 403, "Tolls read permission is required")
				return
			}
			var in repository.PrePassFleetChange
			if decodeJSON(r, &in) != nil || !validatePrePassFleetChange(&in, link) {
				writeAPIError(w, 400, "Provide a valid PrePass action, account, VIN and current revision.")
				return
			}
			if link && !prepassRevision.MatchString(in.TollRevision) {
				writeAPIError(w, 400, "Preview the toll link before saving.")
				return
			}
			if !link && in.PlateSource != "prepass" && !authorizeVendorAction(w, r, "tolls.fleet", in.Action) {
				return
			}
			if in.PlateSource != "" && !slices.Contains(session.User.Permissions, "trucks.edit") {
				writeAPIError(w, 403, "Truck edit permission is required to resolve plates in both systems")
				return
			}
			if err := repo.ChangePrePassFleet(r.Context(), in, session.User.ID); err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, 200, map[string]bool{"saved": true})
		}
	}
	router.Post("/prepass-fleet/change", change(false))
	router.Post("/prepass-fleet/link", change(true))
	router.Post("/prepass-fleet/operations/{id}/verify", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Reviewed bool `json:"reviewed"`
		}
		id := chi.URLParam(r, "id")
		if decodeJSON(r, &in) != nil || !in.Reviewed || !isUUID(id) {
			writeAPIError(w, 400, "Check the result in PrePass before acknowledging it")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.VerifyPrePassFleetOperation(r.Context(), id, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"verified": true})
	})
}
