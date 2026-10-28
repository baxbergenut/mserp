package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func registerDispatcherPayRoutes(r chi.Router, logger *slog.Logger, repo *repository.DispatcherPayRepository) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.ChargeValidationError
		switch {
		case errors.Is(err, repository.ErrDispatcherPayConflict):
			writeAPIError(w, 409, err.Error())
		case errors.As(err, &invalid):
			writeAPIError(w, 400, err.Error())
		default:
			logger.Error("dispatcher pay failed", "error", err)
			writeAPIError(w, 500, "Dispatcher pay could not be loaded or saved.")
		}
	}
	r.Get("/dispatcher-pay", func(w http.ResponseWriter, r *http.Request) {
		week := r.URL.Query().Get("weekStart")
		if week == "" {
			location, _ := time.LoadLocation("America/New_York")
			now := time.Now().In(location)
			week = now.AddDate(0, 0, -(int(now.Weekday())+6)%7).Format(time.DateOnly)
		}
		report, err := repo.Get(r.Context(), week)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, report)
	})
	r.Get("/dispatcher-pay/rules", func(w http.ResponseWriter, r *http.Request) {
		out, err := repo.Rules(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/dispatcher-pay/rules", func(w http.ResponseWriter, r *http.Request) {
		var in repository.CommissionRuleChange
		if decodeJSON(r, &in) != nil {
			writeAPIError(w, 400, "Invalid commission rules")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.SaveRules(r.Context(), in, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	r.Put("/dispatcher-pay/inputs", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			WeekStart string `json:"weekStart"`
			repository.DispatcherPayInput
		}
		if decodeJSON(r, &in) != nil || !isUUID(in.DriverID) {
			writeAPIError(w, 400, "Invalid weekly dispatcher input")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.SaveInput(r.Context(), in.WeekStart, in.DispatcherPayInput, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	for _, reopen := range []bool{false, true} {
		path := "/dispatcher-pay/finalize"
		if reopen {
			path = "/dispatcher-pay/reopen"
		}
		r.Post(path, func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				WeekStart string `json:"weekStart"`
				repository.DispatcherStatementChange
			}
			if decodeJSON(r, &in) != nil || !isUUID(in.DispatcherID) {
				writeAPIError(w, 400, "Invalid dispatcher statement")
				return
			}
			session, _ := authSessionFromContext(r.Context())
			if err := repo.ChangeStatement(r.Context(), in.WeekStart, in.DispatcherStatementChange, session.User.ID, reopen); err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, 200, map[string]bool{"saved": true})
		})
	}
}
