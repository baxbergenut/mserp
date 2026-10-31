package repository

import (
	"context"
	"testing"
)

func TestPayrollSummarySourceWarnings(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"trip":{"mile":400,"empty_mile":0},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Dallas","state":"TX"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Houston","state":"TX"}}]}`,
		`{"trip":{"mile":"bad","empty_mile":0},"stops":[]}`,
		`{"trip":{"mile":-1,"empty_mile":0},"stops":[]}`,
		`{"trip":{"mile":0,"empty_mile":0,"trip_begin_date":"2026-09-28","pickup":{"city":""},"delivery":{"state":""}},"stops":[{"stop_type":"pickup","location":{"address":"  a  "}},{"stop_type":"delivery","location":{"zip":"123"}}]}`,
	} {
		for _, split := range []bool{false, true} {
			for _, review := range []bool{false, true} {
				full := DriverPayLoad{PickupDate: "2026-09-28", TotalMiles: "400", TripReview: review}
				id := 1
				if split {
					full.LoadTripID = &id
				}
				summary := full
				want, we := hydratePayrollLoad(context.Background(), nil, &full, []byte(raw))
				got, ge := hydratePayrollLoad(context.WithValue(context.Background(), payrollSummaryKey{}, true), nil, &summary, []byte(raw))
				if (we != nil) != (ge != nil) || want != got {
					t.Fatalf("summary warning differs: split=%v review=%v raw=%s", split, review, raw)
				}
				if summary.PickupLocation != "" || summary.DeliveryLocation != "" || summary.LoadedMiles != "" || summary.DeadheadMiles != "" {
					t.Fatal("summary hydrated display fields")
				}
			}
		}
	}
}
