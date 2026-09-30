package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"

	"mserp/internal/repository"
)

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
