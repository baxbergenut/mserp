package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mserp/internal/phone"
	"mserp/internal/repository"
)

type fleetHandler struct {
	logger *slog.Logger
	repo   *repository.FleetRepository
}

func registerFleetRoutes(r chi.Router, logger *slog.Logger, repo *repository.FleetRepository) {
	handler := fleetHandler{logger: logger, repo: repo}
	r.Get("/fleet-status/catalog", handler.fleetStatusCatalog)
	r.Get("/drivers/{id}/status-history", handler.fleetStatusHistory)
	r.Patch("/drivers/{id}/availability", handler.changeAvailability)
	r.Get("/trucks/{id}/status-history", handler.fleetStatusHistory)
	r.Patch("/trucks/{id}/condition", handler.changeTruckCondition)

	r.Get("/tasks/five-eld/{id}", handler.fiveELDTruckTask)
	r.Post("/tasks/five-eld/{id}/complete", handler.fiveELDTruckTask)
	r.Get("/investors", handler.listInvestors)
	r.Get("/investors/{id}", handler.getInvestor)
	r.Post("/investors", handler.saveInvestor)
	r.Put("/investors/{id}", handler.saveInvestor)
	r.Get("/drivers", handler.listDrivers)
	r.Get("/drivers/setup-defaults", handler.getDriverSetupDefaults)
	r.Get("/drivers/team-trucks", handler.driverTeamTrucks)
	r.Get("/drivers/{id}", handler.getDriver)
	r.Get("/drivers/{id}/assignments", handler.getDriverAssignments)
	r.Get("/drivers/{id}/team", handler.driverTeam)
	r.Put("/drivers/{id}/team", handler.driverTeam)
	r.Get("/drivers/{id}/notes", handler.profileNotes)
	r.Post("/drivers/{id}/notes", handler.profileNotes)
	r.Get("/drivers/{id}/location", handler.getDriverTruckLocation)
	r.Post("/drivers", handler.createDriver)
	r.Put("/drivers/{id}", handler.updateDriver)
	r.Patch("/drivers/{id}/status", handler.changeDriverStatus)
	r.Delete("/drivers/{id}", handler.deleteDriver)
	r.Get("/trucks", handler.listTrucks)
	r.Get("/trucks/{id}", handler.getTruck)
	r.Get("/trucks/{id}/assignments", handler.getTruckAssignments)
	r.Get("/trucks/{id}/notes", handler.profileNotes)
	r.Post("/trucks/{id}/notes", handler.profileNotes)
	r.Get("/trucks/{id}/location", handler.getTruckLocation)
	r.Post("/trucks", handler.createTruck)
	r.Put("/trucks/{id}", handler.updateTruck)
	r.Delete("/trucks/{id}", handler.deleteTruck)
	r.Get("/updaters", handler.listUpdaters)
	r.Post("/updaters", handler.saveUpdater)
	r.Put("/updaters/{id}", handler.saveUpdater)
	r.Delete("/updaters/{id}", handler.deleteUpdater)
	r.Get("/dispatchers", handler.listDispatchers)
	r.Get("/dispatchers/{id}", handler.getDispatcher)
	r.Post("/dispatchers", handler.createDispatcher)
	r.Put("/dispatchers/{id}", handler.updateDispatcher)
	r.Delete("/dispatchers/{id}", handler.deleteDispatcher)
}

