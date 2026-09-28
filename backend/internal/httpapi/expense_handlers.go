package httpapi

import (
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mserp/internal/gemini"
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
	logger    *slog.Logger
	repo      *repository.ExpenseRepository
	extractor gemini.ExpenseExtractor
}

func registerExpenseRoutes(
	r chi.Router,
	logger *slog.Logger,
	repo *repository.ExpenseRepository,
	extractor gemini.ExpenseExtractor,
) {
	handler := expenseHandler{logger: logger, repo: repo, extractor: extractor}
	r.Get("/expenses", handler.listExpenses)
	r.Post("/expenses", handler.createExpense)
	r.Post("/expenses/bulk", handler.createExpenses)
	r.Post("/expenses/extract", handler.extractExpenses)
	r.Put("/expenses/{id}", handler.updateExpense)
	r.Delete("/expenses/{id}", handler.deleteExpense)
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

type expenseBatchRequest struct {
	Expenses []expenseRequest `json:"expenses"`
}

type extractedExpense struct {
	Company         string   `json:"company"`
	Category        string   `json:"category"`
	ExpenseDate     string   `json:"expenseDate"`
	TruckID         *string  `json:"truckId"`
	DriverID        *string  `json:"driverId"`
	UnitNumber      string   `json:"unitNumber"`
	DriverName      string   `json:"driverName"`
	Amount          string   `json:"amount"`
	PaymentType     string   `json:"paymentType"`
	ExpenseType     string   `json:"expenseType"`
	ReferenceNumber string   `json:"referenceNumber"`
	Description     string   `json:"description"`
	CoveredBy       string   `json:"coveredBy"`
	PaidBy          string   `json:"paidBy"`
	Confidence      float64  `json:"confidence"`
	Evidence        []string `json:"evidence"`
}

type expenseExtractionResponse struct {
	Expenses []extractedExpense `json:"expenses"`
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

func (handler expenseHandler) createExpenses(w http.ResponseWriter, r *http.Request) {
	var request expenseBatchRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(request.Expenses) < 2 || len(request.Expenses) > 25 {
		writeAPIError(w, http.StatusBadRequest, "expenses must contain between 2 and 25 records")
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
	values, err := handler.repo.CreateExpenses(r.Context(), inputs)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, values)
}

func (handler expenseHandler) extractExpenses(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 22<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "the expense attachment must be 20 MB or smaller")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid expense analysis request")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	input := gemini.ExpenseInput{
		Text:        strings.TrimSpace(r.FormValue("text")),
		MessageDate: time.Now().UTC(),
	}
	if headers := r.MultipartForm.File["file"]; len(headers) > 0 {
		data, err := readMultipartFile(headers[0], 20<<20)
		if err != nil {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "the expense attachment must be a non-empty file no larger than 20 MB")
			return
		}
		contentType := detectUploadContentType(headers[0].Header.Get("Content-Type"), data)
		if !isSupportedDocumentType(contentType) && contentType != "text/plain" {
			writeAPIError(w, http.StatusUnsupportedMediaType, "expense attachments must be PDF, TXT, PNG, JPEG, or WEBP")
			return
		}
		input.MIMEType = contentType
		input.FileName = safeUploadFileName(headers[0].Filename, "expense-attachment")
		input.FileData = data
	}
	if input.Text == "" && len(input.FileData) == 0 {
		writeAPIError(w, http.StatusBadRequest, "transaction text or an attachment is required")
		return
	}

	extraction, err := handler.extractor.ExtractExpense(r.Context(), input)
	if err != nil {
		if errors.Is(err, gemini.ErrNotConfigured) {
			writeAPIError(w, http.StatusServiceUnavailable, "GEMINI_API_KEY is not configured on the server")
			return
		}
		handler.logger.Error("extract expenses", "error", err)
		writeAPIError(w, http.StatusBadGateway, "the transaction could not be analyzed: "+err.Error())
		return
	}
	if !extraction.IsExpense || len(extraction.Expenses) == 0 {
		writeJSON(w, http.StatusOK, expenseExtractionResponse{Expenses: []extractedExpense{}})
		return
	}
	if len(extraction.Expenses) > 25 {
		writeAPIError(w, http.StatusBadGateway, "the transaction contains more than 25 expenses")
		return
	}

	values := make([]extractedExpense, 0, len(extraction.Expenses))
	for _, item := range extraction.Expenses {
		unitNumber := cleanExtractedValue(item.UnitNumber)
		driverName := cleanExtractedValue(item.DriverName)
		truckID, driverID, err := handler.repo.ResolveExpenseLinks(r.Context(), stringPointer(unitNumber), stringPointer(driverName))
		if err != nil {
			handler.writeError(w, err)
			return
		}
		values = append(values, extractedExpense{
			Company: validExtractedCompany(item.Company), Category: validExtractedCategory(item.Category),
			ExpenseDate: validExtractedDate(item.ExpenseDate), TruckID: truckID, DriverID: driverID,
			UnitNumber: unitNumber, DriverName: driverName, Amount: validExtractedAmount(item.Amount),
			PaymentType: cleanExtractedValue(item.PaymentType), ExpenseType: cleanExtractedValue(item.ExpenseType),
			ReferenceNumber: cleanExtractedValue(item.ReferenceNumber), Description: cleanExtractedValue(item.Description),
			CoveredBy: defaultExtractedValue(item.CoveredBy, "Company"), PaidBy: cleanExtractedValue(item.PaidBy),
			Confidence: item.Confidence, Evidence: item.Evidence,
		})
	}
	writeJSON(w, http.StatusOK, expenseExtractionResponse{Expenses: values})
}

func cleanExtractedValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func defaultExtractedValue(value *string, fallback string) string {
	if cleaned := cleanExtractedValue(value); cleaned != "" {
		return cleaned
	}
	return fallback
}

func validExtractedCompany(value *string) string {
	cleaned := cleanExtractedValue(value)
	if cleaned == "Flinn Corp" {
		return cleaned
	}
	return "MS Express"
}

func validExtractedCategory(value *string) string {
	cleaned := cleanExtractedValue(value)
	if _, ok := expenseCategories[cleaned]; ok {
		return cleaned
	}
	return "Other"
}

func validExtractedDate(value *string) string {
	cleaned := cleanExtractedValue(value)
	if _, err := time.Parse(time.DateOnly, cleaned); err == nil {
		return cleaned
	}
	return ""
}

func validExtractedAmount(value *string) string {
	cleaned := strings.NewReplacer("$", "", ",", "", " ", "").Replace(cleanExtractedValue(value))
	if !expenseAmountPattern.MatchString(cleaned) {
		return ""
	}
	rational, ok := new(big.Rat).SetString(cleaned)
	if !ok || rational.Sign() < 0 {
		return ""
	}
	return rational.FloatString(2)
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
