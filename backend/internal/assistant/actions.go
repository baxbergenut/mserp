package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mserp/internal/repository"
)

type fleetEnvelope struct {
	Entity  string         `json:"entity"`
	ID      string         `json:"id"`
	Record  map[string]any `json:"record"`
	Changes map[string]any `json:"changes"`
}

func decodeFleetEnvelope(raw json.RawMessage) (fleetEnvelope, error) {
	var args fleetEnvelope
	decoderErr := json.Unmarshal(raw, &args)
	if decoderErr != nil {
		return args, decoderErr
	}
	if args.Entity != "driver" && args.Entity != "truck" && args.Entity != "dispatcher" {
		return args, errors.New("entity must be driver, truck, or dispatcher")
	}
	return args, nil
}

func (e *ToolExecutor) createFleet(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	args, err := decodeFleetEnvelope(raw)
	if err != nil {
		return ToolResult{}, err
	}
	if args.Record == nil {
		return ToolResult{}, errors.New("record is required")
	}
	switch args.Entity {
	case "driver":
		input := repository.DriverInput{Active: true}
		if err := applyDriverChanges(&input, args.Record); err != nil {
			return ToolResult{}, err
		}
		if err := validateDriver(input); err != nil {
			return ToolResult{}, err
		}
		value, err := e.fleet.CreateDriver(ctx, input)
		return ToolResult{Data: value, Action: true}, err
	case "truck":
		input := repository.TruckInput{Active: true, Status: "available"}
		if err := applyTruckChanges(&input, args.Record); err != nil {
			return ToolResult{}, err
		}
		if err := validateTruck(input); err != nil {
			return ToolResult{}, err
		}
		value, err := e.fleet.CreateTruck(ctx, input)
		return ToolResult{Data: value, Action: true}, err
	case "dispatcher":
		input := repository.DispatcherInput{Active: true}
		if err := applyDispatcherChanges(&input, args.Record); err != nil {
			return ToolResult{}, err
		}
		if err := validateDispatcher(input); err != nil {
			return ToolResult{}, err
		}
		value, err := e.fleet.CreateDispatcher(ctx, input)
		return ToolResult{Data: value, Action: true}, err
	}
	return ToolResult{}, errors.New("unsupported entity")
}

func (e *ToolExecutor) updateFleet(ctx context.Context, identity repository.TelegramIdentity, raw json.RawMessage, confirmed bool) (ToolResult, error) {
	args, err := decodeFleetEnvelope(raw)
	if err != nil {
		return ToolResult{}, err
	}
	if args.ID == "" || len(args.Changes) == 0 {
		return ToolResult{}, errors.New("id and at least one explicit change are required")
	}
	before, err := e.currentFleetState(ctx, args.Entity, args.ID)
	if err != nil {
		return ToolResult{}, err
	}
	desired, err := validatedDesiredState(args.Entity, before, args.Changes)
	if err != nil {
		return ToolResult{}, err
	}
	highRisk := hasHighRiskChange(args.Entity, args.Changes)
	if highRisk && !confirmed {
		return e.pendingAction(ctx, identity, "update_fleet_record", raw, before, fmt.Sprintf("Confirm updating %s %s.\n\nBefore:\n%s\n\nAfter:\n%s", args.Entity, args.ID, prettyJSON(before), prettyJSON(desired)))
	}
	switch args.Entity {
	case "driver":
		current := before.(repository.Driver)
		input := driverInput(current)
		if err := applyDriverChanges(&input, args.Changes); err != nil {
			return ToolResult{}, err
		}
		if err := validateDriver(input); err != nil {
			return ToolResult{}, err
		}
		value, err := e.fleet.UpdateDriver(ctx, args.ID, input)
		return ToolResult{Data: value, Action: true, Before: before}, err
	case "truck":
		current := before.(repository.Truck)
		input := truckInput(current)
		if err := applyTruckChanges(&input, args.Changes); err != nil {
			return ToolResult{}, err
		}
		if err := validateTruck(input); err != nil {
			return ToolResult{}, err
		}
		value, err := e.fleet.UpdateTruck(ctx, args.ID, input)
		return ToolResult{Data: value, Action: true, Before: before}, err
	case "dispatcher":
		current := before.(dispatcherState)
		input := dispatcherInput(current)
		if err := applyDispatcherChanges(&input, args.Changes); err != nil {
			return ToolResult{}, err
		}
		if err := validateDispatcher(input); err != nil {
			return ToolResult{}, err
		}
		value, err := e.fleet.UpdateDispatcher(ctx, args.ID, input)
		return ToolResult{Data: value, Action: true, Before: before}, err
	}
	return ToolResult{}, errors.New("unsupported entity")
}

