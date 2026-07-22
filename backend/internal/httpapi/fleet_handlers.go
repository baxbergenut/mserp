package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mserp/internal/repository"
)

type fleetHandler struct {
	logger *slog.Logger
	repo   *repository.FleetRepository
}

func registerFleetRoutes(r chi.Router, logger *slog.Logger, repo *repository.FleetRepository) {
	handler := fleetHandler{logger: logger, repo: repo}

	r.Get("/drivers", handler.listDrivers)
	r.Post("/drivers", handler.createDriver)
	r.Put("/drivers/{id}", handler.updateDriver)
	r.Delete("/drivers/{id}", handler.deleteDriver)
	r.Get("/trucks", handler.listTrucks)
	r.Post("/trucks", handler.createTruck)
	r.Put("/trucks/{id}", handler.updateTruck)
	r.Delete("/trucks/{id}", handler.deleteTruck)
	r.Get("/dispatchers", handler.listDispatchers)
	r.Post("/dispatchers", handler.createDispatcher)
	r.Put("/dispatchers/{id}", handler.updateDispatcher)
	r.Delete("/dispatchers/{id}", handler.deleteDispatcher)
}

type driverRequest struct {
	FullName         string  `json:"fullName"`
	IsOwnerOperator  bool    `json:"isOwnerOperator"`
	PayType          string  `json:"payType"`
	PayRate          float64 `json:"payRate"`
	Phone            string  `json:"phone"`
	Email            string  `json:"email"`
	LicenseNumber    string  `json:"licenseNumber"`
	LicenseState     string  `json:"licenseState"`
	LicenseExpires   string  `json:"licenseExpires"`
	HireDate         string  `json:"hireDate"`
	Address          string  `json:"address"`
	City             string  `json:"city"`
	State            string  `json:"state"`
	PostalCode       string  `json:"postalCode"`
	EmergencyContact string  `json:"emergencyContact"`
	DispatcherID     *string `json:"dispatcherId"`
	TruckID          *string `json:"truckId"`
	Active           bool    `json:"active"`
	Notes            string  `json:"notes"`
	CDLFileID        *string `json:"cdlFileId"`
}

func (request driverRequest) validate() (repository.DriverInput, error) {
	request.FullName = strings.TrimSpace(request.FullName)
	if request.FullName == "" {
		return repository.DriverInput{}, errors.New("full name is required")
	}
	if request.PayType != "cpm" && request.PayType != "gross_percentage" {
		return repository.DriverInput{}, errors.New("pay type must be cpm or gross_percentage")
	}
	if request.PayRate < 0 {
		return repository.DriverInput{}, errors.New("pay rate cannot be negative")
	}
	if request.PayType == "gross_percentage" && request.PayRate > 100 {
		return repository.DriverInput{}, errors.New("gross percentage cannot exceed 100")
	}
	if err := validateOptionalUUID(request.DispatcherID, "dispatcher id"); err != nil {
		return repository.DriverInput{}, err
	}
	if err := validateOptionalUUID(request.TruckID, "truck id"); err != nil {
		return repository.DriverInput{}, err
	}
	if err := validateOptionalUUID(request.CDLFileID, "CDL file id"); err != nil {
		return repository.DriverInput{}, err
	}
	licenseExpires, err := parseOptionalDate(request.LicenseExpires, "license expiration")
	if err != nil {
		return repository.DriverInput{}, err
	}
	hireDate, err := parseOptionalDate(request.HireDate, "hire date")
	if err != nil {
		return repository.DriverInput{}, err
	}
	return repository.DriverInput{
		FullName: request.FullName, IsOwnerOperator: request.IsOwnerOperator,
		PayType: request.PayType, PayRate: request.PayRate,
		Phone: optionalString(request.Phone), Email: optionalString(request.Email),
		LicenseNumber: optionalString(request.LicenseNumber), LicenseState: optionalString(request.LicenseState),
		LicenseExpires: licenseExpires, HireDate: hireDate, Address: optionalString(request.Address),
		City: optionalString(request.City), State: optionalString(request.State), PostalCode: optionalString(request.PostalCode),
		EmergencyContact: optionalString(request.EmergencyContact), DispatcherID: request.DispatcherID,
		TruckID: request.TruckID, Active: request.Active, Notes: optionalString(request.Notes),
		CDLFileID: request.CDLFileID,
	}, nil
}

