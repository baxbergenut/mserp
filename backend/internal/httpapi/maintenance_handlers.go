package httpapi

import (
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/jobs"
	"mserp/internal/openai"
	"mserp/internal/repository"
)

type maintenanceHandler struct {
	repo      *repository.MaintenanceRepository
	expenses  *repository.ExpenseRepository
	logger    *slog.Logger
	extractor openai.ExpenseExtractor
	eld       *jobs.SyncFiveELDJob
}

func registerMaintenanceRoutes(r chi.Router, pool *pgxpool.Pool, logger *slog.Logger, expenses *repository.ExpenseRepository, extractor openai.ExpenseExtractor, eld *jobs.SyncFiveELDJob) {
	h := maintenanceHandler{repo: repository.NewMaintenanceRepository(pool), expenses: expenses, logger: logger, extractor: extractor, eld: eld}
	r.Get("/maintenance", h.list)
	r.Post("/maintenance", h.save)
	r.Put("/maintenance/{id}", h.save)
	r.Get("/maintenance/{id}", h.get)
	r.Post("/maintenance/{id}/void", h.void)
	r.Get("/maintenance/{id}/history", h.history)
	r.Get("/maintenance/access", h.access)
	r.Get("/maintenance/options", h.options)
	r.Get("/maintenance/odometer", h.odometer)
	r.Get("/maintenance/reports", h.reports)
	r.Get("/maintenance/due", h.due)
	r.Get("/maintenance/preventative", h.preventative)
	r.Post("/maintenance/vendors", h.vendor)
	r.Put("/maintenance/vendors/{id}", h.vendor)
	r.Put("/maintenance/types/{code}", h.serviceType)
	r.Get("/maintenance/readings", h.readings)
	r.Post("/maintenance/readings", h.reading)
	r.Put("/maintenance/readings/{id}", h.reading)
	r.Post("/maintenance/schedules", h.schedule)
	r.Put("/maintenance/schedules/{id}", h.schedule)
	r.Post("/maintenance/{id}/documents", h.upload)
	r.Get("/maintenance/documents/{id}", h.download)
	r.Post("/maintenance/extract", h.extract)
}
func (h maintenanceHandler) allowed(w http.ResponseWriter, r *http.Request, action string) (string, bool) {
	id, err := h.repo.CategoryID(r.Context())
	if err != nil {
		h.fail(w, err)
		return "", false
	}
	session, _ := authSessionFromContext(r.Context())
	if !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, id, action) {
		writeAPIError(w, 403, "your role cannot "+action+" maintenance records")
		return "", false
	}
	return id, true
}
func (h maintenanceHandler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		writeAPIError(w, 404, "maintenance record not found")
		return
	}
	if errors.Is(err, repository.ErrMaintenanceConflict) || errors.Is(err, repository.ErrExpenseSettingConflict) {
		writeAPIError(w, 409, "maintenance or its invoice changed; reopen before saving")
		return
	}
	if errors.Is(err, repository.ErrMaintenanceInvalid) {
		writeAPIError(w, 400, err.Error())
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23514", "23503", "23505", "22P02", "22003":
			writeAPIError(w, 400, "check the work costs, odometer, vendor and invoice; paid or finalized charges retain their financial identity")
			return
		}
	}
	h.logger.Error("maintenance request failed", "error", err)
	writeAPIError(w, 500, "maintenance request could not be completed")
}
func maintenanceQuery(r *http.Request) (repository.MaintenanceQuery, error) {
	q := r.URL.Query()
	out := repository.MaintenanceQuery{TruckID: q.Get("truckId"), VendorID: q.Get("vendorId"), TypeCode: q.Get("typeCode"), Status: q.Get("status"), Search: q.Get("search"), From: q.Get("dateFrom"), To: q.Get("dateTo"), ExpenseID: q.Get("expenseId")}
	for _, id := range []string{out.TruckID, out.VendorID, out.ExpenseID} {
		if id != "" && !isUUID(id) {
			return out, errors.New("invalid maintenance filter")
		}
	}
	for _, date := range []string{out.From, out.To} {
		if date != "" {
			if _, err := time.Parse(time.DateOnly, date); err != nil {
				return out, errors.New("invalid date filter")
			}
		}
	}
	if out.From != "" && out.To != "" && out.From > out.To {
		return out, errors.New("start date must precede end date")
	}
	p, err := parsePagination(r)
	out.Page = p.Page
	out.PageSize = p.PageSize
	return out, err
}
func (h maintenanceHandler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	q, err := maintenanceQuery(r)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	v, err := h.repo.List(r.Context(), q)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) get(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := h.repo.Get(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) history(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := h.repo.History(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) reports(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	q, err := maintenanceQuery(r)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	v, err := h.repo.Reports(r.Context(), q)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) due(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	q, err := maintenanceQuery(r)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	today, _ := time.Parse(time.DateOnly, expenseSubmissionDate().Format(time.DateOnly))
	v, err := h.repo.Due(r.Context(), q.TruckID, today)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

type maintenanceRequest struct {
	OilType       string                       `json:"oilType"`
	ID            string                       `json:"id"`
	Version       int                          `json:"version"`
	TruckID       *string                      `json:"truckId"`
	DriverID      *string                      `json:"driverId"`
	DriverName    string                       `json:"driverName"` // Display only; persistence resolves the selected driver's name.
	VendorID      *string                      `json:"vendorId"`
	VendorName    string                       `json:"vendorName"`
	VendorAddress string                       `json:"vendorAddress"`
	VendorPhone   string                       `json:"vendorPhone"`
	VendorContact string                       `json:"vendorContact"`
	ServiceDate   string                       `json:"serviceDate"`
	Odometer      *int                         `json:"odometer"`
	Location      string                       `json:"location"`
	Status        string                       `json:"status"`
	WorkOrder     string                       `json:"workOrder"`
	Notes         string                       `json:"notes"`
	Services      []repository.MaintenanceWork `json:"services"`
	Expense       *expenseRequest              `json:"expense"`
}

func (in maintenanceRequest) validate(category string) (repository.MaintenanceInput, error) {
	out := repository.MaintenanceInput{OilType: repository.NormalizeMaintenanceOilType(in.OilType), ID: in.ID, Version: in.Version, TruckID: in.TruckID, VendorID: in.VendorID, VendorName: strings.TrimSpace(in.VendorName), VendorAddress: strings.TrimSpace(in.VendorAddress), VendorPhone: strings.TrimSpace(in.VendorPhone), VendorContact: strings.TrimSpace(in.VendorContact), Odometer: in.Odometer, Location: in.Location, Status: in.Status, WorkOrder: strings.TrimSpace(in.WorkOrder), Notes: strings.TrimSpace(in.Notes), Services: in.Services}
	out.DriverID = in.DriverID
	if !isUUID(in.ID) || in.Version < 0 {
		return out, errors.New("invalid maintenance identity")
	}
	for _, id := range []*string{in.TruckID, in.VendorID, in.DriverID} {
		if id != nil && !isUUID(*id) {
			return out, errors.New("select a valid truck and vendor")
		}
	}
	if out.Status != "completed" && out.Status != "needs_review" {
		return out, errors.New("choose completed or needs review")
	}
	var err error
	out.ServiceDate, err = parseOptionalDate(in.ServiceDate, "service date")
	if err != nil {
		return out, err
	}
	if out.ServiceDate != nil && in.ServiceDate > expenseSubmissionDate().Format(time.DateOnly) {
		return out, errors.New("completed service dates cannot be in the future")
	}
	if out.Status == "completed" && (out.TruckID == nil || out.ServiceDate == nil) {
		return out, errors.New("completed work requires a truck and service date")
	}
	if in.Odometer != nil && (*in.Odometer < 0 || *in.Odometer > 2000000000) {
		return out, errors.New("odometer must be a nonnegative whole number")
	}
	if out.Location != "shop" && out.Location != "roadside" && out.Location != "internal" && out.Location != "unknown" {
		return out, errors.New("select a service location")
	}
	for _, v := range []string{out.VendorName, out.VendorAddress, out.VendorPhone, out.VendorContact, out.WorkOrder} {
		if utf8.RuneCountInString(v) > 500 {
			return out, errors.New("service details are too long")
		}
	}
	if utf8.RuneCountInString(out.OilType) > 100 {
		return out, errors.New("oil type must be at most 100 characters")
	}
	if utf8.RuneCountInString(out.Notes) > 5000 {
		return out, errors.New("notes must be 5000 characters or fewer")
	}
	if len(out.Services) < 1 || len(out.Services) > 100 {
		return out, errors.New("add between 1 and 100 work items")
	}
	costs := []repository.ExpenseCostItem{}
	priced := 0
	for i := range out.Services {
		s := &out.Services[i]
		s.TypeCode = repository.MaintenanceTypeCode(s.TypeCode)
		s.Name = strings.TrimSpace(s.Name)
		if s.TypeCode == "" {
			return out, errors.New("each service needs a service type")
		}
		if s.Name == "" {
			s.Name = s.TypeCode
		}
		s.TrackingCode = repository.MaintenanceTrackingCode(s.TypeCode)
		if s.Amount != nil {
			v := strings.TrimSpace(*s.Amount)
			s.Amount = &v
			if !expenseAmountPattern.MatchString(v) {
				return out, errors.New("work costs must have at most two decimal places")
			}
			priced++
			costs = append(costs, repository.ExpenseCostItem{Name: s.Name, Amount: v})
		}
	}
	if priced > 0 && priced != len(out.Services) {
		return out, errors.New("price every work item or leave all costs unallocated")
	}
	if in.Expense != nil {
		if in.ServiceDate != "" {
			in.Expense.ExpenseDate = in.ServiceDate
		}
		if in.Expense.ExpenseType == "" {
			in.Expense.ExpenseType = out.Services[0].Name
		}
		in.Expense.DriverID = in.DriverID
		in.Expense.CategoryID = category
		in.Expense.TruckID = in.TruckID
		in.Expense.CostBreakdown = &costs
		e, err := in.Expense.validate()
		if err != nil {
			return out, err
		}
		out.Expense = &e
	} else if priced > 0 {
		if err := validateCostBreakdown(costs, "0"); err != nil {
			return out, errors.New("nonzero work costs require an invoice")
		}
	}
	return out, nil
}
func (h maintenanceHandler) save(w http.ResponseWriter, r *http.Request) {
	action := "create"
	if r.Method == "PUT" {
		action = "edit"
	}
	cat, ok := h.allowed(w, r, action)
	if !ok {
		return
	}
	var request maintenanceRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	if r.Method == "PUT" {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		request.ID = id
		if request.Version < 1 {
			writeAPIError(w, 400, "record version is required")
			return
		}
	} else if request.Version != 0 {
		writeAPIError(w, 400, "new records must have version zero")
		return
	}
	in, err := request.validate(cat)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	in.Actor = session.User.Username
	in.ActorID = session.User.ID
	if in.Expense != nil {
		in.Expense.CreatedBy = session.User.ID
		in.Expense.CreatedByName = session.User.Username
	}
	v, err := h.repo.Save(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) void(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "delete"); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Version int    `json:"version"`
		Reason  string `json:"reason"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Version < 1 || len(in.Reason) < 3 || len(in.Reason) > 1000 {
		writeAPIError(w, 400, "a record version and reason are required")
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if err := h.repo.Void(r.Context(), id, in.Version, in.Reason, session.User.Username); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h maintenanceHandler) options(w http.ResponseWriter, r *http.Request) {
	cat, err := h.repo.CategoryID(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, cat, "view") && !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, cat, "create") {
		writeAPIError(w, 403, "your role cannot view or create maintenance")
		return
	}
	types, err := h.repo.Types(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	vendors, err := h.repo.Vendors(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	schedules, err := h.repo.Schedules(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	settings, err := h.expenses.ListExpenseSettings(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	names := []string{}
	methods := []string{}
	for _, s := range settings {
		if s.Active {
			if s.Kind == "name" && s.CategoryID != nil && *s.CategoryID == cat {
				names = append(names, s.Name)
			}
			if s.Kind == "payment_method" {
				methods = append(methods, s.Name)
			}
		}
	}
	trucks, users, err := h.repo.Choices(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	drivers, err := h.repo.DriverChoices(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"categoryId": cat, "types": types, "vendors": vendors, "schedules": schedules, "names": names, "paymentMethods": methods, "users": users, "currentUser": session.User.Username, "currentUserId": session.User.ID, "trucks": trucks, "drivers": drivers, "administrator": session.User.Administrator, "verificationRole": expenseVerificationRole(session.User)})
}
func (h maintenanceHandler) vendor(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	var in repository.MaintenanceVendor
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 200 {
		writeAPIError(w, 400, "vendor name is required, up to 200 characters")
		return
	}
	for _, s := range []string{in.Address, in.Phone, in.Contact} {
		if utf8.RuneCountInString(s) > 500 {
			writeAPIError(w, 400, "vendor details are too long")
			return
		}
	}
	if r.Method == "PUT" {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		in.ID = id
		if in.Version < 1 {
			writeAPIError(w, 400, "vendor version is required")
			return
		}
	} else {
		in.ID = ""
		in.Version = 0
	}
	v, err := h.repo.SaveVendor(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) serviceType(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !session.User.Administrator {
		writeAPIError(w, 403, "only administrators can manage service reporting types")
		return
	}
	var in repository.MaintenanceType
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	in.Code = chi.URLParam(r, "code")
	in.Name = strings.TrimSpace(in.Name)
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`).MatchString(in.Code) || in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		writeAPIError(w, 400, "use a stable type code and a name up to 100 characters")
		return
	}
	v, err := h.repo.SaveType(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) readings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	q, err := maintenanceQuery(r)
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	v, err := h.repo.Readings(r.Context(), q.TruckID)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) reading(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	var in repository.MaintenanceReading
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	if !isUUID(in.TruckID) || in.Mileage < 0 || in.Mileage > 2000000000 || in.Date > expenseSubmissionDate().Format(time.DateOnly) {
		writeAPIError(w, 400, "provide a truck, nonnegative odometer and a dated reading through today")
		return
	}
	if r.Method == "PUT" {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		in.ID = id
		if in.Version < 1 {
			writeAPIError(w, 400, "reading version is required")
			return
		}
	} else {
		in.ID = ""
	}
	session, _ := authSessionFromContext(r.Context())
	in.ActorName = session.User.Username
	v, err := h.repo.SaveReading(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) schedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "edit"); !ok {
		return
	}
	var in repository.MaintenanceSchedule
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if in.TruckID == nil && !session.User.Administrator {
		writeAPIError(w, 403, "only administrators can set fleet service defaults")
		return
	}
	if in.TruckID != nil && !isUUID(*in.TruckID) {
		writeAPIError(w, 400, "select a valid truck")
		return
	}
	if in.ServiceCode != "pm" || in.WarningDays < 0 || in.WarningMiles < 0 || in.WarningDays > 36500 || in.WarningMiles > 2000000000 || in.IntervalDays != nil && (*in.IntervalDays < 1 || *in.IntervalDays > 36500) || in.IntervalMiles != nil && (*in.IntervalMiles < 1 || *in.IntervalMiles > 2000000000) || in.Enabled && in.IntervalDays == nil && in.IntervalMiles == nil {
		writeAPIError(w, 400, "set a positive mileage/day interval and nonnegative reminder thresholds")
		return
	}
	if r.Method == "PUT" {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		in.ID = id
		if in.Version < 1 {
			writeAPIError(w, 400, "schedule version is required")
			return
		}
	} else {
		in.ID = ""
	}
	v, err := h.repo.SaveSchedule(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h maintenanceHandler) upload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cat, err := h.repo.CategoryID(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, cat, "edit") {
		own, err := h.repo.OwnedBy(r.Context(), id, session.User.ID)
		if err != nil {
			h.fail(w, err)
			return
		}
		if !own || !hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, cat, "create") {
			writeAPIError(w, 403, "your role cannot attach documents to this visit")
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 22<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		writeAPIError(w, 400, "invalid maintenance attachment")
		return
	}
	defer r.MultipartForm.RemoveAll()
	header, err := firstMultipartFile(r, "file")
	if err != nil {
		writeAPIError(w, 400, err.Error())
		return
	}
	data, err := readMultipartFile(header, 20<<20)
	if err != nil {
		writeAPIError(w, 400, "attachment must be nonempty and no larger than 20 MB")
		return
	}
	kind := detectUploadContentType(header.Header.Get("Content-Type"), data)
	if !isSupportedDocumentType(kind) && kind != "text/plain" {
		writeAPIError(w, 400, "use PDF, TXT, PNG, JPEG or WEBP")
		return
	}
	v, err := h.repo.CreateDocument(r.Context(), id, safeUploadFileName(header.Filename, "maintenance-document"), kind, session.User.Username, data)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h maintenanceHandler) download(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.allowed(w, r, "view"); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	file, data, err := h.repo.Document(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	name, kind := file.FileName, file.ContentType
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func (h maintenanceHandler) extract(w http.ResponseWriter, r *http.Request) {
	cat, ok := h.allowed(w, r, "create")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 22<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		writeAPIError(w, 400, "invalid maintenance analysis request")
		return
	}
	defer r.MultipartForm.RemoveAll()
	in := openai.ExpenseInput{Text: strings.TrimSpace(r.FormValue("text")), MessageDate: expenseSubmissionDate(), Maintenance: true, Categories: map[string][]string{"Maintenance": {}}}
	if headers := r.MultipartForm.File["file"]; len(headers) > 0 {
		data, err := readMultipartFile(headers[0], 20<<20)
		if err != nil {
			writeAPIError(w, 400, "attachment must be nonempty and no larger than 20 MB")
			return
		}
		in.FileData = data
		in.MIMEType = detectUploadContentType(headers[0].Header.Get("Content-Type"), data)
		in.FileName = safeUploadFileName(headers[0].Filename, "maintenance-receipt")
		if !isSupportedDocumentType(in.MIMEType) && in.MIMEType != "text/plain" {
			writeAPIError(w, 400, "use PDF, TXT, PNG, JPEG or WEBP")
			return
		}
	}
	if in.Text == "" && len(in.FileData) == 0 {
		writeAPIError(w, 400, "provide maintenance text or a document")
		return
	}
	settings, err := h.expenses.ListExpenseSettings(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	for _, s := range settings {
		if s.Active && s.Kind == "name" && s.CategoryID != nil && *s.CategoryID == cat {
			in.Categories["Maintenance"] = append(in.Categories["Maintenance"], s.Name)
		}
		if s.Active && s.Kind == "payment_method" {
			in.PaymentMethods = append(in.PaymentMethods, s.Name)
		}
	}
	types, err := h.repo.Types(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	in.ServiceTypes = map[string]string{}
	for _, t := range types {
		if t.Active {
			in.ServiceTypes[t.Code] = t.Name
		}
	}
	result, err := h.extractor.ExtractExpense(r.Context(), in)
	if err != nil {
		if errors.Is(err, openai.ErrNotConfigured) {
			writeAPIError(w, 503, "OPENAI_API_KEY is not configured")
			return
		}
		writeAPIError(w, 502, err.Error())
		return
	}
	if !result.IsExpense {
		result.Expenses = nil
	}
	if len(result.Expenses) > 25 {
		writeAPIError(w, 502, "analyze at most 25 maintenance visits at a time")
		return
	}
	drafts := []map[string]any{}
	for _, item := range result.Expenses {
		unit := cleanExtractedValue(item.UnitNumber)
		driver := cleanExtractedValue(item.DriverName)
		truckID, driverID, err := h.expenses.ResolveExpenseLinks(r.Context(), stringPointer(unit), stringPointer(driver))
		if err != nil {
			h.fail(w, err)
			return
		}
		if item.Maintenance != nil && item.Maintenance.OilType != nil {
			grade := repository.NormalizeMaintenanceOilType(*item.Maintenance.OilType)
			item.Maintenance.OilType = &grade
		}
		drafts = append(drafts, map[string]any{"categoryId": cat, "truckId": truckID, "driverId": driverID, "unitNumber": unit, "driverName": driver, "amount": validExtractedAmount(item.Amount), "expenseDate": validExtractedDate(item.ExpenseDate), "paymentType": cleanExtractedValue(item.PaymentType), "expenseType": cleanExtractedValue(item.ExpenseType), "referenceNumber": cleanExtractedValue(item.ReferenceNumber), "description": cleanExtractedValue(item.Description), "coveredBy": defaultExtractedValue(item.CoveredBy, "Company"), "paidBy": cleanExtractedValue(item.PaidBy), "maintenance": item.Maintenance, "costBreakdown": extractedCostBreakdown(item.CostBreakdown), "evidence": item.Evidence})
	}
	writeJSON(w, 200, map[string]any{"drafts": drafts})
}

func (h maintenanceHandler) access(w http.ResponseWriter, r *http.Request) {
	id, err := h.repo.CategoryID(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	session, _ := authSessionFromContext(r.Context())
	writeJSON(w, 200, map[string]any{"categoryId": id, "canView": hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, id, "view"), "canCreate": hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, id, "create"), "canEdit": hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, id, "edit"), "canDelete": hasExpenseCategoryAction(session.User.ExpenseCategoryAccess, id, "delete")})
}
