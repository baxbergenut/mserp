package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
	"mserp/internal/repository"
	"net/http"
	"strings"
)

func registerInvestorPayRoutes(r chi.Router, logger *slog.Logger, pay *repository.DriverPayRepository, charges *repository.DriverChargeRepository) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.ChargeValidationError
		var pg *pgconn.PgError
		switch {
		case errors.As(err, &invalid):
			writeAPIError(w, 400, err.Error())
		case errors.Is(err, repository.ErrDriverPayConflict) || errors.Is(err, repository.ErrChargeConflict):
			writeAPIError(w, 409, err.Error())
		case errors.Is(err, pgx.ErrNoRows):
			writeAPIError(w, 404, "Record not found")
		case errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "23503" || pg.Code == "23505"):
			writeAPIError(w, 409, "A related settlement or charge changed; reload and review")
		default:
			logger.Error("investor accounting failed", "error", err)
			writeAPIError(w, 500, "Investor accounting could not be loaded or saved")
		}
	}
	actor := func(r *http.Request) string { s, _ := authSessionFromContext(r.Context()); return s.User.ID }
	decode := func(w http.ResponseWriter, r *http.Request, v any) bool {
		r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
		if err := decodeJSON(r, v); err != nil {
			writeAPIError(w, 400, "Invalid request")
			return false
		}
		return true
	}
	r.Get("/truck-charges", func(w http.ResponseWriter, r *http.Request) {
		v, err := charges.TruckCharges(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Put("/truck-charges/terms", func(w http.ResponseWriter, r *http.Request) {
		var v repository.TruckTerm
		if !decode(w, r, &v) {
			return
		}
		if !isUUID(v.TruckID) || !isUUID(v.OwnerID) || v.Version < 0 {
			writeAPIError(w, 400, "Invalid truck or owner")
			return
		}
		if err := charges.SaveTruckTerm(r.Context(), v, actor(r)); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Put("/truck-charges/recurring", func(w http.ResponseWriter, r *http.Request) {
		var v repository.TruckChargePhase
		if !decode(w, r, &v) {
			return
		}
		if !isUUID(v.TruckID) || !isUUID(v.TypeID) || v.Version < 0 || (v.MoveScheduleID != "" && !isUUID(v.MoveScheduleID)) {
			writeAPIError(w, 400, "Invalid truck charge")
			return
		}
		if err := charges.SaveTruckCharge(r.Context(), v, actor(r)); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Get("/investor-pay", func(w http.ResponseWriter, r *http.Request) {
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		v, err := pay.InvestorPay(r.Context(), week)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Put("/investor-pay", func(w http.ResponseWriter, r *http.Request) {
		var v repository.DriverPayEdits
		if !decode(w, r, &v) {
			return
		}
		// Investor comments also include source driver identity, since two drivers
		// may have the same slot and load reference on the same day.
		original := v.Comments
		v.Comments = map[string]string{}
		for key, value := range original {
			parts := strings.Split(key, ":")
			if len(parts) < 4 || !isUUID(parts[len(parts)-1]) {
				writeAPIError(w, 400, "Invalid investor load comment")
				return
			}
			v.Comments[strings.Join(parts[:len(parts)-1], ":")] = value
		}
		if err := validateDriverPayEdits(&v); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		v.Comments = original
		saved, err := pay.SaveInvestorPay(r.Context(), v, actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, saved)
	})
	for _, action := range []string{"finalize", "reopen"} {
		r.Post("/investor-pay/"+action, func(w http.ResponseWriter, r *http.Request) {
			var v struct {
				WeekStart string `json:"weekStart"`
				DriverID  string `json:"driverId"`
				Revision  string `json:"revision"`
				Reason    string `json:"reason"`
			}
			if !decode(w, r, &v) {
				return
			}
			week, err := grossBoardWeek(v.WeekStart)
			if err != nil || v.DriverID != "" && !isUUID(v.DriverID) {
				writeAPIError(w, 400, "Invalid week or truck")
				return
			}
			result, err := pay.SettleInvestor(r.Context(), week, v.DriverID, v.Revision, actor(r), v.Reason, action == "reopen")
			if err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, 200, result)
		})
	}
}
