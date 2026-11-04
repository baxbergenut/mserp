package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// TruckAssignmentChoice contains only the fleet evidence needed for a reviewed handover.
type TruckAssignmentChoice struct {
	ID           string `json:"id"`
	Unit         string `json:"unit"`
	Status       string `json:"status"`
	Active       bool   `json:"active"`
	DriverID     string `json:"driverId"`
	DriverName   string `json:"driverName"`
	AssignmentID string `json:"assignmentId"`
	UpdatedAt    string `json:"updatedAt"`
	ActiveLoad   string `json:"activeLoad"`
}

type DriverTruckAssignment struct {
	DriverID      string                  `json:"driverId"`
	DriverName    string                  `json:"driverName"`
	Active        bool                    `json:"active"`
	TeamVersion   int                     `json:"teamVersion"`
	TeamID        string                  `json:"teamId"`
	EffectiveDate string                  `json:"effectiveDate"`
	TruckID       string                  `json:"truckId"`
	Choices       []TruckAssignmentChoice `json:"choices"`
	Revision      string                  `json:"revision"`
}

type DriverTruckAssignmentInput struct {
	TruckID         string `json:"truckId"`
	Revision        string `json:"revision"`
	ConfirmReassign bool   `json:"confirmReassign"`
}

func readDriverTruckAssignment(ctx context.Context, tx pgx.Tx, id string) (DriverTruckAssignment, error) {
	v := DriverTruckAssignment{Choices: []TruckAssignmentChoice{}}
	err := tx.QueryRow(ctx, `SELECT main.id::text,main.full_name,(d.active AND main.active),
 main.team_version,coalesce(team.id::text,''),(now() AT TIME ZONE 'America/New_York')::date::text,
 coalesce(a.truck_id::text,'')
 FROM drivers d
 LEFT JOIN LATERAL (SELECT dt.id,dt.main_driver_id FROM driver_teams dt
 WHERE (dt.main_driver_id=d.id OR dt.team_driver_id=d.id)
 AND dt.start_week<=(now() AT TIME ZONE 'America/New_York')::date
 AND coalesce(dt.end_week,'infinity'::date)>(now() AT TIME ZONE 'America/New_York')::date LIMIT 1) team ON true
 JOIN drivers main ON main.id=coalesce(team.main_driver_id,d.id)
 LEFT JOIN truck_driver_assignments a ON a.driver_id=main.id AND a.unassigned_at IS NULL
 WHERE d.id=$1`, id).Scan(&v.DriverID, &v.DriverName, &v.Active, &v.TeamVersion, &v.TeamID, &v.EffectiveDate, &v.TruckID)
	if err != nil {
		return v, mapNotFound(err)
	}
	v.Choices, err = readTruckAssignmentChoices(ctx, tx)
	if err != nil {
		return v, err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	hash := sha256.Sum256(data)
	v.Revision = hex.EncodeToString(hash[:])
	return v, nil
}

func readTruckAssignmentChoices(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) ([]TruckAssignmentChoice, error) {
	choices := []TruckAssignmentChoice{}
	rows, err := q.Query(ctx, `SELECT t.id::text,t.unit_number,fleet_truck_operating_status(t.id),t.active,
 coalesce(a.driver_id::text,''),coalesce(d.full_name,''),coalesce(a.id::text,''),t.updated_at::text,
 btrim(coalesce(b.current_load,''))
 FROM trucks t LEFT JOIN truck_driver_assignments a ON a.truck_id=t.id AND a.unassigned_at IS NULL
 LEFT JOIN drivers d ON d.id=a.driver_id LEFT JOIN driver_board b ON b.driver_id=a.driver_id
 ORDER BY t.unit_number,t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c TruckAssignmentChoice
		if err = rows.Scan(&c.ID, &c.Unit, &c.Status, &c.Active, &c.DriverID, &c.DriverName, &c.AssignmentID, &c.UpdatedAt, &c.ActiveLoad); err != nil {
			return nil, err
		}
		choices = append(choices, c)
	}
	return choices, rows.Err()
}

func (r *FleetRepository) TruckAssignmentChoices(ctx context.Context) ([]TruckAssignmentChoice, error) {
	return readTruckAssignmentChoices(ctx, r.pool)
}

func (r *FleetRepository) GetDriverTruckAssignment(ctx context.Context, id string) (DriverTruckAssignment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DriverTruckAssignment{}, err
	}
	defer tx.Rollback(ctx)
	return readDriverTruckAssignment(ctx, tx, id)
}

func (r *FleetRepository) AssignDriverTruckToday(ctx context.Context, id string, input DriverTruckAssignmentInput, actor string) error {
	tx, err := beginPayrollMutation(ctx, r.pool)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var today string
	if err = tx.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'America/New_York')::date::text`).Scan(&today); err != nil {
		return err
	}
	if err = setAssignmentWeek(ctx, tx, today); err != nil {
		return err
	}
	// Board edits/actions/undo take this lock before driver rows. Hold it through
	// the read and assignment commit so a concurrent load cannot slip past the check.
	if _, err = tx.Exec(ctx, `SELECT fleet_status_lock()`); err != nil {
		return err
	}
	v, err := readDriverTruckAssignment(ctx, tx, id)
	if err != nil {
		return err
	}
	for _, choice := range v.Choices {
		if choice.ID == input.TruckID && choice.DriverID != v.DriverID && choice.ActiveLoad != "" {
			return chargeInvalid("Truck %s cannot be reassigned: %s has active load %s. Complete or transfer the load first.", choice.Unit, choice.DriverName, choice.ActiveLoad)
		}
	}
	if input.Revision == "" || input.Revision != v.Revision {
		return chargeInvalid("Assignments changed. Close and reopen the truck dropdown to review the latest choices.")
	}
	if !v.Active {
		return chargeInvalid("Only active drivers can be assigned")
	}
	if v.TruckID == input.TruckID {
		return nil
	}
	var selected *TruckAssignmentChoice
	for i := range v.Choices {
		if v.Choices[i].ID == input.TruckID {
			selected = &v.Choices[i]
			break
		}
	}
	if selected == nil {
		return chargeInvalid("Select an existing truck")
	}
	if !selected.Active || (selected.Status != "ready" && selected.Status != "in_use" && selected.Status != "shop") {
		return chargeInvalid("Return the truck to service before assigning a driver")
	}
	if selected.DriverID != "" && !input.ConfirmReassign {
		return chargeInvalid("Confirm reassignment from %s before continuing", selected.DriverName)
	}
	ids := []string{v.DriverID}
	if selected.DriverID != "" && selected.DriverID != v.DriverID {
		ids = append(ids, selected.DriverID)
	}
	if err = lockChargeDrivers(ctx, tx, ids); err != nil {
		return err
	}
	if err = setBoardActor(ctx, tx, actor, "status_board"); err != nil {
		return err
	}
	if err = assignTruck(ctx, tx, input.TruckID, v.DriverID); err != nil {
		return err
	}
	return commitPayrollMutation(ctx, tx)
}
