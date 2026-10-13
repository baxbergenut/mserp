package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// An owner driving their own truck uses their existing driver tariff. Truck
// recurring fees do not require a second investor agreement or an invented rate.
// Trucks with explicit terms retain the established owner/mixed-week routing.
func routeOwnerTruckFees(ctx context.Context, tx pgx.Tx, source *DriverPayWeek, config TruckChargeData) error {
	terms := applicableTruckTerms(config, source.WeekStart)
	needed := false
	for _, phase := range config.Phases {
		if _, configured := terms[phase.TruckID]; !configured && phase.Included && phase.WeekStart <= source.WeekStart {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	charges, _, err := payrollChargeData(ctx, tx, "")
	if err != nil {
		return err
	}
	loadMap, err := truckPayLoadMap(ctx, tx, source.WeekStart)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.unit_number,d.id::text,d.full_name,d.pay_type,d.pay_rate::text,
 coalesce(dp.id::text,''),coalesce(dp.full_name,'Unassigned'),
 EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=t.id AND a.driver_id=d.id
 AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=$1::date
 AND (a.unassigned_at IS NULL OR (a.unassigned_at AT TIME ZONE 'America/New_York')::date>=$1::date+7))
 FROM trucks t
 JOIN LATERAL (SELECT h.owner_id FROM truck_ownership_history h WHERE h.truck_id=t.id
 AND (NOT h.start_known OR (h.assigned_at AT TIME ZONE 'America/New_York')::date<$1::date+7)
 AND (h.unassigned_at IS NULL OR (h.unassigned_at AT TIME ZONE 'America/New_York')::date>=$1::date+7)
 ORDER BY h.assigned_at DESC LIMIT 1) ownership ON true
 JOIN investors i ON i.id=ownership.owner_id AND NOT i.is_company
 JOIN drivers d ON d.id=i.driver_id LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id
 WHERE NOT EXISTS(SELECT 1 FROM truck_settlement_terms s WHERE s.truck_id=t.id AND s.week_start<=$1::date)
 AND NOT EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=t.id AND w.week_start=$1::date)
 AND NOT EXISTS(SELECT 1 FROM truck_ownership_history h WHERE h.truck_id=t.id AND h.owner_id<>i.id
 AND (h.assigned_at AT TIME ZONE 'America/New_York')::date<$1::date+7
 AND (h.unassigned_at IS NULL OR (h.unassigned_at AT TIME ZONE 'America/New_York')::date>$1::date))`, source.WeekStart)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var d DriverPayDriver
		var fullWeek bool
		if err = rows.Scan(&d.TruckID, &d.TruckUnit, &d.ID, &d.FullName, &d.PayType, &d.PayRate, &d.DispatcherID, &d.DispatcherName, &fullWeek); err != nil {
			return err
		}
		if !payrollIncludesTruck(ctx, d.TruckID) {
			continue
		}
		hasLoads, otherDriver := false, false
		for _, person := range source.Drivers {
			for _, load := range person.Loads {
				if loadMap[person.ID+":"+load.CommentKey].TruckID == d.TruckID {
					hasLoads = true
					otherDriver = otherDriver || person.ID != d.ID
				}
			}
		}
		if otherDriver || (!hasLoads && !fullWeek) {
			continue
		}
		fees := truckRecurringCharges(config, charges.Types, d.TruckID, source.WeekStart, hasLoads)
		if len(fees) == 0 {
			continue
		}
		found := false
		for index := range source.Drivers {
			if source.Drivers[index].ID == d.ID {
				source.Drivers[index].AutoCharges = append(source.Drivers[index].AutoCharges, fees...)
				found = true
				break
			}
		}
		if !found {
			d.IsOwnerOperator = true
			d.EmploymentType = "owner_operator"
			d.FuelTotal, d.TollTotal = "0.00", "0.00"
			d.Loads = []DriverPayLoad{}
			d.AutoCharges = fees
			d.Edits = DriverPayEdits{DriverID: d.ID, WeekStart: source.WeekStart, Comments: map[string]string{}, Adjustments: []DriverPayAdjustment{}}
			source.Drivers = append(source.Drivers, d)
		}
	}
	return rows.Err()
}
