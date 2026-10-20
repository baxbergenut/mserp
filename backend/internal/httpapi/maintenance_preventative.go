package httpapi

import (
	"mserp/internal/repository"
	"net/http"
)

func (h maintenanceHandler) preventative(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	service := r.URL.Query().Get("serviceCode")
	// Accept the retired selector for older clients; all reads use the PM cycle.
	if service != "" && service != "pm" && service != "oil" {
		writeAPIError(w, 400, "select PM Service")
		return
	}
	p, err := parsePagination(r)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	v, err := h.repo.Preventative(r.Context(), r.URL.Query().Get("search"), r.URL.Query().Get("includeInactive") == "true", p.Page, p.PageSize)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h maintenanceHandler) pmBaseline(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in repository.MaintenancePMInput
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	if in.RecordID != nil && !isUUID(*in.RecordID) || in.RecordVersion < 0 || in.BaselineVersion < 0 {
		writeAPIError(w, 400, "invalid PM version")
		return
	}
	in.TruckID = id
	session, _ := authSessionFromContext(r.Context())
	in.ActorID = session.User.ID
	in.ActorName = session.User.Username
	if err := h.repo.SavePMBaseline(r.Context(), in); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
