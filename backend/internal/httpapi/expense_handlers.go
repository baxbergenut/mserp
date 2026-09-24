package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mserp/internal/repository"
)

var expenseAmountPattern = regexp.MustCompile(`^-?\d{1,12}(?:\.\d{1,2})?$`)

var expenseCategories = map[string]struct{}{
	"Maintenance":    {},
	"Other":          {},
	"Safety":         {},
	"HR":             {},
	"Administrative": {},
}

type expenseHandler struct {
	logger *slog.Logger
	repo   *repository.ExpenseRepository
}

func registerExpenseRoutes(r chi.Router, logger *slog.Logger, repo *repository.ExpenseRepository) {
	handler := expenseHandler{logger: logger, repo: repo}
	r.Get("/expenses", handler.listExpenses)
	r.Post("/expenses", handler.createExpense)
	r.Put("/expenses/{id}", handler.updateExpense)
	r.Delete("/expenses/{id}", handler.deleteExpense)
	r.Get("/telegram-expense-updates", handler.listTelegramExpenseUpdates)
	r.Post("/telegram-expense-updates/{updateID}/retry", handler.retryTelegramExpenseUpdate)
	r.Post("/telegram-expense-updates/{updateID}/resolve", handler.resolveTelegramExpenseUpdate)
}

var telegramExpenseStatuses = map[string]struct{}{
	"queued": {}, "processing": {}, "retry": {}, "completed": {},
	"ignored": {}, "needs_review": {}, "failed": {},
}

type expenseRequest struct {
	Company            string  `json:"company"`
	Category           string  `json:"category"`
	ExpenseDate        string  `json:"expenseDate"`
	TruckID            *string `json:"truckId"`
	DriverID           *string `json:"driverId"`
	UnitNumber         string  `json:"unitNumber"`
	DriverName         string  `json:"driverName"`
	Amount             string  `json:"amount"`
	PaymentType        string  `json:"paymentType"`
	ExpenseType        string  `json:"expenseType"`
	ReferenceNumber    string  `json:"referenceNumber"`
	Description        string  `json:"description"`
	CoveredBy          string  `json:"coveredBy"`
	PaidBy             string  `json:"paidBy"`
	ManagerVerified    bool    `json:"managerVerified"`
	AccountingVerified bool    `json:"accountingVerified"`
}

type telegramExpenseResolutionRequest struct {
	Expenses []expenseRequest `json:"expenses"`
}

func (request expenseRequest) validate() (repository.ExpenseInput, error) {
	request.Company = strings.TrimSpace(request.Company)
	request.Category = strings.TrimSpace(request.Category)
	request.Amount = strings.TrimSpace(request.Amount)
	if request.Company == "" {
		return repository.ExpenseInput{}, errors.New("company is required")
	}
	if _, ok := expenseCategories[request.Category]; !ok {
		return repository.ExpenseInput{}, errors.New("category must be Maintenance, Other, Safety, HR, or Administrative")
	}
	expenseDate, err := parseOptionalDate(request.ExpenseDate, "expense date")
	if err != nil {
		return repository.ExpenseInput{}, err
	}
	if expenseDate == nil {
		return repository.ExpenseInput{}, errors.New("expense date is required")
	}
	if !expenseAmountPattern.MatchString(request.Amount) {
		return repository.ExpenseInput{}, errors.New("amount must be a decimal value with at most two decimal places")
	}
	if err := validateOptionalUUID(request.TruckID, "truck id"); err != nil {
		return repository.ExpenseInput{}, err
	}
	if err := validateOptionalUUID(request.DriverID, "driver id"); err != nil {
		return repository.ExpenseInput{}, err
	}
	return repository.ExpenseInput{
		Company: request.Company, Category: request.Category, ExpenseDate: *expenseDate,
		TruckID: request.TruckID, DriverID: request.DriverID,
		UnitNumber: optionalString(request.UnitNumber), DriverName: optionalString(request.DriverName),
		Amount: request.Amount, PaymentType: optionalString(request.PaymentType),
		ExpenseType: optionalString(request.ExpenseType), ReferenceNumber: optionalString(request.ReferenceNumber),
		Description: optionalString(request.Description), CoveredBy: optionalString(request.CoveredBy),
		PaidBy: optionalString(request.PaidBy), ManagerVerified: request.ManagerVerified,
		AccountingVerified: request.AccountingVerified,
	}, nil
}

