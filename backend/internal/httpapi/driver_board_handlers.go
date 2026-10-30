package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

type driverBoardRequest struct {
	// Entries may carry resolveCurrentLoad to explicitly reselect an unchanged number.
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
		if e.Version < 0 || e.Version > 2147483646 {
			return errors.New("invalid version")
		}
		for _, field := range []struct {
			value *string
			max   int
		}{{&e.CurrentLoad, 300}, {&e.TrailerNumber, 100}, {&e.TrailerNote, 5000}, {&e.Destination, 500}, {&e.ETA, 500}, {&e.Notes, 5000}, {&e.HomeTime, 500}, {&e.StatusReason, 1000}} {
			// Preserve spaces while an editor saves on every keystroke.
			if len([]rune(*field.value)) > field.max || strings.ContainsRune(*field.value, 0) {
				return errors.New("a board field exceeds its character limit or contains invalid text")
			}
		}
		if err := e.LeavePlan.Validate(); err != nil {
			return err
		}
		switch e.Status {
		case "", "READY", "REST", "COVERED", "DISPATCHED", "ENROUTE", "RESERVED", "VACATION", "DEADHEAD", "HOME", "SHOP", "STOP", "In the YARD", "Hold for Load", "Left", "Family issue", "Issue with PU/Del", "Health issue", "NEW DRIVER", "ACCIDENT":
		// Retain existing values during unrelated autosaves and history recovery.
		// These are no longer offered as driver status choices.
		case "At PU", "At DEL", "HOME (YARD)", "34 RESET", "HOLD", "RESET", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "LOAD CANCELLED", "REJECTED":
			if e.StatusEdited {
				return errors.New("select a current driver status")
			}
		default:
			return errors.New("invalid driver status")
		}
	}
	return nil
}

func registerDriverBoardRoutes(r chi.Router, logger *slog.Logger, repo *repository.DriverBoardRepository) {
	registerBoardPresenceRoutes(r, logger, repo)
	fail := func(w http.ResponseWriter, err error) {
		var statusError *pgconn.PgError
		if errors.As(err, &statusError) && statusError.ConstraintName == "fleet_status_invalid" {
			writeAPIError(w, 409, statusError.Message)
			return
		}
		var invalid *repository.ChargeValidationError
		if errors.As(err, &invalid) {
			writeAPIError(w, 409, err.Error())
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, 404, "history event not found")
			return
		}
		if errors.Is(err, repository.ErrTruckUnavailable) {
			writeAPIError(w, 409, err.Error())
			return
		}
		if errors.Is(err, repository.ErrDriverBoardConflict) || errors.Is(err, repository.ErrBoardLoadSelection) || errors.Is(err, repository.ErrDriverBoardOrderConflict) {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		logger.Error("status board request failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "the status board could not be loaded or saved")
	}
	parseFrom := func(raw string) (string, error) {
		if raw == "" {
			return "", nil // Repository applies today's New York date.
		}
		date, err := time.Parse(time.DateOnly, raw)
		if err != nil || date.Year() < 2000 || date.Year() > 2100 {
			return "", errors.New("invalid plan start date")
		}
		return raw, nil
	}
	r.Get("/driver-board/loads/{driverId}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "driverId")
		from, err := parseFrom(r.URL.Query().Get("from"))
		if !isUUID(id) || err != nil {
			writeAPIError(w, 400, "valid driver and start date are required")
			return
		}
		result, err := repo.Loads(r.Context(), id, from)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Post("/driver-board/loads/{driverId}", func(w http.ResponseWriter, r *http.Request) {
		var a repository.BoardLoadAction
		if err := decodeJSON(r, &a); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		a.DriverID = chi.URLParam(r, "driverId")
		from, err := parseFrom(a.FromDate)
		if !isUUID(a.DriverID) || err != nil || a.Version < 0 || len(a.Revision) != 64 || len(a.Order) > 1000 {
			writeAPIError(w, 400, "invalid load action")
			return
		}
		a.FromDate = from
		a.Number = strings.TrimSpace(a.Number)
		a.ManualStopTime = strings.TrimSpace(a.ManualStopTime)
		if a.Action == "add" && (a.Number == "" || len([]rune(a.Number)) > 100 || strings.ContainsAny(a.Number, "\r\n\t")) {
			writeAPIError(w, 400, "load number must contain 1–100 characters on one line")
			return
		}
		if a.Action == "stop_time" && (len([]rune(a.ManualStopTime)) > 80 || strings.ContainsAny(a.ManualStopTime, "\r\n\t")) {
			writeAPIError(w, 400, "manual stop time must be at most 80 characters on one line")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		result, err := repo.ChangeLoads(r.Context(), a, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Post("/driver-board/order", func(w http.ResponseWriter, r *http.Request) {
		var action repository.DriverBoardOrderAction
		if err := decodeJSON(r, &action); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		// Normalize UUID spellings so advisory locks and dispatcher keys agree.
		for _, id := range []*string{&action.DispatcherID, &action.DriverID, &action.TargetDriverID, action.TargetDispatcherID} {
			if id == nil || (id != &action.DriverID && *id == "") {
				continue
			}
			if !isUUID(*id) {
				writeAPIError(w, 400, "valid dispatcher and driver ids are required")
				return
			}
			compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(*id), "-", ""))
			*id = compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:]
		}
		if len(action.Revision) != 64 {
			writeAPIError(w, 400, "a current driver order revision is required")
			return
		}
		if action.TargetDispatcherID != nil && *action.TargetDispatcherID != action.DispatcherID && len(action.TargetRevision) != 64 {
			writeAPIError(w, 400, "a current destination order revision is required")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		result, err := repo.ReorderDrivers(r.Context(), action, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Post("/driver-board/dispatcher-order", func(w http.ResponseWriter, r *http.Request) {
		var action repository.DispatcherBoardOrderAction
		if err := decodeJSON(r, &action); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		for _, id := range []*string{&action.DispatcherID, &action.TargetDispatcherID} {
			if *id == "" {
				continue
			}
			if !isUUID(*id) {
				writeAPIError(w, 400, "valid dispatcher ids are required")
				return
			}
			compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(*id), "-", ""))
			*id = compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:]
		}
		if len(action.Revision) != 64 {
			writeAPIError(w, 400, "a current dispatcher order revision is required")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		result, err := repo.ReorderDispatchers(r.Context(), action, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
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
		if r.URL.Query().Get("overview") == "1" {
			board = repository.BoardOverview(board)
		}
		if r.URL.Query().Get("compact") == "1" {
			writeJSON(w, 200, repository.CompactBoard(board))
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
		var ids []string
		if raw := r.URL.Query().Get("driverIds"); raw != "" {
			ids = strings.Split(raw, ",")
		}
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
		field := r.URL.Query().Get("field")
		switch field {
		case "", "currentLoad", "trailerNumber", "trailerNote", "status", "destination", "eta", "notes", "homeTime", "leavePlan", "loadPlan", "driverHome":
		default:
			writeAPIError(w, 400, "invalid history field")
			return
		}
		result, err := repo.History(r.Context(), ids, cursor, field)
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
			Personal bool   `json:"personal"`
			DriverID string `json:"driverId"`
			Version  int    `json:"version"`
		}
		if err = decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		if !isUUID(input.DriverID) || input.Version < 0 {
			writeAPIError(w, 400, "invalid driver or version")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		saved, err := repo.Undo(r.Context(), id, input.DriverID, input.Version, session.User.ID, input.Personal)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, saved)
	})
}