type truckRequest struct {
	UnitNumber          string  `json:"unitNumber"`
	VIN                 string  `json:"vin"`
	Year                *int    `json:"year"`
	Make                string  `json:"make"`
	Model               string  `json:"model"`
	LicensePlate        string  `json:"licensePlate"`
	LicenseState        string  `json:"licenseState"`
	IsCompanyOwned      bool    `json:"isCompanyOwned"`
	Status              string  `json:"status"`
	Mileage             *int    `json:"mileage"`
	RegistrationExpires string  `json:"registrationExpires"`
	InsuranceExpires    string  `json:"insuranceExpires"`
	LastServiceDate     string  `json:"lastServiceDate"`
	NextServiceMiles    *int    `json:"nextServiceMiles"`
	DriverID            *string `json:"driverId"`
	Active              bool    `json:"active"`
	Notes               string  `json:"notes"`
	IRPFileID           *string `json:"irpFileId"`
}

func (request truckRequest) validate() (repository.TruckInput, error) {
	request.UnitNumber = strings.TrimSpace(request.UnitNumber)
	if request.UnitNumber == "" {
		return repository.TruckInput{}, errors.New("unit number is required")
	}
	validStatus := map[string]bool{
		"available": true, "assigned": true, "maintenance": true, "out_of_service": true,
	}
	if !validStatus[request.Status] {
		return repository.TruckInput{}, errors.New("invalid truck status")
	}
	if request.Year != nil && (*request.Year < 1900 || *request.Year > 2200) {
		return repository.TruckInput{}, errors.New("truck year must be between 1900 and 2200")
	}
	if request.Mileage != nil && *request.Mileage < 0 {
		return repository.TruckInput{}, errors.New("mileage cannot be negative")
	}
	if request.NextServiceMiles != nil && *request.NextServiceMiles < 0 {
		return repository.TruckInput{}, errors.New("next service mileage cannot be negative")
	}
	if err := validateOptionalUUID(request.DriverID, "driver id"); err != nil {
		return repository.TruckInput{}, err
	}
	if err := validateOptionalUUID(request.IRPFileID, "IRP file id"); err != nil {
		return repository.TruckInput{}, err
	}
	registrationExpires, err := parseOptionalDate(request.RegistrationExpires, "registration expiration")
	if err != nil {
		return repository.TruckInput{}, err
	}
	insuranceExpires, err := parseOptionalDate(request.InsuranceExpires, "insurance expiration")
	if err != nil {
		return repository.TruckInput{}, err
	}
	lastServiceDate, err := parseOptionalDate(request.LastServiceDate, "last service date")
	if err != nil {
		return repository.TruckInput{}, err
	}
	return repository.TruckInput{
		UnitNumber: request.UnitNumber, VIN: optionalString(request.VIN), Year: request.Year,
		Make: optionalString(request.Make), Model: optionalString(request.Model),
		LicensePlate: optionalString(request.LicensePlate), LicenseState: optionalString(request.LicenseState),
		IsCompanyOwned: request.IsCompanyOwned, Status: request.Status, Mileage: request.Mileage,
		RegistrationExpires: registrationExpires, InsuranceExpires: insuranceExpires,
		LastServiceDate: lastServiceDate, NextServiceMiles: request.NextServiceMiles,
		DriverID: request.DriverID, Active: request.Active, Notes: optionalString(request.Notes),
		IRPFileID: request.IRPFileID,
	}, nil
}

type dispatcherRequest struct {
	FullName      string   `json:"fullName"`
	Email         string   `json:"email"`
	Phone         string   `json:"phone"`
	PayPercentage *float64 `json:"payPercentage"`
	DriverIDs     []string `json:"driverIds"`
	Active        bool     `json:"active"`
	Notes         string   `json:"notes"`
}

