package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

// /settings is protected by access.manage, independently of payroll edits.
func registerDriverPaySettings(r chi.Router, repo *repository.DriverPayRepository, fail func(http.ResponseWriter, error)) {
	r.Get("/settings/driver-pay", func(w http.ResponseWriter, r *http.Request) {
		result, err := repo.Settings(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Put("/settings/driver-pay", func(w http.ResponseWriter, r *http.Request) {
		var input repository.DriverPaySettings
		if err := decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		input.LayoverDailyAmount = strings.TrimSpace(input.LayoverDailyAmount)
		if !grossBoardDecimal.MatchString(input.LayoverDailyAmount) || strings.HasPrefix(input.LayoverDailyAmount, "-") {
			writeAPIError(w, 400, "Enter a nonnegative daily layover amount with at most two decimals")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		result, err := repo.SaveSettings(r.Context(), input, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
}
