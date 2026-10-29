package httpapi

import (
	"context"
	"net/http"
	"time"
	_ "time/tzdata"

	"mserp/internal/repository"
)

// Office computers use Eastern time; birthdays follow the team's Tashkent day.
var birthdayLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Tashkent")
	if err != nil {
		panic(err)
	}
	return location
}()

func (h *authHandler) birthday(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, ok := authSessionFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	store, ok := h.store.(interface {
		ClaimBirthday(context.Context, string, time.Time) (repository.BirthdayGreeting, error)
	})
	if !ok {
		writeAPIError(w, 500, "birthday greeting unavailable")
		return
	}
	now := h.now()
	greeting, err := store.ClaimBirthday(r.Context(), session.User.ID, now.In(birthdayLocation))
	if err != nil {
		h.logger.Error("birthday greeting", "error", err)
		writeAPIError(w, 500, "birthday greeting unavailable")
		return
	}
	greeting.ServerNow = h.now()
	writeJSON(w, http.StatusOK, greeting)
}
