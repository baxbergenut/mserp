package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"mserp/internal/repository"
	"net/http"
)

func registerPayCostTransactions(r chi.Router, kind string, repo *repository.DriverPayRepository, fail func(http.ResponseWriter, error)) {
	r.Get("/"+kind+"-pay/{id}/cost-transactions", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		week, err := grossBoardWeek(r.URL.Query().Get("weekStart"))
		if err != nil {
			writeAPIError(w, 400, err.Error())
			return
		}
		cost := r.URL.Query().Get("cost")
		if cost != "fuel" && cost != "toll" {
			writeAPIError(w, 400, "Choose fuel or toll")
			return
		}
		result, err := repo.CostTransactions(r.Context(), kind, id, week, cost)
		if errors.Is(err, pgx.ErrNoRows) {
			writeAPIError(w, 404, "Statement not found")
			return
		}
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
}
