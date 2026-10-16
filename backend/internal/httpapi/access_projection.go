package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"mserp/internal/repository"
)

// Project structured responses before encoding. Downloads and SSE keep their
// native response behaviour and have their own record-level authorization.
type permissionResponseWriter struct {
	http.ResponseWriter
	request *http.Request
}

func (w *permissionResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *permissionResponseWriter) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *permissionResponseWriter) ProjectJSON(value any) any {
	s, _ := authSessionFromContext(w.request.Context())
	path := w.request.URL.Path
	// These endpoints deliberately return their own authorized financial data.
	if strings.HasPrefix(path, "/driver-pay") || strings.HasPrefix(path, "/investor-pay") || path == "/dispatcher-pay" || path == "/financial-dashboard" || strings.HasSuffix(path, "/pay-history") || strings.HasSuffix(path, "/settlement-history") {
		return value
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var result any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil {
		return nil
	}
	projectFields(result, s.User, path)
	return result
}

var driverPrivateFields = []string{"address", "city", "state", "postalCode", "emergencyContact", "licenseNumber", "licenseState", "licenseExpires"}
var driverDocumentFields = []string{"cdlFileId", "cdlFileName", "cdlFileContentType", "cdlFileSizeBytes"}
var truckDocumentFields = []string{"irpFileId", "irpFileName", "irpFileContentType", "irpFileSizeBytes"}

func projectFields(value any, user repository.AuthUser, path string) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			projectFields(item, user, path)
		}
	case map[string]any:
		_, driver := v["payType"]
		_, cdl := v["cdlFileId"]
		_, truck := v["unitNumber"]
		_, dispatcher := v["payPercentage"]
		remove := func(fields []string) {
			for _, key := range fields {
				delete(v, key)
			}
		}
		if driver || cdl {
			if !repository.HasPermission(user, "drivers.private.read") {
				remove(driverPrivateFields)
			}
			if !repository.HasPermission(user, "drivers.documents.read") {
				remove(driverDocumentFields)
			}
			if !repository.HasPermission(user, "driver_compensation.read") {
				remove([]string{"payType", "payRate"})
			}
		}
		if truck && !repository.HasPermission(user, "trucks.documents.read") {
			remove(truckDocumentFields)
		}
		if dispatcher && !repository.HasPermission(user, "dispatcher_compensation.read") {
			delete(v, "payPercentage")
		}
		if strings.HasPrefix(path, "/truck-charges") && !repository.HasPermission(user, "investor_terms.read") {
			delete(v, "terms")
		}
		for _, item := range v {
			projectFields(item, user, path)
		}
	}
}

