package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
	"mserp/internal/repository"
	"net/http"
)

func registerDriverChargeRoutes(r chi.Router, logger *slog.Logger, repo *repository.DriverChargeRepository) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.ChargeValidationError
		if errors.As(err, &invalid) {
			writeAPIError(w, 400, err.Error())
			return
		}
		if errors.Is(err, repository.ErrChargeConflict) {
			writeAPIError(w, 409, err.Error())
			return
		}
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
			writeAPIError(w, 404, "Charge record not found")
			return
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) && (pg.Code == "23503" || pg.Code == "23505" || pg.Code == "23514") {
			writeAPIError(w, 409, "A related record changed or this charge conflicts with an existing record; reload and review")
			return
		}
		logger.Error("driver charges request failed", "error", err)
		writeAPIError(w, 500, "Driver charges could not be loaded or saved")
	}
	decode := func(w http.ResponseWriter, r *http.Request, v any) bool {
		r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
		if err := decodeJSON(r, v); err != nil {
			writeAPIError(w, 400, "Invalid charge request")
			return false
		}
		return true
	}
	actor := func(r *http.Request) string { s, _ := authSessionFromContext(r.Context()); return s.User.ID }
	r.Get("/driver-charges", func(w http.ResponseWriter, r *http.Request) {
		driver := r.URL.Query().Get("driverId")
		if driver != "" && !isUUID(driver) {
			writeAPIError(w, 400, "Invalid driver ID")
			return
		}
		data, err := repo.List(r.Context(), driver)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, data)
	})
	r.Post("/driver-charges/types", func(w http.ResponseWriter, r *http.Request) {
		var t repository.ChargeType
		if !decode(w, r, &t) {
			return
		}
		if t.ID != "" && !isUUID(t.ID) {
			writeAPIError(w, 400, "Invalid type ID")
			return
		}
		saved, err := repo.SaveType(r.Context(), t, actor(r))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, saved)
	})
	for _, preview := range []bool{false, true} {
		path := "/driver-charges/schedules"
		if preview {
			path += "/preview"
		}
		r.Post(path, func(w http.ResponseWriter, r *http.Request) {
			var c repository.ChargeCreate
			if !decode(w, r, &c) {
				return
			}
			for _, id := range c.DriverIDs {
				if !isUUID(id) {
					writeAPIError(w, 400, "Invalid driver ID")
					return
				}
			}
			if c.Kind == "recurring" && !isUUID(c.TypeID) {
				writeAPIError(w, 400, "Select a charge type")
				return
			}
			if preview {
				rows, err := repo.Draft(r.Context(), c)
				if err != nil {
					fail(w, err)
					return
				}
				writeJSON(w, 200, rows)
				return
			}
			ids, err := repo.Create(r.Context(), c, actor(r))
			if err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, 201, ids)
		})
	}
	r.Post("/driver-charges/bulk", func(w http.ResponseWriter, r *http.Request) {
		var b repository.ChargeBulk
		if !decode(w, r, &b) {
			return
		}
		for _, t := range b.Targets {
			if !isUUID(t.ID) || t.Version < 1 {
				writeAPIError(w, 400, "Invalid assignment selection")
				return
			}
		}
		if err := repo.Bulk(r.Context(), b, actor(r)); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Get("/driver-charges/schedules/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		rows, err := repo.Preview(r.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, rows)
	})
	r.Get("/driver-charges/schedules/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		rows, err := repo.History(r.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, rows)
	})
	for _, reopen := range []bool{false, true} {
		path := "/driver-charges/confirm"
		if reopen {
			path = "/driver-charges/reopen"
		}
		r.Post(path, func(w http.ResponseWriter, r *http.Request) {
			var c repository.ChargeConfirm
			if !decode(w, r, &c) {
				return
			}
			if !isUUID(c.DriverID) {
				writeAPIError(w, 400, "Invalid driver ID")
				return
			}
			for _, row := range c.Rows {
				if !isUUID(row.ScheduleID) {
					writeAPIError(w, 400, "Invalid schedule ID")
					return
				}
			}
			if err := repo.Confirm(r.Context(), c, actor(r), reopen); err != nil {
				fail(w, err)
				return
			}
			w.WriteHeader(204)
		})
	}
}
