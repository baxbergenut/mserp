package httpapi

import (
	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
	"net/http"
)

func (h maintenanceHandler) odometerSources(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	truck := chi.URLParam(r, "truckId")
	if !isUUID(truck) {
		writeAPIError(w, 400, "select a truck")
		return
	}
	out, err := h.repo.OdometerSources(r.Context(), truck)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h maintenanceHandler) odometerSourceSave(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	truck := chi.URLParam(r, "truckId")
	if !isUUID(truck) {
		writeAPIError(w, 400, "select a truck")
		return
	}
	var in repository.OdometerSourceInput
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	in.ActorID = session.User.ID
	in.ActorName = session.User.Username
	if err := h.repo.SaveOdometerSource(r.Context(), truck, in); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
