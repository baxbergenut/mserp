package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/repository"
)

var grossBoardDecimal = regexp.MustCompile(`^-?\d{1,10}(?:\.\d{1,2})?$`)

type grossBoardRequest struct {
	WeekStart string                       `json:"weekStart"`
	Entries   []repository.GrossBoardEntry `json:"entries"`
}

func grossBoardWeek(value string) (time.Time, error) {
	week, err := time.Parse("2006-01-02", value)
	if err != nil || week.Weekday() != time.Monday || week.Year() < 2000 || week.Year() > 2100 {
		return time.Time{}, errors.New("weekStart must be a Monday in YYYY-MM-DD format (2000–2100)")
	}
	return week, nil
}

func (request *grossBoardRequest) validate() error {
	week, err := grossBoardWeek(request.WeekStart)
	if err != nil {
		return err
	}
	if len(request.Entries) == 0 || len(request.Entries) > 1000 {
		return errors.New("save between 1 and 1000 edited load slots at a time")
	}
	seen := map[string]bool{}
	for i := range request.Entries {
		e := &request.Entries[i]
		if !isUUID(e.DriverID) {
			return errors.New("invalid driver id")
		}
		// Canonicalize UUIDs before duplicate detection and lock ordering.
		compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(e.DriverID), "-", ""))
		e.DriverID = compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:]
		date, err := time.Parse("2006-01-02", e.Date)
		if err != nil || date.Before(week) || !date.Before(week.AddDate(0, 0, 7)) {
			return errors.New("every edited day must belong to the selected week")
		}
		if e.Slot < 0 || e.Slot > 99 || (e.Slot == 0 && e.Deleted) {
			return errors.New("invalid load slot")
		}
		if e.Deleted {
			e.DayStatus = ""
			e.EnteredOriginalRate = ""
			e.EnteredMiles = ""
			e.AcceptSystemValues = false
			e.LoadNumber = ""
			e.LoadRecordID = nil
			e.OriginalRate = ""
			e.DriverRate = ""
			e.Miles = ""
		}
		key := e.DriverID + e.Date + ":" + strconv.Itoa(e.Slot)
		if seen[key] {
			return errors.New("a driver load slot can only appear once")
		}
		seen[key] = true
		e.LoadNumber = strings.TrimSpace(e.LoadNumber)
		if len([]rune(e.LoadNumber)) > 200 {
			return errors.New("load number or planning text must be 200 characters or fewer")
		}
		if e.Version < 0 || e.Version > 2147483646 {
			return errors.New("invalid entry version")
		}
		if e.LoadRecordID != nil && (*e.LoadRecordID <= 0 || e.LoadNumber == "") {
			return errors.New("a selected load requires a valid record id and load number")
		}
		for _, value := range []*string{&e.OriginalRate, &e.DriverRate, &e.Miles, &e.EnteredOriginalRate, &e.EnteredMiles} {
			*value = strings.TrimSpace(*value)
			if *value != "" && !grossBoardDecimal.MatchString(*value) {
				return errors.New("rates and miles must be decimal numbers with at most 10 whole digits and 2 decimal places")
			}
		}
		if strings.HasPrefix(e.Miles, "-") || strings.HasPrefix(e.EnteredMiles, "-") {
			return errors.New("miles cannot be negative")
		}
		switch e.DayStatus {
		case "":
		case "SHOP", "HOME", "RESET", "IN TRANSIT", "REJECTED", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "NEW DRIVER", "DEADHEAD":
			if e.LoadNumber != "" || e.LoadRecordID != nil || e.OriginalRate != "" || e.DriverRate != "" || e.Miles != "" || e.EnteredOriginalRate != "" || e.EnteredMiles != "" || e.AcceptSystemValues {
				return errors.New("a day status cannot contain a load, rates, or miles")
			}
		default:
			return errors.New("invalid day status")
		}
	}
	return nil
}

func registerGrossBoardRoutes(r chi.Router, logger *slog.Logger, repo *repository.GrossBoardRepository) {
	fail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, repository.ErrGrossBoardConflict) || errors.Is(err, repository.ErrGrossBoardLoad) {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			writeAPIError(w, http.StatusConflict, "a driver or load was removed; reload the board")
			return
		}
		logger.Error("gross board request failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "the gross board could not be loaded or saved")
	}
	r.Get("/gross-board", func(w http.ResponseWriter, r *http.Request) {
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		board, err := repo.Get(r.Context(), week)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, board)
	})
	r.Get("/gross-board/loads", func(w http.ResponseWriter, r *http.Request) {
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		if len([]rune(search)) > 200 {
			writeAPIError(w, http.StatusBadRequest, "search must be 200 characters or fewer")
			return
		}
		if search == "" {
			writeJSON(w, http.StatusOK, []repository.GrossBoardLoad{})
			return
		}
		loads, err := repo.SearchLoads(r.Context(), search)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, loads)
	})
	r.Get("/gross-board/balance", func(w http.ResponseWriter, r *http.Request) {
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		driverID := r.URL.Query().Get("driverId")
		if !isUUID(driverID) {
			writeAPIError(w, http.StatusBadRequest, "invalid driver id")
			return
		}
		history, err := repo.BalanceHistory(r.Context(), driverID, week)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, history)
	})
	r.Put("/gross-board", func(w http.ResponseWriter, r *http.Request) {
		var request grossBoardRequest
		if err := decodeJSON(r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := request.validate(); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		entries, err := repo.SaveEntries(r.Context(), request.Entries)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entries)
	})
}
