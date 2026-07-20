package repository

import (
	"testing"
	"time"

	"mserp/internal/datatruck"
)

func TestLoadToRecordCanonicalizesFleetValues(t *testing.T) {
	loadID := " LOAD-1 "
	dispatcher := "  ALEX   SMITH "
	driver := "JANE DOE"
	teamDriver := "JOHN O'NEIL"
	truck := " ab  123 "
	load := datatruck.Load{
		ID:                 42,
		LoadID:             &loadID,
		DispatcherFullName: &dispatcher,
		Trip: &datatruck.Trip{
			DriverFullName:     &driver,
			TeamDriverFullName: &teamDriver,
			TruckUnitNumber:    &truck,
		},
	}

	record, err := LoadToRecord(load, []byte(`{"id":42}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if record.LoadID != "LOAD-1" {
		t.Errorf("LoadID = %q", record.LoadID)
	}
	assertStringPtr(t, "DispatcherName", record.DispatcherName, "Alex Smith")
	assertStringPtr(t, "DriverName", record.DriverName, "Jane Doe")
	assertStringPtr(t, "TeamDriverName", record.TeamDriverName, "John O'Neil")
	assertStringPtr(t, "TruckUnit", record.TruckUnit, "AB 123")
}

func TestLoadToRecordUsesAssignedDriverFallback(t *testing.T) {
	loadID := "LOAD-2"
	driver := "SAM DRIVER"
	truck := "t-9"
	load := datatruck.Load{
		LoadID: &loadID,
		AssignedDriverNTruck: &datatruck.AssignedDriverNTruck{
			DriverFullName:  &driver,
			TruckUnitNumber: &truck,
		},
	}

	record, err := LoadToRecord(load, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	assertStringPtr(t, "DriverName", record.DriverName, "Sam Driver")
	assertStringPtr(t, "TruckUnit", record.TruckUnit, "T-9")
}

func assertStringPtr(t *testing.T, field string, actual *string, expected string) {
	t.Helper()
	if actual == nil || *actual != expected {
		t.Errorf("%s = %v, want %q", field, actual, expected)
	}
}
