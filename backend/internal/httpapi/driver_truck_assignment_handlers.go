package httpapi

import (
	"mserp/internal/repository"
	"net/http"
	"slices"
)

func (handler fleetHandler) truckAssignmentChoices(w http.ResponseWriter, r *http.Request) {
	session, _ := authSessionFromContext(r.Context())
	if !slices.Contains(session.User.Permissions, "trucks.read") {
		writeAPIError(w, http.StatusForbidden, "Truck read permission is required")
		return
	}
	v, err := handler.repo.TruckAssignmentChoices(r.Context())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (handler fleetHandler) driverTruckAssignment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !slices.Contains(session.User.Permissions, "trucks.read") {
		writeAPIError(w, http.StatusForbidden, "Truck read permission is required")
		return
	}
	if r.Method == http.MethodGet {
		v, err := handler.repo.GetDriverTruckAssignment(r.Context(), id)
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
		return
	}
	var input repository.DriverTruckAssignmentInput
	if err := decodeJSON(r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !isUUID(input.TruckID) || len(input.Revision) != 64 {
		writeAPIError(w, http.StatusBadRequest, "Select a truck from the current dropdown")
		return
	}
	if err := handler.repo.AssignDriverTruckToday(r.Context(), id, input, session.User.ID); err != nil {
		handler.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
