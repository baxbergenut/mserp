package httpapi

import (
	"context"
	"net/http"
	"time"
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
	if h.eld == nil {
		writeJSON(w, 200, map[string]any{"reading": nil, "message": "Five ELD is not configured. Enter the service odometer manually."})
		return
	}
	vin, err := h.repo.OdometerVIN(r.Context(), truck)
	if err != nil {
		writeJSON(w, 200, map[string]any{"reading": nil, "message": "This truck needs a unique VIN to read its Five ELD odometer."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	reading, err := h.eld.MaintenanceOdometer(ctx, vin)
	if err != nil {
		writeJSON(w, 200, map[string]any{"reading": nil, "message": "No recent Five ELD odometer is available. Enter the service odometer manually."})
		return
	}
	writeJSON(w, 200, map[string]any{"reading": reading, "message": ""})
}
