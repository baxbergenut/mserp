package httpapi

import "net/http"

func (handler fleetHandler) getTruckLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	value, err := handler.repo.TruckLocation(r.Context(), id)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler fleetHandler) getDriverTruckLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	value, err := handler.repo.DriverTruckLocation(r.Context(), id)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
