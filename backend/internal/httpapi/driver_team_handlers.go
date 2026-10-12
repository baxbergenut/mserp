package httpapi

import (
	"mserp/internal/repository"
	"net/http"
	"strings"
)

func (handler fleetHandler) driverTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		value, err := handler.repo.GetDriverTeam(r.Context(), id)
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	var input repository.DriverTeamInput
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.TeamDriverID = strings.TrimSpace(input.TeamDriverID)
	if input.TeamDriverID != "" && !isUUID(input.TeamDriverID) {
		writeAPIError(w, http.StatusBadRequest, "invalid team driver id")
		return
	}
	week, err := grossBoardWeek(input.WeekStart)
	if err != nil || input.WeekStart == "" || input.Version < 0 {
		writeAPIError(w, http.StatusBadRequest, "select an effective Monday and a valid team version")
		return
	}
	input.WeekStart = week.Format("2006-01-02")
	session, _ := authSessionFromContext(r.Context())
	value, err := handler.repo.SetDriverTeam(r.Context(), id, input, session.User.ID)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
