package httpapi

import "testing"

func TestDriverSetupTruckValidation(t *testing.T) {
	good := func() driverRequest {
		return driverRequest{FullName: "New Driver", PayType: "cpm", PayRate: .65, Active: true, NewTruck: &truckRequest{UnitNumber: "001", Status: "available", Active: true}}
	}
	input, err := good().validate()
	if err != nil || input.NewTruck == nil || input.NewTruck.UnitNumber != "001" {
		t.Fatal("nested truck rejected", err)
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
