package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/fleetscope"
	"mserp/internal/repository"
)

type fleetScopeStore interface {
	AcceptFleetScopeHire(context.Context, fleetscope.Event, string) (repository.IntakeResult, error)
}

func fleetScopeWebhook(logger *slog.Logger, store fleetScopeStore, options fleetscope.Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if options.CompanyID == "" || options.Secret == "" || options.Validate() != nil {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, fleetscope.MaxBodyBytes))
		if err != nil {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "webhook body exceeds limit")
			return
		}
		if len(r.Header.Values("X-FleetScope-Timestamp")) != 1 || len(r.Header.Values("X-FleetScope-Signature")) != 1 ||
			!fleetscope.Verify(options.Secret, r.Header.Get("X-FleetScope-Timestamp"), r.Header.Get("X-FleetScope-Signature"), body, time.Now()) {
			writeAPIError(w, http.StatusUnauthorized, "invalid webhook signature or timestamp")
			return
		}
		var event fleetscope.Event
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&event); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid webhook JSON")
			return
		}
		if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "webhook must contain one JSON object")
			return
		}
		if err = event.Validate(); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !strings.EqualFold(event.CompanyID, options.CompanyID) {
			writeAPIError(w, http.StatusForbidden, "company is not authorized for this integration")
			return
		}
		hash := sha256.Sum256(body)
		result, err := store.AcceptFleetScopeHire(r.Context(), event, hex.EncodeToString(hash[:]))
		if errors.Is(err, repository.ErrFleetScopeEventConflict) {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			// Do not log database details: they may include the incoming driver PII.
			logger.Error("FleetScope new-hire receipt failed")
			writeAPIError(w, http.StatusServiceUnavailable, "hire could not be recorded; retry delivery")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func registerDriverIntakeRoutes(r chi.Router, logger *slog.Logger, repo *repository.FleetRepository) {
	handler := fleetHandler{logger: logger, repo: repo}
	r.Get("/driver-directory", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		page, err := repo.ListDriverDirectory(r.Context(), pagination, strings.TrimSpace(r.URL.Query().Get("search")), r.URL.Query().Get("includeInactive") == "true")
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	r.Get("/driver-intake/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		item, err := repo.GetDriverIntake(r.Context(), id)
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	r.Get("/driver-intake", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		page, err := repo.ListDriverIntake(r.Context(), pagination, strings.TrimSpace(r.URL.Query().Get("search")))
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	r.Post("/driver-intake/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var request struct {
			Driver            *driverRequest `json:"driver"`
			LinkDriverID      string         `json:"linkDriverId"`
			SeparateConfirmed bool           `json:"separateConfirmed"`
		}
		if err := decodeJSON(r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid completion request")
			return
		}
		if (request.Driver == nil) == (request.LinkDriverID == "") {
			writeAPIError(w, http.StatusBadRequest, "provide either driver details or linkDriverId")
			return
		}
		var input *repository.DriverInput
		if request.Driver != nil {
			value, err := request.Driver.validate()
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			if value.PayRate <= 0 {
				writeAPIError(w, http.StatusBadRequest, "set a positive pay rate before completing setup")
				return
			}
			input = &value
		} else if !isUUID(request.LinkDriverID) {
			writeAPIError(w, http.StatusBadRequest, "invalid driver id")
			return
		}
		session, ok := r.Context().Value(authContextKey{}).(repository.AuthSession)
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		value, err := repo.CompleteDriverIntake(r.Context(), id, session.User.ID, request.LinkDriverID, input, request.SeparateConfirmed)
		if errors.Is(err, repository.ErrIntakeCompleted) || errors.Is(err, repository.ErrIntakeMatch) {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	})
}
