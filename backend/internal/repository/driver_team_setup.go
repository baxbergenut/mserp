package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type DriverTeamTruck struct {
	TruckID        string  `json:"truckId"`
	UnitNumber     string  `json:"unitNumber"`
	MainDriverID   string  `json:"mainDriverId"`
	MainDriverName string  `json:"mainDriverName"`
	MainVersion    int     `json:"mainVersion"`
	DispatcherID   *string `json:"dispatcherId"`
	DispatcherName *string `json:"dispatcherName"`
	TeamDriverID   *string `json:"teamDriverId"`
}

// Only an unambiguous operating driver for the selected week can define a team.
// Zero-length corrected assignments are history, not matching evidence.
const driverTeamTrucksSQL = `SELECT vehicle.id::text,vehicle.unit_number,d.id::text,d.full_name,d.team_version,
 h.dispatcher_id::text,dp.full_name,team.team_driver_id::text
 FROM trucks vehicle JOIN LATERAL (
 SELECT (array_agg(DISTINCT driver_id))[1] main_driver_id FROM truck_driver_assignments a
 WHERE a.truck_id=vehicle.id AND a.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York')
 AND (a.unassigned_at IS NULL OR a.unassigned_at > ($1::date::timestamp AT TIME ZONE 'America/New_York'))
 AND (a.unassigned_at IS NULL OR a.assigned_at<a.unassigned_at)
 HAVING count(DISTINCT driver_id)=1 AND bool_or(a.unassigned_at IS NULL OR a.unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 ) assigned ON true JOIN drivers d ON d.id=assigned.main_driver_id
 LEFT JOIN LATERAL (
 SELECT dispatcher_id FROM driver_dispatcher_assignments h WHERE h.driver_id=d.id
 AND (NOT h.start_known OR h.assigned_at < (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 AND (h.unassigned_at IS NULL OR h.unassigned_at >= (($1::date+7)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY h.assigned_at DESC LIMIT 1
 ) h ON true LEFT JOIN dispatchers dp ON dp.id=h.dispatcher_id
 LEFT JOIN driver_teams team ON team.main_driver_id=d.id AND team.start_week<=$1::date AND (team.end_week IS NULL OR $1::date<team.end_week)
 WHERE vehicle.active AND d.active AND payroll_driver_at(d.id,$1::date)=d.id`

func readDriverTeamTrucks(ctx context.Context, q chargeQuery, week string) ([]DriverTeamTruck, error) {
	rows, err := q.Query(ctx, driverTeamTrucksSQL+` ORDER BY vehicle.unit_number,vehicle.id`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DriverTeamTruck{}
	for rows.Next() {
		var value DriverTeamTruck
		if err = rows.Scan(&value.TruckID, &value.UnitNumber, &value.MainDriverID, &value.MainDriverName, &value.MainVersion, &value.DispatcherID, &value.DispatcherName, &value.TeamDriverID); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
func (r *FleetRepository) DriverTeamTrucks(ctx context.Context, week string) ([]DriverTeamTruck, error) {
	if _, err := chargeWeek(week); err != nil {
		return nil, err
	}
	return readDriverTeamTrucks(ctx, r.pool, week)
}

func lockDriverTeamSetup(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736281940)`)
	return err
}

// Create and edit use this same transaction as profile/assignment persistence.
// Selecting a team truck never transfers its primary operating assignment.
func applyDriverTeamSetup(ctx context.Context, tx pgx.Tx, driver string, input *DriverInput) error {
	if input.IsTeamDriver == nil {
		return nil
	}
	week := input.AssignmentWeek
	if week == "" {
		week = ChargeCurrentWeek()
	}
	old := DriverTeam{Periods: []DriverTeamPeriod{}}
	if driver != "" {
		var err error
		old, err = readDriverTeams(ctx, tx, driver)
		if err != nil {
			return err
		}
		if input.TeamVersion != nil && *input.TeamVersion != old.Version {
			return ErrDriverTeamConflict
		}
	}
	current := teamPeriodFor(old.Periods, driver, week)
	if current != nil && current.MainDriverID == driver && *input.IsTeamDriver {
		return chargeInvalid("Remove this driver's teammate before making them a team driver")
	}
	if !*input.IsTeamDriver {
		if current == nil || current.TeamDriverID != driver {
			return nil
		}
		if input.AssignmentWeek == "" {
			return chargeInvalid("Select the Monday when this driver should leave the team")
		}
		previous, err := readDriverTeams(ctx, tx, current.MainDriverID)
		if err != nil {
			return err
		}
		_, err = setDriverTeamTx(ctx, tx, current.MainDriverID, DriverTeamInput{WeekStart: week, Version: previous.Version}, input.ChargeActor)
		return err
	}
	if !input.Active {
		if current != nil && current.TeamDriverID == driver {
			input.TruckID = nil
			input.DispatcherID = nil
			return nil
		}
		return chargeInvalid("A new team driver must have an active status")
	}
	if input.TruckID == nil {
		return chargeInvalid("Select the main driver's truck")
	}
	choices, err := readDriverTeamTrucks(ctx, tx, week)
	if err != nil {
		return err
	}
	var selected *DriverTeamTruck
	for i := range choices {
		if choices[i].TruckID == *input.TruckID {
			selected = &choices[i]
			break
		}
	}
	if selected == nil {
		return chargeInvalid("This truck needs one active main driver for the selected week; reload and review the truck")
	}
	if input.TeamMainDriverID != nil && *input.TeamMainDriverID != selected.MainDriverID {
		return ErrDriverTeamConflict
	}
	if input.TeamMainVersion != nil && *input.TeamMainVersion != selected.MainVersion {
		return ErrDriverTeamConflict
	}
	if selected.MainDriverID == driver {
		return chargeInvalid("Select another driver's truck for team driving")
	}
	if selected.TeamDriverID != nil && *selected.TeamDriverID != driver {
		return chargeInvalid("This truck's main driver already has a team driver")
	}
	input.PayType = "cpm"
	input.PayRate = 0
	input.DispatcherID = selected.DispatcherID
	input.TruckID = nil
	if driver == "" {
		// The caller inserts the profile first, then calls again with this reviewed
		// truck selection; no incomplete team can escape the transaction.
		return nil
	}
	if current != nil && current.TeamDriverID == driver && current.MainDriverID == selected.MainDriverID {
		return nil
	}
	if input.AssignmentWeek == "" {
		return chargeInvalid("Select the Monday when this driver should join the team")
	}
	if current != nil && current.TeamDriverID == driver {
		previous, e := readDriverTeams(ctx, tx, current.MainDriverID)
		if e != nil {
			return e
		}
		if _, e = setDriverTeamTx(ctx, tx, current.MainDriverID, DriverTeamInput{WeekStart: week, Version: previous.Version}, input.ChargeActor); e != nil {
			return e
		}
	}
	if err = releaseDriverTruck(ctx, tx, driver); err != nil {
		return err
	}
	_, err = setDriverTeamTx(ctx, tx, selected.MainDriverID, DriverTeamInput{TeamDriverID: driver, WeekStart: week, Version: selected.MainVersion}, input.ChargeActor)
	return err
}

func driverStartWeek(input DriverInput) string {
	if input.HireDate != nil {
		return input.HireDate.AddDate(0, 0, -(int(input.HireDate.Weekday())+6)%7).Format(time.DateOnly)
	}
	if input.AssignmentWeek != "" {
		return input.AssignmentWeek
	}
	return ChargeCurrentWeek()
}
