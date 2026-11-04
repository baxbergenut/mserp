package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Without investor terms, dated ownership supplies the recurring fee destination,
// independently of load attribution. Conflicting loads/assignments require review
// instead of hiding the fees. Explicit terms retain owner/mixed-week routing.
func routeOwnerTruckFees(ctx context.Context, tx pgx.Tx, source *DriverPayWeek, config TruckChargeData, terms map[string]TruckTerm) error {
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
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.unit_number,d.id::text,d.full_name,d.pay_type,d.pay_rate::text,d.active,
 coalesce(dp.id::text,''),coalesce(dp.full_name,'Unassigned'),
 EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=t.id AND a.driver_id=d.id
 AND (a.assigned_at AT TIME ZONE 'America/New_York')::date<=$1::date
 AND (a.unassigned_at IS NULL OR (a.unassigned_at AT TIME ZONE 'America/New_York')::date>=$1::date+7))
 FROM trucks t `+truckOwnershipForWeekSQL+`
 JOIN drivers d ON d.id=i.driver_id LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id
 WHERE NOT i.is_company
 AND NOT EXISTS(SELECT 1 FROM investor_pay_weeks w WHERE w.truck_id=t.id AND w.week_start=$1::date AND w.owner_id=i.id)`, source.WeekStart)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var d DriverPayDriver
		var active, fullWeek bool
		if err = rows.Scan(&d.TruckID, &d.TruckUnit, &d.ID, &d.FullName, &d.PayType, &d.PayRate, &active, &d.DispatcherID, &d.DispatcherName, &fullWeek); err != nil {
			return err
		}
		if _, configured := terms[d.TruckID]; configured || !payrollIncludesTruck(ctx, d.TruckID) {
			continue
		}
		hasLoads := false
		recordedLoads := []string{}
		conflicts := []string{}
		for _, person := range source.Drivers {
			for _, load := range person.Loads {
				if loadMap[person.ID+":"+load.CommentKey].TruckID == d.TruckID {
					hasLoads = true
					recordedLoads = append(recordedLoads, fmt.Sprintf("%s (%s)", load.LoadNumber, load.Date))
					if person.ID != d.ID {
						conflicts = append(conflicts, fmt.Sprintf("%s (%s, %s)", person.FullName, load.LoadNumber, load.Date))
					}
				}
			}
		}
		fees := truckRecurringCharges(config, charges.Types, d.TruckID, source.WeekStart, hasLoads)
		if len(fees) == 0 {
			continue
		}
		// Ownership supplies the fee destination. Load attribution is reviewed
		// separately and must never silently remove the owner's recurring fees.
		if len(conflicts) > 0 {
			sort.Strings(conflicts)
			d.Issues = append(d.Issues, fmt.Sprintf("Truck %s has loads entered for another driver: %s. Recurring fees remain on the recorded owner's statement; review Gross Board entries and truck settlement terms.", d.TruckUnit, strings.Join(conflicts, "; ")))
		}
		// Idle owned trucks do not require an operating assignment. Recorded
		// work without that assignment still needs source/assignment review.
		if hasLoads && !fullWeek {
			sort.Strings(recordedLoads)
			d.Issues = append(d.Issues, fmt.Sprintf("Truck %s has recorded loads without a full-week owner-driver assignment: %s. Recurring fees remain on the recorded owner's statement; review the source truck, dated assignments and settlement terms.", d.TruckUnit, strings.Join(recordedLoads, "; ")))
		}
		found := false
		for index := range source.Drivers {
			if source.Drivers[index].ID == d.ID {
				source.Drivers[index].AutoCharges = append(source.Drivers[index].AutoCharges, fees...)
				source.Drivers[index].Issues = append(source.Drivers[index].Issues, d.Issues...)
				found = true
				break
			}
		}
		if !found {
			// An open historical assignment and truck fees cannot re-enroll a
			// terminated driver. Existing weekly work/saved statements stay above.
			if !active {
				continue
			}
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
