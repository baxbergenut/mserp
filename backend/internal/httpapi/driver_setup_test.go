package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDriverSetupSelfOwnershipPermissions(t *testing.T) {
	request := driverRequest{NewTruck: &truckRequest{}, NewTruckOwnedByDriver: true}
	grants := []string{"drivers.create", "drivers.private.edit", "driver_compensation.edit", "escrow.create", "trucks.create"}
	for _, path := range []string{"/drivers", "/driver-intake/id/complete"} {
		t.Run(path, func(t *testing.T) {
			if !authorizeDriverSetup(httptest.NewRecorder(), capabilityRequest("POST", path, "", grants...), request) {
				t.Fatal("combined setup must allow the new driver to own the new truck without investors.create")
			}
			for index, missing := range grants {
				t.Run("missing "+missing, func(t *testing.T) {
					remaining := append([]string{}, grants[:index]...)
					remaining = append(remaining, grants[index+1:]...)
					w := httptest.NewRecorder()
					if authorizeDriverSetup(w, capabilityRequest("POST", path, "", remaining...), request) || w.Code != http.StatusForbidden {
						t.Fatal("self ownership must retain the setup permission: " + missing)
					}
				})
			}
		})
	}
}

func TestDriverSetupTruckValidation(t *testing.T) {
	good := func() driverRequest {
		return driverRequest{FullName: "New Driver", PayType: "cpm", PayRate: .65, Active: true, NewTruck: &truckRequest{UnitNumber: "001", Status: "available", Active: true}}
	}
	input, err := good().validate()
	if err != nil || input.NewTruck == nil || input.NewTruck.UnitNumber != "001" {
		t.Fatal("nested truck rejected", err)
	}
	selfOwned := good()
	selfOwned.NewTruckOwnedByDriver = true
	selfOwned.NewTruck.OwnerID = stringPtr("00000000-0000-0000-0000-000000000001")
	selfOwned.NewTruck.IsCompanyOwned = true
	input, err = selfOwned.validate()
	if err != nil || !input.NewTruckOwnedByDriver || input.NewTruck == nil || input.NewTruck.OwnerID != nil {
		t.Fatal("driver ownership must override the retained owner dropdown choice", err)
	}
	for _, mutate := range []func(*driverRequest){
		func(r *driverRequest) { r.NewTruck.UnitNumber = "" },
		func(r *driverRequest) { n := 1800; r.NewTruck.Year = &n },
		func(r *driverRequest) { n := -1; r.NewTruck.Mileage = &n },
		func(r *driverRequest) { r.NewTruck.OwnerID = stringPtr("invalid") },
		func(r *driverRequest) { r.TruckID = stringPtr("08000000-0000-0000-0000-000000000001") },
		func(r *driverRequest) { r.NewTruck.DriverID = stringPtr("08000000-0000-0000-0000-000000000001") },
		func(r *driverRequest) { r.Status = "terminated" },
		func(r *driverRequest) { team := true; r.IsTeamDriver = &team },
		func(r *driverRequest) { r.NewTruck = nil; r.NewTruckOwnedByDriver = true },
	} {
		request := good()
		mutate(&request)
		if _, err := request.validate(); err == nil {
			t.Fatalf("invalid setup accepted: %+v", request)
		}
	}
}

func stringPtr(value string) *string { return &value }
