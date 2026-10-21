package httpapi

import (
	"net/http"
)

func (h maintenanceHandler) odometer(w http.ResponseWriter, r *http.Request) {
	cat, err := h.repo.CategoryID(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, cat, "view") && !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, cat, "create") && !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, cat, "edit") {
		writeAPIError(w, 403, "your role cannot prepare maintenance records")
		return
	}
	truck := r.URL.Query().Get("truckId")
	if !isUUID(truck) {
		writeAPIError(w, 400, "select a truck")
		return
	}
	reading, err := h.repo.CachedMaintenanceOdometer(r.Context(), truck)
	if err != nil {
		h.fail(w, err)
		return
	}
	message := ""
	if reading == nil {
		message = "Waiting for a recent background telematics reading."
	}
	writeJSON(w, 200, map[string]any{"reading": reading, "message": message})
}
