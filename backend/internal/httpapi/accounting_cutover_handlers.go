package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
	"mserp/internal/repository"
	"net/http"
)

func registerAccountingCutoverRoutes(r chi.Router, logger *slog.Logger, repo *repository.DriverPayRepository) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.ChargeValidationError
		var pg *pgconn.PgError
		if errors.As(err, &invalid) {
			writeAPIError(w, 400, err.Error())
			return
		}
		if errors.Is(err, repository.ErrDriverPayConflict) {
			writeAPIError(w, 409, "The draft or financial data changed. Generate and review a new preview.")
			return
		}
		if errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "23503" || pg.Code == "23505") {
			writeAPIError(w, 409, pg.Message)
			return
		}
		logger.Error("accounting cutover failed", "error", err)
		writeAPIError(w, 500, "Accounting cutover could not be completed")
	}
	admin := func(w http.ResponseWriter, r *http.Request) bool {
		s, _ := authSessionFromContext(r.Context())
		if !s.User.Administrator {
			writeAPIError(w, 403, "Administrator access required")
			return false
		}
		return true
	}
	r.Get("/accounting/cutover", func(w http.ResponseWriter, r *http.Request) {
		if !admin(w, r) {
			return
		}
		v, err := repo.AccountingCutover(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Put("/accounting/cutover", func(w http.ResponseWriter, r *http.Request) {
		if !admin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
		var d repository.AccountingCutoverDraft
		if err := decodeJSON(r, &d); err != nil {
			writeAPIError(w, 400, "Invalid draft")
			return
		}
		v, err := repo.SaveAccountingCutover(r.Context(), d)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Post("/accounting/cutover/preview", func(w http.ResponseWriter, r *http.Request) {
		if !admin(w, r) {
			return
		}
		s, _ := authSessionFromContext(r.Context())
		v, err := repo.PreviewAccountingCutover(r.Context(), s.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Post("/accounting/cutover/openings", func(w http.ResponseWriter, r *http.Request) {
		if !admin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		var input repository.RateOpeningCorrection
		if e := decodeJSON(r, &input); e != nil {
			writeAPIError(w, 400, "Invalid opening correction")
			return
		}
		s, _ := authSessionFromContext(r.Context())
		v, e := repo.CorrectRateOpening(r.Context(), input, s.User.ID)
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Post("/accounting/cutover/activate", func(w http.ResponseWriter, r *http.Request) {
		if !admin(w, r) {
			return
		}
		var input struct {
			PreviewID string `json:"previewId"`
			Reviewed  bool   `json:"reviewed"`
		}
		if err := decodeJSON(r, &input); err != nil || !input.Reviewed || !isUUID(input.PreviewID) {
			writeAPIError(w, 400, "Review the complete preview before activation")
			return
		}
		s, _ := authSessionFromContext(r.Context())
		v, err := repo.ActivateAccountingCutover(r.Context(), input.PreviewID, s.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
}