func validatedDesiredState(entity string, before any, changes map[string]any) (any, error) {
	switch entity {
	case "driver":
		input := driverInput(before.(repository.Driver))
		if err := applyDriverChanges(&input, changes); err != nil {
			return nil, err
		}
		if err := validateDriver(input); err != nil {
			return nil, err
		}
		return input, nil
	case "truck":
		input := truckInput(before.(repository.Truck))
		if err := applyTruckChanges(&input, changes); err != nil {
			return nil, err
		}
		if err := validateTruck(input); err != nil {
			return nil, err
		}
		return input, nil
	case "dispatcher":
		input := dispatcherInput(before.(dispatcherState))
		if err := applyDispatcherChanges(&input, changes); err != nil {
			return nil, err
		}
		if err := validateDispatcher(input); err != nil {
			return nil, err
		}
		return input, nil
	default:
		return nil, errors.New("unsupported entity")
	}
}

func (e *ToolExecutor) deleteFleet(ctx context.Context, identity repository.TelegramIdentity, raw json.RawMessage, confirmed bool) (ToolResult, error) {
	args, err := decodeFleetEnvelope(raw)
	if err != nil {
		return ToolResult{}, err
	}
	if args.ID == "" {
		return ToolResult{}, errors.New("id is required")
	}
	before, err := e.currentFleetState(ctx, args.Entity, args.ID)
	if err != nil {
		return ToolResult{}, err
	}
	if !confirmed {
		return e.pendingAction(ctx, identity, "delete_fleet_record", raw, before, fmt.Sprintf("Confirm permanently deleting this %s:\n%s", args.Entity, prettyJSON(before)))
	}
	switch args.Entity {
	case "driver":
		err = e.fleet.DeleteDriver(ctx, args.ID)
	case "truck":
		err = e.fleet.DeleteTruck(ctx, args.ID)
	case "dispatcher":
		err = e.fleet.DeleteDispatcher(ctx, args.ID)
	}
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Data: map[string]any{"deleted": true, "entity": args.Entity, "id": args.ID}, Action: true, Before: before}, nil
}

func (e *ToolExecutor) pendingAction(ctx context.Context, identity repository.TelegramIdentity, name string, args json.RawMessage, before any, preview string) (ToolResult, error) {
	state, err := json.Marshal(before)
	if err != nil {
		return ToolResult{}, err
	}
	digest := sha256.Sum256(state)
	request, err := e.repo.CreateActionRequest(ctx, identity, name, args, state, hex.EncodeToString(digest[:]), preview)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Data: map[string]any{"confirmationRequired": true, "preview": preview, "expiresAt": request.ExpiresAt}, Pending: &request}, nil
}

