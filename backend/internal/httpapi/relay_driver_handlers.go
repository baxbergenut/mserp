package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"mserp/internal/phone"
	"mserp/internal/relay"
	"mserp/internal/repository"
)

var relayExternalID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

func validateRelayChange(in *repository.RelayDriverChange) bool {
	if !isUUID(in.RequestID) || !slices.Contains([]string{"add", "edit", "remove", "enable", "disable", "assign", "unassign"}, in.Action) {
		return false
	}
	if in.Action == "add" {
		if !isUUID(in.DriverID) || in.RelayDriverID != "" {
			return false
		}
	} else if !relayExternalID.MatchString(in.RelayDriverID) || len(in.Revision) != 64 {
		return false
	}
	if in.Action == "assign" && !relayExternalID.MatchString(in.PolicyID) {
		return false
	}
	if in.Action != "add" && in.DriverID != "" {
		return false
	}
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)
	in.Email = strings.TrimSpace(in.Email)
	in.DriverPIN = strings.TrimSpace(in.DriverPIN)
	in.TruckNumber = strings.TrimSpace(in.TruckNumber)
	if len(in.DriverPIN) > 100 || len(in.TruckNumber) > 100 {
		return false
	}
	if in.Action == "add" || in.Action == "edit" {
		if in.FirstName == "" || in.LastName == "" || len(in.FirstName) > 150 || len(in.LastName) > 150 || len(in.Email) > 254 {
			return false
		}
		if in.Email != "" {
			address, err := mail.ParseAddress(in.Email)
			if err != nil || address.Address != in.Email {
				return false
			}
		}
		if in.Action == "add" {
			number, err := phone.Normalize(in.Phone)
			if err != nil || number == "" {
				return false
			}
			in.Phone = "+1" + number
		}
	}
	return true
}

func registerRelayDriverRoutes(router chi.Router, logger *slog.Logger, repo *repository.FuelRepository) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.RelayDriverValidation
		var upstream *relay.APIError
		switch {
		case errors.As(err, &invalid):
			writeAPIError(w, 400, err.Error())
		case errors.Is(err, repository.ErrRelayDriverConflict), errors.Is(err, repository.ErrRelayReviewConflict):
			writeAPIError(w, 409, err.Error())
		case errors.Is(err, repository.ErrRelayNotConfigured):
			writeAPIError(w, 503, err.Error())
		case errors.Is(err, repository.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
			writeAPIError(w, 404, "Relay account or MSERP driver was not found")
		case errors.As(err, &upstream):
			writeAPIError(w, 502, err.Error())
		default:
			logger.Error("Relay driver management failed", "error", err)
			writeAPIError(w, 500, "Relay drivers could not be loaded or saved. Refresh before retrying.")
		}
	}
	router.Get("/relay-drivers", func(w http.ResponseWriter, r *http.Request) {
		data, err := repo.RelayDrivers(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		session, _ := authSessionFromContext(r.Context())
		data.CanLink = data.CanLink && slices.Contains(session.User.Permissions, "tasks.write")
		if !data.CanLink {
			for i := range data.Items {
				data.Items[i].Suggestions = []repository.RelaySuggestion{}
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, data)
	})
	router.Post("/relay-drivers/refresh", func(w http.ResponseWriter, r *http.Request) {
		if err := repo.RefreshRelayDrivers(r.Context()); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"refreshed": true})
	})
	router.Post("/relay-drivers/change", func(w http.ResponseWriter, r *http.Request) {
		var in repository.RelayDriverChange
		if decodeJSON(r, &in) != nil || !validateRelayChange(&in) {
			writeAPIError(w, 400, "Provide a valid Relay action, driver, revision and required contact details.")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.ChangeRelayDriver(r.Context(), in, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	router.Post("/relay-drivers/{id}/link", func(w http.ResponseWriter, r *http.Request) {
		session, _ := authSessionFromContext(r.Context())
		if !slices.Contains(session.User.Permissions, "fuel.read") {
			writeAPIError(w, 403, "Fuel read permission is required")
			return
		}
		var in struct {
			DriverID string `json:"driverId"`
		}
		id := chi.URLParam(r, "id")
		if decodeJSON(r, &in) != nil || !isUUID(in.DriverID) || !relayExternalID.MatchString(id) {
			writeAPIError(w, 400, "Choose a Relay account and MSERP driver")
			return
		}
		n, err := repo.LinkRelayRosterDriver(r.Context(), id, in.DriverID, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]int64{"transactionsLinked": n})
	})
	router.Post("/relay-drivers/operations/{id}/verify", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Reviewed bool `json:"reviewed"`
		}
		id := chi.URLParam(r, "id")
		if decodeJSON(r, &in) != nil || !isUUID(id) || !in.Reviewed {
			writeAPIError(w, 400, "Verify the result in Relay before acknowledging this request")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.VerifyRelayOperation(r.Context(), id, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"verified": true})
	})
}
