package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/jobs"
	"mserp/internal/repository"
)

func validateDriverPayEdits(e *repository.DriverPayEdits) error {
	week, err := grossBoardWeek(e.WeekStart)
	if err != nil {
		return err
	}
	if !isUUID(e.DriverID) {
		return errors.New("invalid driver id")
	}
	if e.Version < 0 || e.Version > 2147483646 {
		return errors.New("invalid version")
	}
	for _, value := range []*string{e.FuelOverride, e.TollOverride} {
		if value != nil {
			*value = strings.TrimSpace(*value)
			if !grossBoardDecimal.MatchString(*value) {
				return errors.New("Fuel and Toll amounts must be signed numbers with at most two decimal places, or null for automatic totals")
			}
		}
	}
	if len([]rune(e.Notes)) > 5000 {
		return errors.New("weekly notes must be 5000 characters or fewer")
	}
	if e.Comments == nil {
		e.Comments = map[string]string{}
	}
	if e.Adjustments == nil {
		e.Adjustments = []repository.DriverPayAdjustment{}
	}
	if len(e.Comments) > 700 || len(e.Adjustments) > 100 || len(e.GeneratedCharges) > 100 {
		return errors.New("too many comments or adjustments")
	}
	for key, value := range e.Comments {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 || len([]rune(key)) > 220 || len([]rune(value)) > 2000 {
			return errors.New("invalid load comment")
		}
		slot, err := strconv.Atoi(parts[1])
		if err != nil || slot < 0 || slot > 99 {
			return errors.New("invalid comment slot")
		}
		validDay := false
		for i := 0; i < 7; i++ {
			if parts[0] == week.AddDate(0, 0, i).Format("2006-01-02") {
				validDay = true
			}
		}
		if !validDay {
			return errors.New("comments must belong to the selected week")
		}
	}
	if len(e.ExpenseDeductions) > 500 {
		return errors.New("too many expense deductions")
	}
	expenseIDs := map[string]bool{}
	for _, item := range e.ExpenseDeductions {
		if !isUUID(item.ExpenseID) || expenseIDs[item.ExpenseID] || item.Version < 1 ||
			!expenseAmountPattern.MatchString(item.Amount) || strings.HasPrefix(item.Amount, "-") {
			return errors.New("expense deductions need unique expense ids, a valid version and a nonnegative amount with at most two decimals")
		}
		expenseIDs[item.ExpenseID] = true
	}
	seen := map[string]bool{}
	for i := range e.Adjustments {
		a := &e.Adjustments[i]
		a.ID = strings.ToLower(strings.TrimSpace(a.ID))
		a.Name = strings.TrimSpace(a.Name)
		a.Amount = strings.TrimSpace(a.Amount)
		if !isUUID(a.ID) || seen[a.ID] {
			return errors.New("adjustments need unique valid ids")
		}
		seen[a.ID] = true
		if a.Kind != "reimbursement" && a.Kind != "addition" && a.Kind != "deduction" {
			return errors.New("invalid adjustment kind")
		}
		if a.Name == "" || len([]rune(a.Name)) > 200 || len([]rune(a.Note)) > 2000 {
			return errors.New("provide an adjustment name (up to 200 characters) and note (up to 2000)")
		}
		if !grossBoardDecimal.MatchString(a.Amount) || strings.HasPrefix(a.Amount, "-") || strings.Trim(a.Amount, "0.") == "" {
			return errors.New("adjustment amounts must be positive with at most two decimal places")
		}
	}
	return nil
}

func registerDriverPayRoutes(r chi.Router, logger *slog.Logger, repo *repository.DriverPayRepository, job *jobs.SyncLoadsJob) {
	fail := func(w http.ResponseWriter, err error) {
		var invalid *repository.ChargeValidationError
		if errors.As(err, &invalid) {
			writeAPIError(w, 400, err.Error())
			return
		}
		if errors.Is(err, repository.ErrDriverPayConflict) || errors.Is(err, repository.ErrChargeConflict) {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			writeAPIError(w, http.StatusConflict, pgErr.Message)
			return
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			writeAPIError(w, http.StatusConflict, "the driver was removed; reload this week")
			return
		}
		logger.Error("driver pay request failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "driver pay could not be loaded or saved")
	}
	r.Get("/drivers/{id}/pay-history", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		page, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		page.PageSize = min(page.PageSize, 12)
		result, err := repo.History(r.Context(), id, page)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Get("/drivers/{id}/settlement-history", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		result, err := repo.SettlementHistory(r.Context(), id, week.Format("2006-01-02"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	for _, action := range []string{"finalize", "reopen"} {
		reopen := action == "reopen"
		r.Post("/driver-pay/"+action, func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				WeekStart string `json:"weekStart"`
				DriverID  string `json:"driverId"`
				Revision  string `json:"revision"`
				Reason    string `json:"reason"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeAPIError(w, 400, err.Error())
				return
			}
			week, err := grossBoardWeek(input.WeekStart)
			if err != nil {
				writeAPIError(w, 400, err.Error())
				return
			}
			if input.DriverID != "" && !isUUID(input.DriverID) {
				writeAPIError(w, 400, "invalid driver id")
				return
			}
			session, _ := authSessionFromContext(r.Context())
			result, err := repo.Settle(r.Context(), week, input.DriverID, input.Revision, session.User.ID, input.Reason, reopen)
			if err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, 200, result)
		})
	}
	registerPaySourceAcceptance(r, "/driver-pay", repo, fail)
	r.Get("/driver-pay", func(w http.ResponseWriter, r *http.Request) {
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		query, queryErr := payPageQuery(r)
		if queryErr != nil {
			writeAPIError(w, 400, queryErr.Error())
			return
		}
		result, err := repo.Get(r.Context(), week)
		if err != nil {
			fail(w, err)
			return
		}
		if r.URL.Query().Has("page") || r.URL.Query().Has("pageSize") {
			result = repository.PaginatePay(result, query)
		}
		writeJSON(w, 200, result)
	})
	r.Put("/driver-pay", func(w http.ResponseWriter, r *http.Request) {
		var e repository.DriverPayEdits
		if err := decodeJSON(r, &e); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		if err := validateDriverPayEdits(&e); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		session, _ := authSessionFromContext(r.Context())
		saved, err := repo.Save(r.Context(), e, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, saved)
	})
	r.Post("/driver-pay/refresh-loads", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			WeekStart string `json:"weekStart"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		week, err := grossBoardWeek(input.WeekStart)
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		report, err := repo.Get(r.Context(), week)
		if err != nil {
			fail(w, err)
			return
		}
		ids := []int{}
		seen := map[int]bool{}
		for _, d := range report.Drivers {
			for _, l := range d.Loads {
				if l.LoadRecordID != nil && !seen[*l.LoadRecordID] {
					ids = append(ids, *l.LoadRecordID)
					seen[*l.LoadRecordID] = true
				}
			}
		}
		if err := job.RefreshLoadDetails(r.Context(), ids); err != nil {
			logger.Error("refresh payroll load details failed", "error", err)
			writeAPIError(w, http.StatusBadGateway, "Load detail refresh failed. Some loads may have refreshed; reload to check, then retry.")
			return
		}
		report, err = repo.Get(r.Context(), week)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, report)
	})
}
