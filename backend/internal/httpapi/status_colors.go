package httpapi

import (
	"errors"
	"mserp/internal/repository"
	"net/http"
)

func (h fleetHandler) statusColors(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		result, err := h.repo.StatusColors(r.Context())
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	var input repository.StatusColors
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, 400, "Invalid status color settings")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	result, err := h.repo.SaveStatusColors(r.Context(), input, session.User.ID)
	if errors.Is(err, repository.ErrStatusColorsConflict) {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
