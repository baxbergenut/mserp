package repository

import (
	"errors"
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
		ID:     43,
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

func TestLoadToRecordUsesStableDisplayFallbackAndLaterNumber(t *testing.T) {
	empty, whitespace := "", "  \t "
	for _, value := range []*string{nil, &empty, &whitespace} {
		load := datatruck.Load{ID: 123, LoadID: value, Status: "invoiced"}
		payload := []byte(`{"id":123,"load_id":null}`)
		record, err := LoadToRecord(load, payload, time.Now())
		if err != nil || record.ID != 123 || record.LoadID != "DataTruck #123" || string(record.RawPayload) != string(payload) {
			t.Fatalf("record = %+v, error = %v", record, err)
		}
		actual := " ACTUAL-123 "
		load.LoadID = &actual
		updated, err := LoadToRecord(load, nil, time.Now())
		if err != nil || updated.ID != record.ID || updated.LoadID != "ACTUAL-123" {
			t.Fatalf("updated = %+v, error = %v", updated, err)
		}
	}
}

func TestLoadToRecordRequiresStableUpstreamID(t *testing.T) {
	label := "DISPLAY-NUMBER"
	for _, id := range []int{0, -1} {
		_, err := LoadToRecord(datatruck.Load{ID: id, LoadID: &label}, nil, time.Now())
		if !errors.Is(err, ErrMissingLoadRecordID) {
			t.Fatalf("record %d error = %v", id, err)
		}
	}
}

func assertStringPtr(t *testing.T, field string, actual *string, expected string) {
	t.Helper()
	if actual == nil || *actual != expected {
		t.Errorf("%s = %v, want %q", field, actual, expected)
	}
}
