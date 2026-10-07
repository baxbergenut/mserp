package httpapi

import (
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/repository"
)

type escrowHandler struct {
	logger *slog.Logger
	repo   *repository.EscrowRepository
}

func registerEscrowRoutes(r chi.Router, logger *slog.Logger, repo *repository.EscrowRepository) {
	h := escrowHandler{logger: logger, repo: repo}
	r.Get("/escrows", h.list)
	r.Get("/escrows/settings", h.getDriverEscrowSetting)
	r.Put("/escrows/settings", h.saveDriverEscrowSetting)
	r.Post("/escrows/{id}/releases", h.saveRelease)
	r.Put("/escrows/{id}/releases/{releaseID}", h.saveRelease)
}
func (h escrowHandler) writeError(w http.ResponseWriter, err error) {
	var invalid *repository.ChargeValidationError
	var database *pgconn.PgError
	if errors.As(err, &invalid) {
		writeAPIError(w, 400, err.Error())
		return
	}
	if errors.Is(err, repository.ErrEscrowReleaseConflict) {
		writeAPIError(w, 409, err.Error())
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeAPIError(w, 404, "Escrow record not found")
		return
	}
	if errors.As(err, &database) && database.Code == "23514" {
		writeAPIError(w, 409, database.Message)
		return
	}
	h.logger.Error("escrow request failed", "error", err)
	writeAPIError(w, http.StatusInternalServerError, "Escrow could not be loaded or saved")
}

func (h escrowHandler) saveRelease(w http.ResponseWriter, r *http.Request) {
	var input repository.EscrowReleaseInput
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, 400, "Invalid release request")
		return
	}
	if !isUUID(chi.URLParam(r, "id")) || !isUUID(input.ID) || !validDriverEscrowAmount(input.Amount) ||
		(r.Method == http.MethodPost && input.Version != 0) ||
		(r.Method == http.MethodPut && (input.Version < 1 || input.ID != chi.URLParam(r, "releaseID"))) {
		writeAPIError(w, 400, "Enter a valid release amount and identity")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if err := h.repo.SaveRelease(r.Context(), chi.URLParam(r, "id"), input, session.User.ID); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}
func (h escrowHandler) list(w http.ResponseWriter, r *http.Request) {
	page, err := parsePagination(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "paid" && status != "partial" && status != "unpaid" {
		writeAPIError(w, http.StatusBadRequest, "Unknown escrow status")
		return
	}
	result, err := h.repo.List(r.Context(), repository.EscrowQuery{Pagination: page, Search: strings.TrimSpace(r.URL.Query().Get("search")), DriverID: r.URL.Query().Get("driverId"), Status: status, IncludeInactive: r.URL.Query().Get("includeInactive") == "true"})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h escrowHandler) getDriverEscrowSetting(w http.ResponseWriter, r *http.Request) {
	value, err := h.repo.GetDriverEscrowSetting(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h escrowHandler) saveDriverEscrowSetting(w http.ResponseWriter, r *http.Request) {
	var input repository.DriverEscrowSetting
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.DefaultAmount = strings.TrimSpace(input.DefaultAmount)
	if !validDriverEscrowAmount(input.DefaultAmount) || input.Version < 1 {
		writeAPIError(w, http.StatusBadRequest, "Enter a positive default escrow amount with at most two decimal places")
		return
	}
	value, err := h.repo.SaveDriverEscrowSetting(r.Context(), input)
	if errors.Is(err, repository.ErrDriverEscrowSettingConflict) {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func validDriverEscrowAmount(value string) bool {
	value = strings.TrimSpace(value)
	amount, ok := new(big.Rat).SetString(value)
	return ok && amount.Sign() > 0 && expenseAmountPattern.MatchString(value) && !strings.HasPrefix(value, "-")
}