func (handler fleetHandler) getDriverSetupDefaults(w http.ResponseWriter, r *http.Request) {
	value, err := handler.repo.GetDriverSetupDefaults(r.Context())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler fleetHandler) getDriver(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	value, err := handler.repo.GetDriver(r.Context(), id)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler fleetHandler) getDriverAssignments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	value, err := handler.repo.DriverAssignmentHistory(r.Context(), id)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (handler fleetHandler) getTruck(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	value, err := handler.repo.GetTruck(r.Context(), id)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

type driverRequest struct {
	StatusVersion         *int `json:"statusVersion"`
	preserveFields        []string
	NewTruck              *truckRequest                    `json:"newTruck"`
	NewTruckOwnedByDriver bool                             `json:"newTruckOwnedByDriver"`
	Services              []repository.DriverServiceIntent `json:"services"`
	IsTeamDriver          *bool                            `json:"isTeamDriver"`
	TeamVersion           *int                             `json:"teamVersion"`
	TeamMainDriverID      *string                          `json:"teamMainDriverId"`
	TeamMainVersion       *int                             `json:"teamMainVersion"`
	Status                string                           `json:"status"`
	TerminationDate       string                           `json:"terminationDate"`
	AssignmentWeek        string                           `json:"assignmentWeek"`
	ChargePauseWeek       string                           `json:"chargePauseWeek"`
	EscrowAmount          string                           `json:"escrowAmount"`
	FullName              string                           `json:"fullName"`
	FirstName             *string                          `json:"firstName"`
	LastName              *string                          `json:"lastName"`
	IsOwnerOperator       bool                             `json:"isOwnerOperator"`
	PayType               string                           `json:"payType"`
	PayRate               float64                          `json:"payRate"`
	Phone                 string                           `json:"phone"`
	Email                 string                           `json:"email"`
	LicenseNumber         string                           `json:"licenseNumber"`
	LicenseState          string                           `json:"licenseState"`
	LicenseExpires        string                           `json:"licenseExpires"`
	HireDate              string                           `json:"hireDate"`
	Address               string                           `json:"address"`
	City                  string                           `json:"city"`
	State                 string                           `json:"state"`
	PostalCode            string                           `json:"postalCode"`
	EmergencyContact      string                           `json:"emergencyContact"`
	DispatcherID          *string                          `json:"dispatcherId"`
	TruckID               *string                          `json:"truckId"`
	Active                bool                             `json:"active"`
	Notes                 string                           `json:"notes"`
	CDLFileID             *string                          `json:"cdlFileId"`
}

func (request driverRequest) validate() (repository.DriverInput, error) {
	if request.IsTeamDriver != nil && *request.IsTeamDriver {
		request.PayType = "cpm"
		request.PayRate = 0
		request.EscrowAmount = ""
	}
	if (request.TeamVersion != nil && *request.TeamVersion < 0) || (request.TeamMainVersion != nil && *request.TeamMainVersion < 0) {
		return repository.DriverInput{}, errors.New("invalid team version")
	}
	if err := validateOptionalUUID(request.TeamMainDriverID, "main driver id"); err != nil {
		return repository.DriverInput{}, err
	}
	if request.Status != "" && request.Status != "active" && request.Status != "vacation" && request.Status != "home" && request.Status != "terminated" {
		return repository.DriverInput{}, errors.New("invalid driver status")
	}
	if request.Status != "" {
		request.Active = request.Status != "terminated"
	}
	terminationDate, dateErr := parseOptionalDate(request.TerminationDate, "termination date")
	if dateErr != nil {
		return repository.DriverInput{}, dateErr
	}
	location, _ := time.LoadLocation("America/New_York")
	if terminationDate != nil && terminationDate.Format("2006-01-02") > time.Now().In(location).Format("2006-01-02") {
		return repository.DriverInput{}, errors.New("termination date cannot be in the future")
	}
	request.FullName = strings.TrimSpace(request.FullName)
	if request.FirstName != nil || request.LastName != nil {
		if request.FirstName == nil || request.LastName == nil {
			return repository.DriverInput{}, errors.New("given and family names must be supplied together")
		}
		first := strings.Join(strings.Fields(*request.FirstName), " ")
		last := strings.Join(strings.Fields(*request.LastName), " ")
		if len([]rune(first)) < 1 || len([]rune(first)) > 50 || len([]rune(last)) < 1 || len([]rune(last)) > 50 {
			return repository.DriverInput{}, errors.New("given and family names must each contain 1–50 characters")
		}
		request.FirstName, request.LastName = &first, &last
		request.FullName = first + " " + last
	}
	if request.FullName == "" {
		return repository.DriverInput{}, errors.New("full name is required")
	}
	normalizedPhone, err := phone.Normalize(request.Phone)
	if err != nil {
		return repository.DriverInput{}, err
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
	request.EscrowAmount = strings.TrimSpace(request.EscrowAmount)
	if request.EscrowAmount != "" && !validDriverEscrowAmount(request.EscrowAmount) {
		return repository.DriverInput{}, errors.New("escrow amount must be greater than zero with at most two decimal places")
	}
	var newTruck *repository.TruckInput
	if request.NewTruck != nil {
		if request.TruckID != nil || !request.Active || (request.IsTeamDriver != nil && *request.IsTeamDriver) {
			return repository.DriverInput{}, errors.New("new trucks require an active solo driver without an existing truck selection")
		}
		if request.NewTruck.DriverID != nil {
			return repository.DriverInput{}, errors.New("the new truck is assigned to this driver automatically")
		}
		truckRequest := *request.NewTruck
		truckRequest.Active = true
		truckRequest.Status = "available"
		truckRequest.AssignmentWeek = ""
		if request.NewTruckOwnedByDriver {
			truckRequest.OwnerID = nil
		}
		truck, err := truckRequest.validate()
		if err != nil {
			return repository.DriverInput{}, err
		}
		newTruck = &truck
	} else if request.NewTruckOwnedByDriver {
		return repository.DriverInput{}, errors.New("driver ownership requires a new truck")
	}
	return repository.DriverInput{
		StatusVersion:  request.StatusVersion,
		PreserveFields: request.preserveFields,
		NewTruck:       newTruck, NewTruckOwnedByDriver: request.NewTruckOwnedByDriver,
		IsTeamDriver: request.IsTeamDriver, TeamVersion: request.TeamVersion, TeamMainDriverID: request.TeamMainDriverID, TeamMainVersion: request.TeamMainVersion,
		Status: request.Status, TerminationDate: terminationDate,
		AssignmentWeek:  request.AssignmentWeek,
		ChargePauseWeek: request.ChargePauseWeek,
		EscrowAmount:    request.EscrowAmount,
		FullName:        request.FullName, // Ownership is derived from the assigned truck.
		FirstName:       request.FirstName,
		LastName:        request.LastName,
		PayType:         request.PayType, PayRate: request.PayRate,
		Phone: optionalString(normalizedPhone), Email: optionalString(request.Email),
		LicenseNumber: optionalString(request.LicenseNumber), LicenseState: optionalString(request.LicenseState),
		LicenseExpires: licenseExpires, HireDate: hireDate, Address: optionalString(request.Address),
		City: optionalString(request.City), State: optionalString(request.State), PostalCode: optionalString(request.PostalCode),
		EmergencyContact: optionalString(request.EmergencyContact), DispatcherID: request.DispatcherID,
		TruckID: request.TruckID, Active: request.Active, Notes: optionalString(request.Notes),
		CDLFileID: request.CDLFileID,
	}, nil
}

type truckRequest struct {
	StatusVersion            *int `json:"statusVersion"`
	AddToDataTruck           bool `json:"addToDataTruck"`
	preserveFields           []string
	OwnershipEffectiveDate   string  `json:"ownershipEffectiveDate"`
	ExpectedOwnershipVersion string  `json:"expectedOwnershipVersion"`
	OwnerDriverID            *string `json:"ownerDriverId"`
	AssignmentWeek           string  `json:"assignmentWeek"`
	OwnerID                  *string `json:"ownerId"`
	UnitNumber               string  `json:"unitNumber"`
	VIN                      string  `json:"vin"`
	Year                     *int    `json:"year"`
	Make                     string  `json:"make"`
	Model                    string  `json:"model"`
	Color                    *string `json:"color"`
	LicensePlate             string  `json:"licensePlate"`
	LicenseState             string  `json:"licenseState"`
	IsCompanyOwned           bool    `json:"isCompanyOwned"`
	Status                   string  `json:"status"`
	Mileage                  *int    `json:"mileage"`
	RegistrationExpires      string  `json:"registrationExpires"`
	InsuranceExpires         string  `json:"insuranceExpires"`
	LastServiceDate          string  `json:"lastServiceDate"`
	NextServiceMiles         *int    `json:"nextServiceMiles"`
	DriverID                 *string `json:"driverId"`
	Active                   bool    `json:"active"`
	Notes                    string  `json:"notes"`
	IRPFileID                *string `json:"irpFileId"`
}

func (request truckRequest) validate() (repository.TruckInput, error) {
	if request.Color != nil {
		color := strings.TrimSpace(*request.Color)
		if len([]rune(color)) > 30 || strings.ContainsAny(color, "\x00\r\n") {
			return repository.TruckInput{}, errors.New("truck color must be at most 30 characters on one line")
		}
		request.Color = &color
	}
	if err := validateOptionalUUID(request.OwnerDriverID, "owner driver id"); err != nil {
		return repository.TruckInput{}, err
	}
	if request.OwnerDriverID != nil && request.OwnerID != nil {
		return repository.TruckInput{}, errors.New("choose one truck owner")
	}
	if err := validateOptionalUUID(request.OwnerID, "owner id"); err != nil {
		return repository.TruckInput{}, err
	}
	request.UnitNumber = strings.TrimSpace(request.UnitNumber)
	if request.UnitNumber == "" {
		return repository.TruckInput{}, errors.New("unit number is required")
	}
	validStatus := map[string]bool{
		"available": true, "assigned": true, "maintenance": true, "out_of_service": true, "hold": true,
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
		StatusVersion:            request.StatusVersion,
		AddToDataTruck:           request.AddToDataTruck,
		PreserveFields:           request.preserveFields,
		OwnershipEffectiveDate:   request.OwnershipEffectiveDate,
		ExpectedOwnershipVersion: request.ExpectedOwnershipVersion,
		AssignmentWeek:           request.AssignmentWeek,
		OwnerDriverID:            request.OwnerDriverID, OwnerID: request.OwnerID, UnitNumber: request.UnitNumber, VIN: optionalString(request.VIN), Year: request.Year,
		Make: optionalString(request.Make), Model: optionalString(request.Model), Color: request.Color,
		LicensePlate: optionalString(request.LicensePlate), LicenseState: optionalString(request.LicenseState),
		IsCompanyOwned: request.IsCompanyOwned, Status: request.Status, Mileage: request.Mileage,
		RegistrationExpires: registrationExpires, InsuranceExpires: insuranceExpires,
		LastServiceDate: lastServiceDate, NextServiceMiles: request.NextServiceMiles,
		DriverID: request.DriverID, Active: request.Active, Notes: optionalString(request.Notes),
		IRPFileID: request.IRPFileID,
	}, nil
}

type dispatcherRequest struct {
	preserveFields []string
	Extension      json.RawMessage                `json:"extension"`
	Updaters       *repository.UpdaterAssignments `json:"updaters"`
	AssignmentWeek string                         `json:"assignmentWeek"`
	FullName       string                         `json:"fullName"`
	Email          string                         `json:"email"`
	Phone          string                         `json:"phone"`
	PayPercentage  *float64                       `json:"payPercentage"`
	DriverIDs      []string                       `json:"driverIds"`
	Active         bool                           `json:"active"`
	Notes          string                         `json:"notes"`
}

func (request dispatcherRequest) validate() (repository.DispatcherInput, error) {
	var extension *int
	if len(request.Extension) > 0 {
		if err := json.Unmarshal(request.Extension, &extension); err != nil {
			return repository.DispatcherInput{}, errors.New("extension must be a whole number")
		}
	}
	if err := validateExtension(extension); err != nil {
		return repository.DispatcherInput{}, err
	}
	if request.Updaters != nil {
		for _, id := range []*string{request.Updaters.MainUpdaterID, request.Updaters.AfterHoursUpdaterID} {
			if err := validateOptionalUUID(id, "updater id"); err != nil {
				return repository.DispatcherInput{}, err
			}
		}
	}
	request.FullName = strings.TrimSpace(request.FullName)
	if request.FullName == "" {
		return repository.DispatcherInput{}, errors.New("full name is required")
	}
	normalizedPhone, err := phone.Normalize(request.Phone)
	if err != nil {
		return repository.DispatcherInput{}, err
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
		PreserveFields: request.preserveFields,
		Extension:      extension, ExtensionSet: len(request.Extension) > 0, Updaters: request.Updaters,
		AssignmentWeek: request.AssignmentWeek,
		FullName:       request.FullName, Email: optionalString(request.Email), Phone: optionalString(normalizedPhone),
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
	if request.NewTruck != nil && !handler.authorizeTruckEffects(w, r, *request.NewTruck) {
		return
	}
	if request.NewTruck != nil && !handler.authorizeAttachment(w, r, request.NewTruck.IRPFileID, "trucks") {
		return
	}
	if !authorizeDriverSetup(w, r, request) {
		return
	}
	if !slices.Contains(request.preserveFields, "cdlFileId") && !handler.authorizeAttachment(w, r, request.CDLFileID, "drivers") {
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if input.IsTeamDriver != nil && *input.IsTeamDriver && !slices.Contains(session.User.Permissions, "driver_pay.edit") {
		writeAPIError(w, 403, "Payroll write permission is required to set up a team driver")
		return
	}
	input.ChargeActor = session.User.ID
	r, allowed := serviceIntentContext(w, r, request.Services)
	if !allowed {
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
	currentProfile, readErr := handler.repo.GetDriver(r.Context(), id)
	if readErr != nil {
		handler.writeError(w, readErr)
		return
	}
	var request driverRequest
	if !decodeProtectedProfile(w, r, &request, currentProfile, "drivers") {
		return
	}
	if !slices.Contains(request.preserveFields, "cdlFileId") && !handler.authorizeAttachment(w, r, request.CDLFileID, "drivers") {
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	if input.IsTeamDriver != nil && !*input.IsTeamDriver && currentProfile.TeamRole != "team" {
		input.IsTeamDriver = nil
	}
	if input.IsTeamDriver != nil && !slices.Contains(session.User.Permissions, "driver_pay.edit") {
		current, readErr := handler.repo.GetDriver(r.Context(), id)
		if readErr != nil {
			handler.writeError(w, readErr)
			return
		}
		sameTruck := (input.TruckID == nil && current.TeamTruckID == nil) || (input.TruckID != nil && current.TeamTruckID != nil && *input.TruckID == *current.TeamTruckID)
		sameWeek := input.AssignmentWeek == "" || (current.TeamStartWeek != nil && input.AssignmentWeek == *current.TeamStartWeek)
		if input.TeamVersion != nil && *input.TeamVersion != current.TeamVersion {
			handler.writeError(w, repository.ErrDriverTeamConflict)
			return
		}
		if !*input.IsTeamDriver || current.TeamRole != "team" || !sameTruck || !sameWeek {
			writeAPIError(w, 403, "Payroll write permission is required to change driver teams")
			return
		}
		// Fleet editors may still change contact/documents on an existing member.
		// Preserve the team relationship and its primary truck assignment.
		input.IsTeamDriver = nil
		input.TruckID = nil
		input.DispatcherID = current.DispatcherID
	}
	input.ChargeActor = session.User.ID
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
	if !decodeProtectedProfile(w, r, &request, nil, "trucks") {
		return
	}
	if !handler.authorizeTruckEffects(w, r, request) {
		return
	}
	if !slices.Contains(request.preserveFields, "irpFileId") && !handler.authorizeAttachment(w, r, request.IRPFileID, "trucks") {
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	input.StatusActor = session.User.ID
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
	currentProfile, readErr := handler.repo.GetTruck(r.Context(), id)
	if readErr != nil {
		handler.writeError(w, readErr)
		return
	}
	var request truckRequest
	if !decodeProtectedProfile(w, r, &request, currentProfile, "trucks") {
		return
	}
	if !handler.authorizeTruckEffects(w, r, request) {
		return
	}
	if !slices.Contains(request.preserveFields, "irpFileId") && !handler.authorizeAttachment(w, r, request.IRPFileID, "trucks") {
		return
	}
	input, err := request.validate()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, _ := authSessionFromContext(r.Context())
	input.StatusActor = session.User.ID
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
	if !decodeProtectedProfile(w, r, &request, nil, "dispatchers") {
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
	currentProfile, readErr := handler.repo.GetDispatcher(r.Context(), id)
	if readErr != nil {
		handler.writeError(w, readErr)
		return
	}
	var request dispatcherRequest
	if !decodeProtectedProfile(w, r, &request, currentProfile, "dispatchers") {
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
	var invalid *repository.ChargeValidationError
	if errors.As(err, &invalid) {
		writeAPIError(w, 409, err.Error())
		return
	}
	if errors.Is(err, repository.ErrDriverTeamConflict) || errors.Is(err, repository.ErrUpdaterConflict) || errors.Is(err, repository.ErrDriverBoardConflict) || errors.Is(err, repository.ErrProfileNoteConflict) {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, repository.ErrInvestorConflict) || errors.Is(err, repository.ErrInactiveOwner) {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "record not found")
		return
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		if postgresError.ConstraintName == "fleet_status_invalid" {
			writeAPIError(w, 409, postgresError.Message)
			return
		}
		switch postgresError.Code {
		case "23514":
			if strings.HasPrefix(postgresError.Message, "Assign the truck to the main driver") || strings.HasPrefix(postgresError.Message, "A driver can belong to only one") {
				writeAPIError(w, http.StatusConflict, postgresError.Message)
				return
			}
			if strings.HasPrefix(postgresError.Message, "The selected week precedes") || strings.HasPrefix(postgresError.Message, "Reopen finalized") {
				writeAPIError(w, http.StatusConflict, postgresError.Message)
				return
			}
			writeAPIError(w, http.StatusBadRequest, "the request references invalid data")
			return
		case "23503", "22P02":
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
	if projector, ok := w.(interface{ ProjectJSON(any) any }); ok && status < 400 {
		value = projector.ProjectJSON(value)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	code := "request_failed"
	switch status {
	case 400:
		code = "validation_failed"
	case 401:
		code = "unauthenticated"
	case 403:
		code = "permission_denied"
	case 409:
		code = "conflict"
	}
	if message == "invalid CSRF token" {
		code = "csrf_invalid"
	}
	writeJSON(w, status, map[string]string{"error": message, "code": code})
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
