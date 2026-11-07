package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func registerDriverPayEscrowRoutes(r chi.Router, repo *repository.DriverPayRepository, fail func(http.ResponseWriter, error)) {
	r.Get("/driver-pay/candidates", func(w http.ResponseWriter, r *http.Request) {
		week, err := availablePayrollWeek(r.URL.Query().Get("weekStart"), repo.FirstPayrollWeek())
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		drivers, err := repo.DriverPayCandidates(r.Context(), week, r.URL.Query().Get("search"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, drivers)
	})
	r.Post("/driver-pay/membership", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			WeekStart string `json:"weekStart"`
			DriverID  string `json:"driverId"`
			Remove    bool   `json:"remove"`
		}
		if decodeJSON(r, &input) != nil || !isUUID(input.DriverID) {
			writeAPIError(w, 400, "Select a valid driver")
			return
		}
		week, err := availablePayrollWeek(input.WeekStart, repo.FirstPayrollWeek())
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		if err = repo.ChangeDriverPayMembership(r.Context(), week, input.DriverID, input.Remove); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	r.Get("/driver-pay/escrow", func(w http.ResponseWriter, r *http.Request) {
		if !requireCapabilities(w, r, "escrow.read") {
			return
		}
		week, err := availablePayrollWeek(r.URL.Query().Get("weekStart"), repo.FirstPayrollWeek())
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		driver := r.URL.Query().Get("driverId")
		if !isUUID(driver) {
			writeAPIError(w, 400, "Select a valid driver")
			return
		}
		editor, err := repo.DriverPayEscrow(r.Context(), week, driver)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, editor)
	})
	r.Put("/driver-pay/escrow", func(w http.ResponseWriter, r *http.Request) {
		if !requireCapabilities(w, r, "escrow.read", "escrow.create", "escrow.edit") {
			return
		}
		var input struct {
			WeekStart       string                        `json:"weekStart"`
			DriverID        string                        `json:"driverId"`
			EditVersion     int                           `json:"editVersion"`
			ExpenseRevision string                        `json:"expenseRevision"`
			Draft           repository.PayrollEscrowDraft `json:"draft"`
		}
		if decodeJSON(r, &input) != nil || !isUUID(input.DriverID) || (input.Draft.EscrowID != "" && !isUUID(input.Draft.EscrowID)) {
			writeAPIError(w, 400, "Invalid escrow instruction")
			return
		}
		week, err := availablePayrollWeek(input.WeekStart, repo.FirstPayrollWeek())
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		err = repo.SaveDriverPayEscrow(r.Context(), week, input.DriverID, input.EditVersion, input.ExpenseRevision, input.Draft)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
}
