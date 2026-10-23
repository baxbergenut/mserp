package httpapi

import (
	"mserp/internal/repository"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h fleetHandler) fleetStatusCatalog(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.FleetStatusCatalog(r.Context())
	if e != nil {
		h.writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h fleetHandler) fleetStatusHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	kind := "driver"
	if strings.HasPrefix(r.URL.Path, "/trucks/") {
		kind = "truck"
	}
	q := r.URL.Query()
	page := 1
	var err error
	if q.Get("page") != "" {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 || page > 10000 {
			writeAPIError(w, 400, "Invalid history page")
			return
		}
	}
	snapshot := int64(0)
	if q.Get("snapshot") != "" {
		snapshot, err = strconv.ParseInt(q.Get("snapshot"), 10, 64)
		if err != nil || snapshot < 0 {
			writeAPIError(w, 400, "Invalid history snapshot")
			return
		}
	}
	for _, key := range []string{"from", "to"} {
		if q.Get(key) != "" {
			if _, err = time.Parse(time.DateOnly, q.Get(key)); err != nil {
				writeAPIError(w, 400, "Invalid history date")
				return
			}
		}
	}
	v, e := h.repo.StatusHistory(r.Context(), kind, id, q.Get("track"), q.Get("status"), q.Get("from"), q.Get("to"), page, snapshot)
	if e != nil {
		h.writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h fleetHandler) changeAvailability(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in repository.FleetStatusChange
	if decodeJSON(r, &in) != nil || !validFleetStatusInput(in) {
		writeAPIError(w, 400, "Choose a valid status, version and effective time")
		return
	}
	switch in.Status {
	case "active", "home", "vacation", "rest", "reset", "family_issue", "health_issue", "new_driver":
	default:
		writeAPIError(w, 400, "Invalid driver availability")
		return
	}
	if err := in.LeavePlan.Validate(); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	v, e := h.repo.ChangeAvailability(r.Context(), id, session.User.ID, in)
	if e != nil {
		h.writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h fleetHandler) changeTruckCondition(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in repository.FleetStatusChange
	if decodeJSON(r, &in) != nil || !validFleetStatusInput(in) {
		writeAPIError(w, 400, "Choose a valid status, version and effective time")
		return
	}
	switch in.Status {
	case "ready", "shop", "out_of_service", "hold":
	default:
		writeAPIError(w, 400, "Invalid truck condition")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	v, e := h.repo.ChangeTruckCondition(r.Context(), id, session.User.ID, in)
	if e != nil {
		h.writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func validFleetStatusInput(in repository.FleetStatusChange) bool {
	return in.Version > 0 && len(in.Reason) <= 1000 && (in.EffectiveAt.IsZero() || (!in.EffectiveAt.After(time.Now()) && in.EffectiveAt.Year() >= 2000))
}
