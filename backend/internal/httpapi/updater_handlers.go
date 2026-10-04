package httpapi

import (
	"errors"
	"mserp/internal/repository"
	"net/http"
	"strings"
)

func validateExtension(extension *int) error {
	if extension != nil && (*extension < 0 || *extension > 999999) {
		return errors.New("extension must be a whole number between 0 and 999999")
	}
	return nil
}
func validateUpdater(in repository.UpdaterInput) error {
	if strings.TrimSpace(in.FullName) == "" {
		return errors.New("full name is required")
	}
	if in.Shift != "main" && in.Shift != "after_hours" {
		return errors.New("shift must be main or after_hours")
	}
	return validateExtension(in.Extension)
}
func (handler fleetHandler) listUpdaters(w http.ResponseWriter, r *http.Request) {
	values, err := handler.repo.ListUpdaters(r.Context())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}
func (handler fleetHandler) saveUpdater(w http.ResponseWriter, r *http.Request) {
	id := ""
	status := http.StatusCreated
	if r.Method == http.MethodPut {
		var ok bool
		id, ok = pathID(w, r)
		if !ok {
			return
		}
		status = http.StatusOK
	}
	var in repository.UpdaterInput
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateUpdater(in); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.SaveUpdater(r.Context(), id, in)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, status, value)
}
func (handler fleetHandler) deleteUpdater(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := handler.repo.DeleteUpdater(r.Context(), id); err != nil {
		handler.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
