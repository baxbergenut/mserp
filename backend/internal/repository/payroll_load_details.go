package repository

import (
	"context"
	"encoding/json"
	"math/big"
	"mserp/internal/datatruck"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Evidence is parsed once per distinct load/trip in this uninterrupted read
// phase. Summaries retain completeness warnings without formatting addresses or
// display mileages. No memo survives a financial write.
func hydratePayrollLoad(ctx context.Context, tx pgx.Tx, row *DriverPayLoad, raw []byte) (bool, error) {
	if len(raw) == 0 {
		return true, nil
	}
	key := ""
	if row.LoadRecordID != nil {
		key = loadAllocationKey(*row.LoadRecordID, row.LoadTripID)
	}
	memo := payrollReadMemo(ctx, tx)
	var load datatruck.Load
	var found bool
	if memo != nil && key != "" {
		load, found = memo.loads[key]
	}
	if !found {
		if err := json.Unmarshal(raw, &load); err != nil {
			return false, err
		}
		if memo != nil && key != "" {
			if memo.loads == nil {
				memo.loads = map[string]datatruck.Load{}
			}
			memo.loads[key] = load
		}
	}
	if payrollSummaryOnly(ctx) {
		pickup, delivery := false, false
		for _, stop := range load.Stops {
			present := false
			for _, v := range []string{stop.Location.City, stop.Location.State, stop.Location.ZipCode, stop.Location.Address} {
				present = present || strings.TrimSpace(v) != ""
			}
			if stop.StopType == "pickup" {
				pickup = pickup || present
			}
			if stop.StopType == "delivery" {
				delivery = delivery || present
			}
		}
		miles := false
		if load.Trip != nil && !row.TripReview {
			if row.LoadTripID != nil {
				row.PickupDate = load.Trip.BeginDate
				// The full projection joins city/state with a comma, even if both
				// source labels are empty. Preserve that existing warning behavior.
				pickup = pickup || load.Trip.Pickup != nil
				delivery = delivery || load.Trip.Delivery != nil
			}
			valid := func(v *datatruck.FlexibleString) bool {
				if v == nil {
					return false
				}
				n, ok := new(big.Rat).SetString(v.String())
				return ok && n.Sign() >= 0
			}
			miles = valid(load.Trip.LoadedMiles) && valid(load.Trip.DeadheadMiles)
		}
		return row.PickupDate == "" || !pickup || !delivery || row.TotalMiles == "" || !miles, nil
	}
	row.PickupLocation, row.DeliveryLocation = payLoadLocations(load.Stops)
	if load.Trip != nil && !row.TripReview {
		if row.LoadTripID != nil {
			row.PickupDate = load.Trip.BeginDate
			if load.Trip.Pickup != nil {
				row.PickupLocation = strings.TrimSpace(load.Trip.Pickup.City + ", " + load.Trip.Pickup.State)
			}
			if load.Trip.Delivery != nil {
				row.DeliveryLocation = strings.TrimSpace(load.Trip.Delivery.City + ", " + load.Trip.Delivery.State)
			}
		}
		row.LoadedMiles = paySourceMiles(load.Trip.LoadedMiles)
		row.DeadheadMiles = paySourceMiles(load.Trip.DeadheadMiles)
	}
	return row.PickupDate == "" || row.PickupLocation == "" || row.DeliveryLocation == "" || row.TotalMiles == "" || row.LoadedMiles == "" || row.DeadheadMiles == "", nil
}
