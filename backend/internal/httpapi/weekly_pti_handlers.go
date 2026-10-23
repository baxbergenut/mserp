package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func (h fleetHandler) ptiWeek(w http.ResponseWriter, r *http.Request) {
	week := r.URL.Query().Get("weekStart")
	if _, err := grossBoardWeek(week); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	value, err := h.repo.PTIWeek(r.Context(), week)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (h fleetHandler) ptiMatrix(w http.ResponseWriter, r *http.Request) {
	value, err := h.repo.PTIMatrix(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (h fleetHandler) ptiLayout(w http.ResponseWriter, r *http.Request) {
	var in repository.PTILayoutChange
	if decodeJSON(r, &in) != nil {
		writeAPIError(w, 400, "Invalid PTI layout")
		return
	}
	for _, p := range in.Placements {
		if !isUUID(p.TruckID) {
			writeAPIError(w, 400, "Invalid truck")
			return
		}
	}
	session, _ := authSessionFromContext(r.Context())
	version, err := h.repo.SavePTILayout(r.Context(), session.User.ID, in)
	if errors.Is(err, repository.ErrPTIConflict) {
		writeAPIError(w, 409, "PTI layout or active trucks changed. Reload and try again.")
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"version": version})
}
func (h fleetHandler) createPTIWeek(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WeekStart string `json:"weekStart"`
	}
	if decodeJSON(r, &in) != nil {
		writeAPIError(w, 400, "Invalid weekly PTI request")
		return
	}
	if _, err := grossBoardWeek(in.WeekStart); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if err := h.repo.CreatePTIWeek(r.Context(), in.WeekStart, session.User.ID); err != nil {
		h.writeError(w, err)
		return
	}
	value, err := h.repo.PTIWeek(r.Context(), in.WeekStart)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (h fleetHandler) changePTI(w http.ResponseWriter, r *http.Request) {
	week, truck := chi.URLParam(r, "week"), chi.URLParam(r, "truck")
	if _, err := grossBoardWeek(week); err != nil || !isUUID(truck) {
		writeAPIError(w, 400, "Invalid PTI week or truck")
		return
	}
	var in repository.PTIChange
	if decodeJSON(r, &in) != nil || !isUUID(in.RequestID) || (in.UndoRequestID != "" && !isUUID(in.UndoRequestID)) {
		writeAPIError(w, 400, "Invalid PTI change request")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	value, err := h.repo.ChangePTI(r.Context(), week, truck, session.User.ID, in)
	if errors.Is(err, repository.ErrPTIConflict) {
		writeAPIError(w, 409, err.Error())
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (h fleetHandler) ptiHistory(w http.ResponseWriter, r *http.Request) {
	week, truck := chi.URLParam(r, "week"), chi.URLParam(r, "truck")
	if _, err := grossBoardWeek(week); err != nil || !isUUID(truck) {
		writeAPIError(w, 400, "Invalid PTI week or truck")
		return
	}
	value, err := h.repo.PTIHistory(r.Context(), week, truck)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
