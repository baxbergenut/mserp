package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"mserp/internal/repository"
)

func payPageQuery(r *http.Request) (repository.PayPageQuery, error) {
	p, err := parsePagination(r)
	q := r.URL.Query()
	return repository.PayPageQuery{Pagination: p, Search: q.Get("search"), DispatcherID: q.Get("dispatcherId"), ID: q.Get("statementId")}, err
}

func registerPaySourceAcceptance(r chi.Router, path string, repo *repository.DriverPayRepository, fail func(http.ResponseWriter, error)) {
	r.Post(path+"/accept-system", func(w http.ResponseWriter, r *http.Request) {
		var input repository.PaySourceAcceptance
		if err := decodeJSON(r, &input); err != nil {
			writeAPIError(w, 400, "Invalid source acceptance")
			return
		}
		if !isUUID(input.DriverID) || input.Version < 1 || input.Slot < 0 || input.Slot > 99 || input.LoadRecordID < 1 || !grossBoardDecimal.MatchString(input.OriginalRate) || (input.Miles != "" && (!grossBoardDecimal.MatchString(input.Miles) || strings.HasPrefix(input.Miles, "-"))) {
			writeAPIError(w, 400, "Invalid source acceptance")
			return
		}
		if date, err := time.Parse(time.DateOnly, input.Date); err != nil || date.Year() < 2000 || date.Year() > 2100 {
			writeAPIError(w, 400, "Invalid load date")
			return
		}
		if err := repo.AcceptPaySource(r.Context(), input); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