func (handler expenseHandler) listExpenses(w http.ResponseWriter, r *http.Request) {
	pagination, err := parsePagination(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	dateFrom, err := parseOptionalDate(r.URL.Query().Get("dateFrom"), "dateFrom")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	dateTo, err := parseOptionalDate(r.URL.Query().Get("dateTo"), "dateTo")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	truckID, err := parseOptionalQueryUUID(r.URL.Query().Get("truckId"), "truckId")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	driverID, err := parseOptionalQueryUUID(r.URL.Query().Get("driverId"), "driverId")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.ListExpensesPage(r.Context(), repository.ExpensePageQuery{
		Pagination: pagination,
		Search:     strings.TrimSpace(r.URL.Query().Get("search")),
		Category:   strings.TrimSpace(r.URL.Query().Get("category")),
		Company:    strings.TrimSpace(r.URL.Query().Get("company")),
		DateFrom:   dateFrom,
		DateTo:     dateTo,
		TruckID:    truckID,
		DriverID:   driverID,
	})
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func parseOptionalQueryUUID(value, label string) (*string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if !isUUID(trimmed) {
		return nil, errors.New(label + " must be a valid UUID")
	}
	return &trimmed, nil
}

func (handler expenseHandler) createExpense(w http.ResponseWriter, r *http.Request) {
	var request expenseRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.CreateExpense(r.Context(), input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (handler expenseHandler) updateExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var request expenseRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.UpdateExpense(r.Context(), id, input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler expenseHandler) deleteExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := handler.repo.DeleteExpense(r.Context(), id); err != nil {
		handler.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler expenseHandler) listTelegramExpenseUpdates(w http.ResponseWriter, r *http.Request) {
	pagination, err := parsePagination(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" {
		if _, ok := telegramExpenseStatuses[status]; !ok {
			writeAPIError(w, http.StatusBadRequest, "invalid Telegram expense status")
			return
		}
	}
	value, err := handler.repo.ListTelegramExpenseActivities(
		r.Context(), pagination, status, strings.TrimSpace(r.URL.Query().Get("search")),
	)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler expenseHandler) retryTelegramExpenseUpdate(w http.ResponseWriter, r *http.Request) {
	updateID, ok := telegramUpdateID(w, r)
	if !ok {
		return
	}
	retried, err := handler.repo.RetryTelegramUpdateNow(r.Context(), updateID)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	if !retried {
		writeAPIError(w, http.StatusConflict, "this update is already completed or cannot be retried")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"updateId": updateID, "status": "queued"})
}

func (handler expenseHandler) resolveTelegramExpenseUpdate(w http.ResponseWriter, r *http.Request) {
	updateID, ok := telegramUpdateID(w, r)
	if !ok {
		return
	}
	var request telegramExpenseResolutionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(request.Expenses) == 0 || len(request.Expenses) > 25 {
		writeAPIError(w, http.StatusBadRequest, "expenses must contain between 1 and 25 records")
		return
	}
	inputs := make([]repository.ExpenseInput, 0, len(request.Expenses))
	for index, expense := range request.Expenses {
		input, err := expense.validate()
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "expense "+strconv.Itoa(index+1)+": "+err.Error())
			return
		}
		inputs = append(inputs, input)
	}
	values, err := handler.repo.ResolveTelegramExpenses(r.Context(), updateID, inputs)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, values)
}

func telegramUpdateID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	updateID, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "updateID")), 10, 64)
	if err != nil || updateID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "update id must be a positive integer")
		return 0, false
	}
	return updateID, true
}

func (handler expenseHandler) writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "record not found")
		return
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "23514", "22P02", "22003":
			writeAPIError(w, http.StatusBadRequest, "the expense contains invalid data")
			return
		}
	}
	handler.logger.Error("expense request failed", "error", err)
	writeAPIError(w, http.StatusInternalServerError, "the expense request could not be completed")
}