func (e *ToolExecutor) currentState(ctx context.Context, name string, raw json.RawMessage) ([]byte, error) {
	if name != "update_fleet_record" && name != "delete_fleet_record" {
		return nil, errors.New("action does not support confirmation")
	}
	args, err := decodeFleetEnvelope(raw)
	if err != nil {
		return nil, err
	}
	value, err := e.currentFleetState(ctx, args.Entity, args.ID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

type dispatcherState struct {
	repository.Dispatcher
	DriverIDs []string `json:"driverIds"`
}

func (e *ToolExecutor) currentFleetState(ctx context.Context, entity, id string) (any, error) {
	switch entity {
	case "driver":
		return e.fleet.GetDriver(ctx, id)
	case "truck":
		return e.fleet.GetTruck(ctx, id)
	case "dispatcher":
		value, err := e.fleet.GetDispatcher(ctx, id)
		if err != nil {
			return nil, err
		}
		drivers, err := e.fleet.ListDrivers(ctx)
		if err != nil {
			return nil, err
		}
		state := dispatcherState{Dispatcher: value, DriverIDs: make([]string, 0)}
		for _, driver := range drivers {
			if driver.DispatcherID != nil && *driver.DispatcherID == id {
				state.DriverIDs = append(state.DriverIDs, driver.ID)
			}
		}
		return state, nil
	}
	return nil, errors.New("unsupported entity")
}

func hasHighRiskChange(entity string, changes map[string]any) bool {
	for key := range changes {
		if key == "active" {
			return true
		}
		if entity == "driver" && key == "payRate" {
			return true
		}
		if entity == "dispatcher" && key == "payPercentage" {
			return true
		}
	}
	return false
}

func driverInput(v repository.Driver) repository.DriverInput {
	return repository.DriverInput{FullName: v.FullName, IsOwnerOperator: v.IsOwnerOperator, PayType: v.PayType, PayRate: v.PayRate, Phone: v.Phone, Email: v.Email, LicenseNumber: v.LicenseNumber, LicenseState: v.LicenseState, LicenseExpires: v.LicenseExpires, HireDate: v.HireDate, Address: v.Address, City: v.City, State: v.State, PostalCode: v.PostalCode, EmergencyContact: v.EmergencyContact, DispatcherID: v.DispatcherID, TruckID: v.TruckID, Active: v.Active, Notes: v.Notes, CDLFileID: v.CDLFileID}
}
func truckInput(v repository.Truck) repository.TruckInput {
	return repository.TruckInput{UnitNumber: v.UnitNumber, VIN: v.VIN, Year: v.Year, Make: v.Make, Model: v.Model, LicensePlate: v.LicensePlate, LicenseState: v.LicenseState, IsCompanyOwned: v.IsCompanyOwned, Status: v.Status, Mileage: v.Mileage, RegistrationExpires: v.RegistrationExpires, InsuranceExpires: v.InsuranceExpires, LastServiceDate: v.LastServiceDate, NextServiceMiles: v.NextServiceMiles, DriverID: v.DriverID, Active: v.Active, Notes: v.Notes, IRPFileID: v.IRPFileID}
}
func dispatcherInput(v dispatcherState) repository.DispatcherInput {
	return repository.DispatcherInput{FullName: v.FullName, Email: v.Email, Phone: v.Phone, PayPercentage: v.PayPercentage, DriverIDs: v.DriverIDs, Active: v.Active, Notes: v.Notes}
}

func applyDriverChanges(v *repository.DriverInput, c map[string]any) error {
	if err := validateChangeTypes(c,
		[]string{"fullName", "payType", "phone", "email", "licenseNumber", "licenseState", "licenseExpires", "hireDate", "address", "city", "state", "postalCode", "emergencyContact", "dispatcherId", "truckId", "notes", "cdlFileId"},
		[]string{"isOwnerOperator", "active"}, map[string]bool{"payRate": false}, nil); err != nil {
		return err
	}
	for key, value := range c {
		switch key {
		case "fullName":
			v.FullName = asString(value)
		case "isOwnerOperator":
			v.IsOwnerOperator = asBool(value)
		case "payType":
			v.PayType = asString(value)
		case "payRate":
			v.PayRate = asFloat(value)
		case "phone":
			v.Phone = asOptionalString(value)
		case "email":
			v.Email = asOptionalString(value)
		case "licenseNumber":
			v.LicenseNumber = asOptionalString(value)
		case "licenseState":
			v.LicenseState = asOptionalString(value)
		case "licenseExpires":
			date, err := asOptionalDate(value)
			if err != nil {
				return err
			}
			v.LicenseExpires = date
		case "hireDate":
			date, err := asOptionalDate(value)
			if err != nil {
				return err
			}
			v.HireDate = date
		case "address":
			v.Address = asOptionalString(value)
		case "city":
			v.City = asOptionalString(value)
		case "state":
			v.State = asOptionalString(value)
		case "postalCode":
			v.PostalCode = asOptionalString(value)
		case "emergencyContact":
			v.EmergencyContact = asOptionalString(value)
		case "dispatcherId":
			v.DispatcherID = asOptionalString(value)
		case "truckId":
			v.TruckID = asOptionalString(value)
		case "active":
			v.Active = asBool(value)
		case "notes":
			v.Notes = asOptionalString(value)
		case "cdlFileId":
			v.CDLFileID = asOptionalString(value)
		default:
			return fmt.Errorf("unsupported driver field %q", key)
		}
	}
	return nil
}
func applyTruckChanges(v *repository.TruckInput, c map[string]any) error {
	if err := validateChangeTypes(c,
		[]string{"unitNumber", "vin", "make", "model", "licensePlate", "licenseState", "status", "registrationExpires", "insuranceExpires", "lastServiceDate", "driverId", "notes", "irpFileId"},
		[]string{"isCompanyOwned", "active"}, map[string]bool{"year": true, "mileage": true, "nextServiceMiles": true}, nil); err != nil {
		return err
	}
	for key, value := range c {
		switch key {
		case "unitNumber":
			v.UnitNumber = asString(value)
		case "vin":
			v.VIN = asOptionalString(value)
		case "year":
			v.Year = asOptionalInt(value)
		case "make":
			v.Make = asOptionalString(value)
		case "model":
			v.Model = asOptionalString(value)
		case "licensePlate":
			v.LicensePlate = asOptionalString(value)
		case "licenseState":
			v.LicenseState = asOptionalString(value)
		case "isCompanyOwned":
			v.IsCompanyOwned = asBool(value)
		case "status":
			v.Status = asString(value)
		case "mileage":
			v.Mileage = asOptionalInt(value)
		case "registrationExpires":
			date, err := asOptionalDate(value)
			if err != nil {
				return err
			}
			v.RegistrationExpires = date
		case "insuranceExpires":
			date, err := asOptionalDate(value)
			if err != nil {
				return err
			}
			v.InsuranceExpires = date
		case "lastServiceDate":
			date, err := asOptionalDate(value)
			if err != nil {
				return err
			}
			v.LastServiceDate = date
		case "nextServiceMiles":
			v.NextServiceMiles = asOptionalInt(value)
		case "driverId":
			v.DriverID = asOptionalString(value)
		case "active":
			v.Active = asBool(value)
		case "notes":
			v.Notes = asOptionalString(value)
		case "irpFileId":
			v.IRPFileID = asOptionalString(value)
		default:
			return fmt.Errorf("unsupported truck field %q", key)
		}
	}
	return nil
}
func applyDispatcherChanges(v *repository.DispatcherInput, c map[string]any) error {
	if err := validateChangeTypes(c, []string{"fullName", "email", "phone", "notes"}, []string{"active"}, map[string]bool{"payPercentage": true}, []string{"driverIds"}); err != nil {
		return err
	}
	for key, value := range c {
		switch key {
		case "fullName":
			v.FullName = asString(value)
		case "email":
			v.Email = asOptionalString(value)
		case "phone":
			v.Phone = asOptionalString(value)
		case "payPercentage":
			if value == nil {
				v.PayPercentage = nil
			} else {
				x := asFloat(value)
				v.PayPercentage = &x
			}
		case "driverIds":
			encoded, _ := json.Marshal(value)
			if err := json.Unmarshal(encoded, &v.DriverIDs); err != nil {
				return errors.New("driverIds must be an array of UUID strings")
			}
		case "active":
			v.Active = asBool(value)
		case "notes":
			v.Notes = asOptionalString(value)
		default:
			return fmt.Errorf("unsupported dispatcher field %q", key)
		}
	}
	return nil
}

func validateDriver(v repository.DriverInput) error {
	if strings.TrimSpace(v.FullName) == "" {
		return errors.New("driver fullName is required")
	}
	if v.PayType != "cpm" && v.PayType != "gross_percentage" {
		return errors.New("payType must be cpm or gross_percentage")
	}
	if v.PayRate < 0 || (v.PayType == "gross_percentage" && v.PayRate > 100) {
		return errors.New("invalid driver payRate")
	}
	return nil
}
func validateTruck(v repository.TruckInput) error {
	if strings.TrimSpace(v.UnitNumber) == "" {
		return errors.New("truck unitNumber is required")
	}
	valid := map[string]bool{"available": true, "assigned": true, "maintenance": true, "out_of_service": true}
	if !valid[v.Status] {
		return errors.New("invalid truck status")
	}
	if v.Year != nil && (*v.Year < 1900 || *v.Year > 2200) {
		return errors.New("invalid truck year")
	}
	if v.Mileage != nil && *v.Mileage < 0 {
		return errors.New("mileage cannot be negative")
	}
	return nil
}
func validateDispatcher(v repository.DispatcherInput) error {
	if strings.TrimSpace(v.FullName) == "" {
		return errors.New("dispatcher fullName is required")
	}
	if v.PayPercentage != nil && (*v.PayPercentage < 0 || *v.PayPercentage > 100) {
		return errors.New("payPercentage must be between 0 and 100")
	}
	return nil
}

func asString(value any) string { result, _ := value.(string); return strings.TrimSpace(result) }
func asBool(value any) bool     { result, _ := value.(bool); return result }
func asFloat(value any) float64 { result, _ := value.(float64); return result }
func asOptionalString(value any) *string {
	if value == nil {
		return nil
	}
	result := asString(value)
	if result == "" {
		return nil
	}
	return &result
}
func asOptionalInt(value any) *int {
	if value == nil {
		return nil
	}
	result := int(asFloat(value))
	return &result
}
func asOptionalDate(value any) (*time.Time, error) {
	if value == nil || asString(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.DateOnly, asString(value))
	if err != nil {
		return nil, errors.New("dates must use YYYY-MM-DD")
	}
	return &parsed, nil
}
func validateChangeTypes(values map[string]any, stringFields, boolFields []string, numberFields map[string]bool, arrayFields []string) error {
	stringsAllowed := make(map[string]bool, len(stringFields))
	for _, key := range stringFields {
		stringsAllowed[key] = true
	}
	boolsAllowed := make(map[string]bool, len(boolFields))
	for _, key := range boolFields {
		boolsAllowed[key] = true
	}
	arraysAllowed := make(map[string]bool, len(arrayFields))
	for _, key := range arrayFields {
		arraysAllowed[key] = true
	}
	for key, value := range values {
		switch {
		case stringsAllowed[key]:
			if value != nil {
				if _, ok := value.(string); !ok {
					return fmt.Errorf("%s must be a string or null", key)
				}
			}
		case boolsAllowed[key]:
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be true or false", key)
			}
		case numberFields[key] || hasKey(numberFields, key):
			if value == nil {
				if !numberFields[key] {
					return fmt.Errorf("%s must be a number", key)
				}
			} else if _, ok := value.(float64); !ok {
				return fmt.Errorf("%s must be a number or null", key)
			}
		case arraysAllowed[key]:
			raw, ok := value.([]any)
			if !ok {
				return fmt.Errorf("%s must be an array", key)
			}
			for _, item := range raw {
				if _, ok := item.(string); !ok {
					return fmt.Errorf("%s must contain only strings", key)
				}
			}
		default:
			return fmt.Errorf("unsupported field %q", key)
		}
	}
	return nil
}
func hasKey(values map[string]bool, key string) bool { _, ok := values[key]; return ok }
func prettyJSON(value any) string {
	encoded, _ := json.MarshalIndent(value, "", "  ")
	return string(encoded)
}
