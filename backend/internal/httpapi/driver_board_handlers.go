package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type driverBoardRequest struct {
	Entries []repository.DriverBoardEntry `json:"entries"`
}

func (request *driverBoardRequest) validate() error {
	if len(request.Entries) == 0 || len(request.Entries) > 1000 {
		return errors.New("save between 1 and 1000 drivers at a time")
	}
	seen := map[string]bool{}
	for index := range request.Entries {
		e := &request.Entries[index]
		if !isUUID(e.DriverID) {
			return errors.New("invalid driver id")
		}
		compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(e.DriverID), "-", ""))
		e.DriverID = compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:]
		if seen[e.DriverID] {
			return errors.New("a driver can appear only once")
		}
		seen[e.DriverID] = true
		if e.Version < 0 || e.Version > 2147483646 || e.HomeVersion < 0 || e.HomeVersion > 2147483646 {
			return errors.New("invalid version")
		}
		for _, field := range []struct {
			value *string
			max   int
		}{{&e.CurrentLoad, 300}, {&e.TrailerNumber, 100}, {&e.Destination, 500}, {&e.ETA, 500}, {&e.Notes, 5000}, {&e.HomeTime, 500}, {&e.DriverHome, 300}} {
			*field.value = strings.TrimSpace(*field.value)
			if len([]rune(*field.value)) > field.max || strings.ContainsRune(*field.value, 0) {
				return errors.New("a board field exceeds its character limit or contains invalid text")
			}
		}
		switch e.Status {
		case "", "ENROUTE", "DISPATCHED", "RESERVED", "HOME", "VACATION", "SHOP", "RESET", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "NEW DRIVER", "DEADHEAD", "LOAD CANCELLED", "REJECTED":
		default:
			return errors.New("invalid driver status")
		}
	}
	return nil
}

func registerDriverBoardRoutes(r chi.Router, logger *slog.Logger, repo *repository.DriverBoardRepository) {
	fail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, 404, "history event not found")
			return
		}
		if errors.Is(err, repository.ErrDriverBoardConflict) {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		logger.Error("driver board request failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "the driver board could not be loaded or saved")
	}
	r.Get("/driver-board", func(w http.ResponseWriter, r *http.Request) {
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		board, err := repo.Get(r.Context(), week)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, board)
	})
	r.Put("/driver-board", func(w http.ResponseWriter, r *http.Request) {
		var request driverBoardRequest
		if err := decodeJSON(r, &request); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		if err := request.validate(); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		session, _ := authSessionFromContext(r.Context())
		entries, err := repo.Save(r.Context(), request.Entries, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, entries)
	})
	r.Get("/driver-board/history", func(w http.ResponseWriter, r *http.Request) {
		ids := strings.Split(r.URL.Query().Get("driverIds"), ",")
		if len(ids) > 1000 {
			writeAPIError(w, 400, "too many drivers")
			return
		}
		for _, id := range ids {
			if !isUUID(id) {
				writeAPIError(w, 400, "valid driver ids are required")
				return
			}
		}
		var cursor int64
		if raw := r.URL.Query().Get("before"); raw != "" {
			var err error
			cursor, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || cursor < 0 {
				writeAPIError(w, 400, "invalid history cursor")
				return
			}
		}
		result, err := repo.History(r.Context(), ids, cursor)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Post("/driver-board/history/{id}/undo", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeAPIError(w, 400, "invalid history id")
			return
		}
		var input struct {
			DriverID    string `json:"driverId"`
			Version     int    `json:"version"`
			HomeVersion int    `json:"homeVersion"`
		}
		if err = decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		if !isUUID(input.DriverID) || input.Version < 0 || input.HomeVersion < 0 {
			writeAPIError(w, 400, "invalid driver or version")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		saved, err := repo.Undo(r.Context(), id, input.DriverID, input.Version, input.HomeVersion, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, saved)
	})
}