func (request dispatcherRequest) validate() (repository.DispatcherInput, error) {
	request.FullName = strings.TrimSpace(request.FullName)
	if request.FullName == "" {
		return repository.DispatcherInput{}, errors.New("full name is required")
	}
	if request.PayPercentage != nil && (*request.PayPercentage < 0 || *request.PayPercentage > 100) {
		return repository.DispatcherInput{}, errors.New("pay percentage must be between 0 and 100")
	}
	seenDriverIDs := make(map[string]struct{}, len(request.DriverIDs))
	for _, id := range request.DriverIDs {
		if !isUUID(id) {
			return repository.DispatcherInput{}, errors.New("driver ids must be valid UUIDs")
		}
		seenDriverIDs[id] = struct{}{}
	}
	driverIDs := make([]string, 0, len(seenDriverIDs))
	for id := range seenDriverIDs {
		driverIDs = append(driverIDs, id)
	}
	return repository.DispatcherInput{
		FullName: request.FullName, Email: optionalString(request.Email), Phone: optionalString(request.Phone),
		PayPercentage: request.PayPercentage, DriverIDs: driverIDs,
		Active: request.Active, Notes: optionalString(request.Notes),
	}, nil
}

func (handler fleetHandler) listDrivers(w http.ResponseWriter, r *http.Request) {
	if wantsPagination(r) {
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		value, err := handler.repo.ListDriversPage(
			r.Context(), pagination, strings.TrimSpace(r.URL.Query().Get("search")),
			strings.EqualFold(r.URL.Query().Get("includeInactive"), "true"),
		)
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	values, err := handler.repo.ListDrivers(r.Context())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (handler fleetHandler) createDriver(w http.ResponseWriter, r *http.Request) {
	var request driverRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.CreateDriver(r.Context(), input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (handler fleetHandler) updateDriver(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var request driverRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.UpdateDriver(r.Context(), id, input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler fleetHandler) deleteDriver(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := handler.repo.DeleteDriver(r.Context(), id); err != nil {
		handler.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler fleetHandler) listTrucks(w http.ResponseWriter, r *http.Request) {
	if wantsPagination(r) {
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		value, err := handler.repo.ListTrucksPage(
			r.Context(), pagination, strings.TrimSpace(r.URL.Query().Get("search")),
		)
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	values, err := handler.repo.ListTrucks(r.Context())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (handler fleetHandler) createTruck(w http.ResponseWriter, r *http.Request) {
	var request truckRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.CreateTruck(r.Context(), input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (handler fleetHandler) updateTruck(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var request truckRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.UpdateTruck(r.Context(), id, input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler fleetHandler) deleteTruck(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := handler.repo.DeleteTruck(r.Context(), id); err != nil {
		handler.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler fleetHandler) listDispatchers(w http.ResponseWriter, r *http.Request) {
	if wantsPagination(r) {
		pagination, err := parsePagination(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		value, err := handler.repo.ListDispatchersPage(
			r.Context(), pagination, strings.TrimSpace(r.URL.Query().Get("search")),
		)
		if err != nil {
			handler.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	values, err := handler.repo.ListDispatchers(r.Context())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func wantsPagination(r *http.Request) bool {
	query := r.URL.Query()
	return query.Has("page") || query.Has("pageSize")
}

func (handler fleetHandler) createDispatcher(w http.ResponseWriter, r *http.Request) {
	var request dispatcherRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.CreateDispatcher(r.Context(), input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (handler fleetHandler) updateDispatcher(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var request dispatcherRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := handler.repo.UpdateDispatcher(r.Context(), id, input)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler fleetHandler) deleteDispatcher(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := handler.repo.DeleteDispatcher(r.Context(), id); err != nil {
		handler.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler fleetHandler) writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "record not found")
		return
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "23514", "22P02":
			writeAPIError(w, http.StatusBadRequest, "the request references invalid data")
			return
		case "23505":
			writeAPIError(w, http.StatusConflict, "a record with that unique value already exists")
			return
		}
	}
	handler.logger.Error("fleet request failed", "error", err)
	writeAPIError(w, http.StatusInternalServerError, "the request could not be completed")
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		writeAPIError(w, http.StatusBadRequest, "invalid record id")
		return "", false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func parseOptionalDate(value, label string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return nil, fmt.Errorf("%s must use YYYY-MM-DD", label)
	}
	return &parsed, nil
}

func validateOptionalUUID(value *string, label string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if !isUUID(trimmed) {
		return fmt.Errorf("%s must be a valid UUID", label)
	}
	*value = trimmed
	return nil
}

func isUUID(value string) bool {
	compact := strings.ReplaceAll(strings.TrimSpace(value), "-", "")
	if len(compact) != 32 {
		return false
	}
	_, err := hex.DecodeString(compact)
	return err == nil
}
