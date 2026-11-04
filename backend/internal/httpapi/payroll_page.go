package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"mserp/internal/repository"
)

// Payroll statements become available when their New York calendar week starts.
// Future schedules and source dates remain valid inputs for that later week.
func availablePayrollWeek(raw string, first ...string) (time.Time, error) {
	week, err := grossBoardWeek(raw)
	if err == nil && week.Format(time.DateOnly) > repository.ChargeCurrentWeek() {
		return week, errors.New("Payroll weeks are available when the week starts")
	}
	if err == nil && len(first) > 0 && week.Format(time.DateOnly) < first[0] {
		return week, errors.New("This payroll week has been retired")
	}
	return week, err
}

func payPageQuery(r *http.Request) (repository.PayPageQuery, error) {
	p, err := parsePagination(r)
	q := r.URL.Query()
	result := repository.PayPageQuery{Pagination: p, Search: q.Get("search"), DispatcherID: q.Get("dispatcherId"), ID: q.Get("statementId"), Summary: q.Get("summary") == "1", Interactive: true}
	if err != nil {
		return result, err
	}
	if raw := q.Get("detailIds"); raw != "" {
		ids := strings.Split(raw, ",")
		if len(ids) > 100 {
			return result, errors.New("at most 100 statement details may be requested")
		}
		result.DetailIDs = map[string]bool{}
		for _, id := range ids {
			if !isUUID(id) {
				return result, errors.New("invalid statement detail id")
			}
			result.DetailIDs[strings.ToLower(id)] = true
		}
	}
	return result, nil
}

