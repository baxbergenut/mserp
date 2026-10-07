package httpapi

import (
	"errors"
	"math/big"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"mserp/internal/repository"
)

func validDriverEscrowAmount(value string) bool {
	value = strings.TrimSpace(value)
	amount, ok := new(big.Rat).SetString(value)
	return ok && amount.Sign() > 0 && expenseAmountPattern.MatchString(value) && !strings.HasPrefix(value, "-")
}

func (h expenseHandler) getDriverEscrowSetting(w http.ResponseWriter, r *http.Request) {
	value, err := h.repo.GetDriverEscrowSetting(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h expenseHandler) saveDriverEscrowSetting(w http.ResponseWriter, r *http.Request) {
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

func (h expenseHandler) listSettings(w http.ResponseWriter, r *http.Request) {
	values, err := h.repo.ListExpenseSettings(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (h expenseHandler) saveSetting(w http.ResponseWriter, r *http.Request) {
	id := ""
	if r.Method == http.MethodPut {
		var ok bool
		id, ok = pathID(w, r)
		if !ok {
			return
		}
	}
	var input repository.ExpenseSetting
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateOptionalUUID(input.CategoryID, "category id"); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.repo.SaveExpenseSetting(r.Context(), id, input)
	if err != nil {
		var pg *pgconn.PgError
		switch {
		case errors.Is(err, repository.ErrExpenseSettingConflict):
			writeAPIError(w, http.StatusConflict, err.Error())
		case errors.Is(err, repository.ErrExpenseSettingInvalid):
			writeAPIError(w, http.StatusBadRequest, "Enter a name of 1–100 characters and a valid setting type/category")
		case errors.As(err, &pg) && pg.Code == "23505":
			writeAPIError(w, http.StatusConflict, "This name already exists. Check archived items too.")
		case errors.As(err, &pg) && pg.ConstraintName == "driver_escrow_category":
			writeAPIError(w, http.StatusBadRequest, "Safety is required for driver escrow and cannot be renamed or archived")
		case errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "23503"):
			writeAPIError(w, http.StatusBadRequest, "Choose an active category and a valid setting name")
		default:
			h.writeError(w, err)
		}
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}