// Merge omitted protected fields from the saved record. Presence, including
// explicit null/zero, is an attempted write and needs the matching capability.
func decodeProtectedProfile(w http.ResponseWriter, r *http.Request, destination any, existing any, area string) bool {
	var incoming map[string]json.RawMessage
	if err := decodeJSON(r, &incoming); err != nil {
		writeAPIError(w, 400, err.Error())
		return false
	}
	s, _ := authSessionFromContext(r.Context())
	rules := map[string]string{}
	add := func(fields []string, permission string) {
		for _, key := range fields {
			rules[key] = permission
		}
	}
	switch area {
	case "drivers":
		add(driverPrivateFields, "drivers.private.edit")
		add([]string{"cdlFileId"}, "drivers.documents.edit")
		add([]string{"payType", "payRate"}, "driver_compensation.edit")
		if existing == nil {
			add([]string{"escrowAmount"}, "escrow.create")
		} else {
			add([]string{"escrowAmount"}, "escrow.edit")
		}
	case "trucks":
		add([]string{"irpFileId"}, "trucks.documents.edit")
	case "dispatchers":
		add([]string{"payPercentage"}, "dispatcher_compensation.edit")
	}
	// encoding/json accepts case-insensitive field aliases. Apply authorization
	// before decoding those aliases, and require canonical names for valid writes.
	for key := range incoming {
		for canonical, permission := range rules {
			if !strings.EqualFold(key, canonical) {
				continue
			}
			if !repository.HasPermission(s.User, permission) {
				permissionDenied(w)
				return false
			}
			if key != canonical {
				writeAPIError(w, 400, "Use the canonical field name: "+canonical)
				return false
			}
		}
	}
	preserved := []string{}
	var saved map[string]json.RawMessage
	if existing != nil {
		raw, _ := json.Marshal(existing)
		_ = json.Unmarshal(raw, &saved)
	}
	for key, permission := range rules {
		if _, present := incoming[key]; present && !repository.HasPermission(s.User, permission) {
			permissionDenied(w)
			return false
		}
		if _, present := incoming[key]; !present && saved != nil {
			preserved = append(preserved, key)
			if raw, ok := saved[key]; ok {
				// Profile input dates use YYYY-MM-DD; read models contain RFC3339.
				if key == "licenseExpires" {
					var date string
					_ = json.Unmarshal(raw, &date)
					if len(date) > 10 {
						date = date[:10]
					}
					raw, _ = json.Marshal(date)
				}
				if string(raw) == "null" && key != "cdlFileId" && key != "irpFileId" && key != "payPercentage" {
					raw = []byte(`""`)
				}
				incoming[key] = raw
			}
		}
	}
	raw, err := json.Marshal(incoming)
	if err != nil {
		writeAPIError(w, 400, "invalid profile")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		writeAPIError(w, 400, fmt.Sprintf("invalid profile: %v", err))
		return false
	}
	switch d := destination.(type) {
	case *driverRequest:
		d.preserveFields = preserved
	case *truckRequest:
		d.preserveFields = preserved
	case *dispatcherRequest:
		d.preserveFields = preserved
	}
	return true
}

func authorizeDriverSetup(w http.ResponseWriter, r *http.Request, request driverRequest) bool {
	keys := []string{"drivers.create", "drivers.private.edit"}
	if request.IsTeamDriver != nil && *request.IsTeamDriver {
		keys = append(keys, "driver_pay.edit")
	} else {
		keys = append(keys, "driver_compensation.edit", "escrow.create")
	}
	if request.NewTruck != nil {
		keys = append(keys, "trucks.create")
		if request.NewTruck.IRPFileID != nil {
			keys = append(keys, "trucks.documents.edit")
		}
	}
	if request.NewTruckOwnedByDriver {
		keys = append(keys, "investors.create")
	}
	if request.CDLFileID != nil {
		keys = append(keys, "drivers.documents.edit")
	}
	return requireCapabilities(w, r, keys...)
}

func authorizeVendorAction(w http.ResponseWriter, r *http.Request, area, action string) bool {
	key := "edit"
	switch action {
	case "add":
		key = "create"
	case "remove":
		key = "delete"
	}
	return requireCapabilities(w, r, area+"."+key)
}

func (h fleetHandler) authorizeAttachment(w http.ResponseWriter, r *http.Request, id *string, kind string) bool {
	if id == nil {
		return true
	}
	s, _ := authSessionFromContext(r.Context())
	ok, err := h.repo.CanAttachFile(r.Context(), *id, s.User, kind)
	if err != nil {
		writeAPIError(w, 500, "Could not verify attachment access")
		return false
	}
	if !ok {
		permissionDenied(w)
		return false
	}
	return true
}

// Selecting a previously unregistered driver-owner also creates an investor.
// Check that extra effect before opening the fleet transaction.
func (h fleetHandler) authorizeTruckEffects(w http.ResponseWriter, r *http.Request, request truckRequest) bool {
	if request.OwnerDriverID == nil {
		return true
	}
	owners, err := h.repo.ListInvestors(r.Context(), "")
	if err != nil {
		h.writeError(w, err)
		return false
	}
	for _, owner := range owners {
		if owner.DriverID != nil && *owner.DriverID == *request.OwnerDriverID {
			if !owner.Active {
				return requireCapabilities(w, r, "investors.edit")
			}
			return true
		}
	}
	return requireCapabilities(w, r, "investors.create")
}
