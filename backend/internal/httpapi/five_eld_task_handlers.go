package httpapi

import (
	"errors"
	"mserp/internal/repository"
	"net/http"
	"slices"
)

func (h fleetHandler) fiveELDTruckTask(w http.ResponseWriter, r *http.Request) {
	session, _ := authSessionFromContext(r.Context())
	permission := "fleet.read"
	if r.Method == http.MethodPost {
		permission = "fleet.write"
	}
	if !slices.Contains(session.User.Permissions, permission) {
		writeAPIError(w, 403, "Truck review requires "+permission)
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		value, err := h.repo.FiveELDTruckReview(r.Context(), id)
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, 200, value)
		return
	}
	var request struct {
		Version int          `json:"version"`
		Truck   truckRequest `json:"truck"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	input, err := request.Truck.validate()
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	if input.VIN == nil || len(*input.VIN) != 17 {
		writeAPIError(w, 400, "A 17-character VIN is required")
		return
	}
	if !input.Active || input.DriverID != nil {
		writeAPIError(w, 400, "Add an active, unassigned truck before managing assignments")
		return
	}
	truckID, err := h.repo.CompleteFiveELDTruckReview(r.Context(), id, session.User.ID, request.Version, input)
	if errors.Is(err, repository.ErrFiveELDReviewConflict) {
		writeAPIError(w, 409, err.Error())
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"truckId": truckID})
}
