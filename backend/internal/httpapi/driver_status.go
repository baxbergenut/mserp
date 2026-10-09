package httpapi

import (
	"net/http"
	"time"

	"mserp/internal/repository"
)

func (handler fleetHandler) changeDriverStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input repository.DriverStatusChange
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, 400, "Invalid status change")
		return
	}
	if input.Status != "active" && input.Status != "vacation" && input.Status != "home" && input.Status != "terminated" {
		writeAPIError(w, 400, "Invalid driver status")
		return
	}
	if input.UpdatedAt.IsZero() {
		writeAPIError(w, 400, "Reload the driver before changing status")
		return
	}
	if _, err := grossBoardWeek(input.AssignmentWeek); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	if input.Status == "terminated" {
		date, err := time.Parse(time.DateOnly, input.TerminationDate)
		location, _ := time.LoadLocation("America/New_York")
		if err != nil || date.Year() < 2000 || input.TerminationDate > time.Now().In(location).Format(time.DateOnly) {
			writeAPIError(w, 400, "Choose a valid termination date that is not in the future")
			return
		}
		if _, err := grossBoardWeek(input.ChargePauseWeek); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
	}
	session, _ := authSessionFromContext(r.Context())
	value, err := handler.repo.ChangeDriverStatus(r.Context(), id, session.User.ID, input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