func registerPaySourceAcceptance(r chi.Router, path string, repo *repository.DriverPayRepository, fail func(http.ResponseWriter, error)) {
	r.Get(path+"/availability", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"firstWeek": repo.FirstPayrollWeek(), "currentWeek": repository.ChargeCurrentWeek()})
	})
	writeSourceReceipt := func(w http.ResponseWriter, r *http.Request, actor, requestID string) bool {
		if requestID == "" {
			return false
		}
		receipt, err := repo.PayrollSourceCommandReceipt(r.Context(), actor, requestID)
		if err != nil {
			fail(w, err)
		} else {
			writeJSON(w, http.StatusOK, receipt)
		}
		return true
	}
	r.Post(path+"/prepare", func(w http.ResponseWriter, r *http.Request) {
		if !repo.PreparedReadsEnabled() {
			writeAPIError(w, 404, "Prepared payroll is not enabled")
			return
		}
		var input struct {
			WeekStart string `json:"weekStart"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, "Invalid preparation request")
			return
		}
		week, err := availablePayrollWeek(input.WeekStart, repo.FirstPayrollWeek())
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		if err = repo.RequestPayrollPreparation(r.Context(), week, true); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"state": "preparing"})
	})
	r.Get(path+"/settlement-preview", func(w http.ResponseWriter, r *http.Request) {
		week, err := availablePayrollWeek(r.URL.Query().Get("weekStart"), repo.FirstPayrollWeek())
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		kind := "driver"
		if path == "/investor-pay" {
			kind = "investor"
		}
		var result repository.DriverPayWeek
		if repo.PreparedReadsEnabled() {
			result, err = repo.PreparedPayroll(r.Context(), kind, week, repository.PayPageQuery{Summary: true, Pagination: repository.Pagination{PageSize: 2147483647}}, r.URL.Query().Get("prepared") == "1")
			result.Pagination = nil
		} else {
			result, err = repo.PayrollPreview(r.Context(), kind, week)
		}
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Get(path+"/statements/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		week, err := availablePayrollWeek(r.URL.Query().Get("weekStart"), repo.FirstPayrollWeek())
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		kind := "driver"
		if path == "/investor-pay" {
			kind = "investor"
		}
		var result repository.DriverPayWeek
		if repo.PreparedReadsEnabled() {
			version := r.URL.Query().Get("revision")
			if version != "" && !isUUID(version) {
				writeAPIError(w, 400, "Invalid statement revision")
				return
			}
			result, err = repo.PreparedPayroll(r.Context(), kind, week, repository.PayPageQuery{ID: id, StatementRevision: version}, r.URL.Query().Get("prepared") == "1")
			result.Pagination = nil
			if err == nil && len(result.Drivers) == 0 && result.Preparation.State == "ready" {
				err = pgx.ErrNoRows
			}
		} else {
			result, err = repo.PayrollStatement(r.Context(), kind, week, id)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeAPIError(w, 404, "Statement no longer exists for this week")
			return
		}
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	r.Post(path+"/statement-load", func(w http.ResponseWriter, r *http.Request) {
		var input repository.PayLoadPlacementInput
		if err := decodeJSON(r, &input); err != nil || !isUUID(input.DriverID) || (input.SourceDriverID != "" && !isUUID(input.SourceDriverID)) {
			writeAPIError(w, 400, "Invalid statement load")
			return
		}
		kind := "driver"
		if path == "/investor-pay" {
			kind = "investor"
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.PlacePayLoad(r.Context(), kind, input, session.User.ID); err != nil {
			var choice *repository.PayLoadChoiceRequired
			if errors.As(err, &choice) {
				writeJSON(w, http.StatusOK, choice)
				return
			}
			fail(w, err)
			return
		}
		if writeSourceReceipt(w, r, session.User.ID, input.RequestID) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post(path+"/transfer-load", func(w http.ResponseWriter, r *http.Request) {
		var input repository.LoadWeekTransferInput
		if err := decodeJSON(r, &input); err != nil || !isUUID(input.DriverID) {
			writeAPIError(w, 400, "Invalid load transfer")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.TransferLoadWeek(r.Context(), input, session.User.ID); err != nil {
			fail(w, err)
			return
		}
		if writeSourceReceipt(w, r, session.User.ID, input.RequestID) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post(path+"/undo-system", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			repository.PayrollCommandMetadata
			ID string `json:"id"`
		}
		if err := decodeJSON(r, &input); err != nil || !isUUID(input.ID) {
			writeAPIError(w, 400, "Invalid undo action")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		if err := repo.UndoPaySource(r.Context(), input.ID, session.User.ID, input.PayrollCommandMetadata); err != nil {
			fail(w, err)
			return
		}
		if writeSourceReceipt(w, r, session.User.ID, input.RequestID) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post(path+"/accept-system", func(w http.ResponseWriter, r *http.Request) {
		var input repository.PaySourceAcceptance
		if err := decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, "Invalid source acceptance")
			return
		}
		if !isUUID(input.DriverID) || input.Version < 1 || input.Slot < 0 || input.Slot > 99 || input.LoadRecordID < 1 || (input.Field != "" && input.Field != "originalRate" && input.Field != "totalMiles" && input.Field != "driverGross") || ((input.Field == "" || input.Field == "originalRate") && !grossBoardDecimal.MatchString(input.OriginalRate)) || ((input.Field == "" || input.Field == "totalMiles") && input.Miles != "" && (!grossBoardDecimal.MatchString(input.Miles) || strings.HasPrefix(input.Miles, "-"))) || (input.Field == "driverGross" && !grossBoardDecimal.MatchString(input.DriverGross)) {
			writeAPIError(w, 400, "Invalid source acceptance")
			return
		}
		if date, err := time.Parse(time.DateOnly, input.Date); err != nil || date.Year() < 2000 || date.Year() > 2100 {
			writeAPIError(w, 400, "Invalid load date")
			return
		}
		session, _ := authSessionFromContext(r.Context())
		id, err := repo.AcceptPaySourceWithUndo(r.Context(), input, session.User.ID)
		if err != nil {
			fail(w, err)
			return
		}
		if writeSourceReceipt(w, r, session.User.ID, input.RequestID) {
			return
		}
		// Old clients retain the no-content response.
		if r.URL.Query().Get("undoReceipt") == "1" {
			writeJSON(w, http.StatusOK, map[string]string{"undoId": id})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func payrollPreparationError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, repository.ErrPayrollVersionExpired) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "statement_version_expired"})
		return true
	}
	if errors.Is(err, repository.ErrPayrollPreparing) || errors.Is(err, repository.ErrPayrollProjectionCoverage) {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error(), "code": "payroll_preparing"})
		return true
	}
	return false
}
