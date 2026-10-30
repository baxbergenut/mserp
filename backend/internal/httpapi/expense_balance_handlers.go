package httpapi

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mserp/internal/repository"
	"net/http"
)

func registerExpenseBalanceRoutes(r chi.Router, repo *repository.ExpenseRepository) {
	handle := func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		s, _ := authSessionFromContext(r.Context())
		expense, err := repo.GetExpense(r.Context(), id)
		if err != nil {
			writeAPIError(w, 404, "Expense not found")
			return
		}
		if !hasExpenseCategoryAction(s.User.ExpenseCategoryAccess, expense.CategoryID, "view") {
			writeAPIError(w, 403, "Expense category access required")
			return
		}
		if r.Method == "POST" {
			if !repository.HasPermission(s.User, "expenses.settle") {
				writeAPIError(w, 403, "Balance correction permission required")
				return
			}
			var x repository.ExpenseBalanceCorrection
			r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
			if err = decodeJSON(r, &x); err != nil {
				writeAPIError(w, 400, "Invalid balance correction")
				return
			}
			if err = repo.CorrectExpenseBalance(r.Context(), id, s.User.ID, x); err != nil {
				var invalid *repository.ChargeValidationError
				var pg *pgconn.PgError
				if errors.As(err, &invalid) {
					writeAPIError(w, 400, err.Error())
				} else if errors.Is(err, repository.ErrDriverPayConflict) {
					writeAPIError(w, 409, "The expense or request changed; reload and review")
				} else if errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "23505") {
					writeAPIError(w, 409, pg.Message)
				} else {
					writeAPIError(w, 500, "Balance correction could not be saved")
				}
				return
			}
		}
		entries, err := repo.ExpenseBalanceHistory(r.Context(), id)
		if err != nil {
			writeAPIError(w, 500, "Balance history could not be loaded")
			return
		}
		writeJSON(w, 200, entries)
	}
	r.Get("/expenses/{id}/balance-corrections", handle)
	r.Post("/expenses/{id}/balance-corrections", handle)
}
