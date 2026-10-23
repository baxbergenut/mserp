package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type TerminatedOwnerTruck struct {
	TruckID       string          `json:"truckId"`
	UnitNumber    string          `json:"unitNumber"`
	DriverID      string          `json:"driverId"`
	DriverName    string          `json:"driverName"`
	TerminationID string          `json:"terminationId"`
	Active        bool            `json:"active"`
	ActiveDrivers []string        `json:"activeDrivers"`
	Outcome       string          `json:"outcome,omitempty"`
	Snapshot      json.RawMessage `json:"-"`
}

type TerminatedDriverAssignments struct {
	DriverID     string `json:"driverId"`
	DriverName   string `json:"driverName"`
	DispatcherID string `json:"dispatcherId"`
	TruckID      string `json:"truckId"`
	TruckUnit    string `json:"truckUnit"`
	RetainTruck  bool   `json:"retainTruck"`
	Revision     string `json:"revision"`
}

type TerminationCleanup struct {
	Revision string                        `json:"revision"`
	Trucks   []TerminatedOwnerTruck        `json:"trucks"`
	Drivers  []TerminatedDriverAssignments `json:"drivers"`
}

// A current active co-driver is just as important as a direct assignment.
// Names here are display snapshots only; all decisions use saved UUID links.
const terminatedOwnerTrucksSQL = `SELECT t.id::text,t.unit_number,d.id::text,d.full_name,d.termination_id::text,t.active,
 ARRAY(SELECT person.full_name FROM drivers person WHERE person.active AND (
 EXISTS(SELECT 1 FROM truck_driver_assignments a WHERE a.truck_id=t.id AND a.driver_id=person.id AND a.unassigned_at IS NULL)
 OR EXISTS(SELECT 1 FROM driver_teams team JOIN truck_driver_assignments a ON a.driver_id=team.main_driver_id AND a.truck_id=t.id AND a.unassigned_at IS NULL
 WHERE (person.id=team.main_driver_id OR person.id=team.team_driver_id)
 AND team.start_week <= (now() AT TIME ZONE 'America/New_York')::date
 AND (team.end_week IS NULL OR team.end_week > (now() AT TIME ZONE 'America/New_York')::date))) ORDER BY person.id),
 coalesce(receipt.outcome,''),jsonb_build_object('truck',to_jsonb(t),'ownerUpdatedAt',d.updated_at,
 'assignments',coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM truck_driver_assignments a WHERE a.truck_id=t.id AND a.unassigned_at IS NULL),'[]'::jsonb),
 'teams',coalesce((SELECT jsonb_agg(to_jsonb(team) ORDER BY team.id) FROM driver_teams team JOIN truck_driver_assignments a ON a.driver_id=team.main_driver_id AND a.truck_id=t.id AND a.unassigned_at IS NULL
 WHERE team.start_week <= (now() AT TIME ZONE 'America/New_York')::date AND (team.end_week IS NULL OR team.end_week > (now() AT TIME ZONE 'America/New_York')::date)),'[]'::jsonb))
 FROM trucks t JOIN investors owner ON owner.id=t.owner_id JOIN drivers d ON d.id=owner.driver_id
 LEFT JOIN driver_termination_truck_actions receipt ON receipt.termination_id=d.termination_id AND receipt.truck_id=t.id
 WHERE NOT d.active AND ($1='' OR d.id::text=$1) ORDER BY t.id`

func readTerminatedOwnerTrucks(ctx context.Context, q chargeQuery, driver string) ([]TerminatedOwnerTruck, error) {
	rows, err := q.Query(ctx, terminatedOwnerTrucksSQL, driver)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TerminatedOwnerTruck{}
	for rows.Next() {
		var v TerminatedOwnerTruck
		if err = rows.Scan(&v.TruckID, &v.UnitNumber, &v.DriverID, &v.DriverName, &v.TerminationID, &v.Active, &v.ActiveDrivers, &v.Outcome, &v.Snapshot); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func recordTerminatedOwnerTrucks(ctx context.Context, tx pgx.Tx, driver, actor, source string) error {
	trucks, err := readTerminatedOwnerTrucks(ctx, tx, driver)
	if err != nil {
		return err
	}
	for _, truck := range trucks {
		if truck.Outcome != "" {
			continue
		}
		// Fleet status serialization is acquired by every caller before row locks.
		if _, err = tx.Exec(ctx, `SELECT id FROM trucks WHERE id=$1 FOR UPDATE`, truck.TruckID); err != nil {
			return err
		}
		outcome := "already_inactive"
		var task *string
		if len(truck.ActiveDrivers) > 0 {
			outcome = "held"
			var id string
			notes := fmt.Sprintf("Owner %s is terminated. Truck %s was left unchanged because active drivers are linked: %s. Review the operating crew before changing this truck's active status. Ownership and existing assignments were retained.\nTruck: /trucks/detail?id=%s\nOwner: /drivers/detail?id=%s", truck.DriverName, truck.UnitNumber, strings.Join(truck.ActiveDrivers, ", "), truck.TruckID, truck.DriverID)
			title := []rune("Review terminated owner's truck " + truck.UnitNumber)
			if len(title) > 200 {
				title = title[:200]
			}
			if err = tx.QueryRow(ctx, `INSERT INTO custom_tasks(title,notes,system_task_kind) VALUES($1,$2,'driver_offboarding') RETURNING id::text`, string(title), notes).Scan(&id); err != nil {
				return err
			}
			task = &id
		} else if truck.Active {
			outcome = "inactivated"
			if _, err = tx.Exec(ctx, `UPDATE trucks SET active=false,updated_at=now() WHERE id=$1`, truck.TruckID); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO profile_notes(id,truck_id,body,actor_id,actor_name)
 VALUES(gen_random_uuid(),$1,$2,nullif($3,'')::uuid,coalesce((SELECT username FROM app_users WHERE id=nullif($3,'')::uuid),'System'))`, truck.TruckID, "Made inactive after owner "+truck.DriverName+" was terminated. Owner and existing owner-driver link retained; no active driver was linked.", actor); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO driver_termination_truck_actions(termination_id,driver_id,truck_id,outcome,task_id,before_value,after_value,actor_id,source)
 SELECT $1,$2,$3,$4,$5,$6,to_jsonb(t),nullif($7,'')::uuid,$8 FROM trucks t WHERE t.id=$3`, truck.TerminationID, truck.DriverID, truck.TruckID, outcome, task, truck.Snapshot, actor, source); err != nil {
			return err
		}
	}
	return nil
}

// Only an already-existing assignment to the driver's own truck can survive
// termination. Never restore an ended assignment or use the legacy type flag.
func offboardDriverAssignments(ctx context.Context, tx pgx.Tx, driver, actor, source string) error {
	var nonOwned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM truck_driver_assignments a JOIN trucks t ON t.id=a.truck_id
 JOIN investors o ON o.id=t.owner_id WHERE a.driver_id=$1 AND a.unassigned_at IS NULL AND o.driver_id IS DISTINCT FROM a.driver_id)`, driver).Scan(&nonOwned); err != nil {
		return err
	}
	if nonOwned {
		if err := releaseDriverTruck(ctx, tx, driver); err != nil {
			return err
		}
	}
	return recordTerminatedOwnerTrucks(ctx, tx, driver, actor, source)
}

func (r *FleetRepository) PreviewTerminationCleanup(ctx context.Context) (TerminationCleanup, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return TerminationCleanup{}, err
	}
	defer tx.Rollback(ctx)
	return previewTerminationCleanup(ctx, tx)
}

func previewTerminationCleanup(ctx context.Context, tx pgx.Tx) (TerminationCleanup, error) {
	out := TerminationCleanup{Drivers: []TerminatedDriverAssignments{}}
	var err error
	out.Trucks, err = readTerminatedOwnerTrucks(ctx, tx, "")
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT d.id::text,d.full_name,coalesce(d.dispatcher_id::text,''),coalesce(t.id::text,''),coalesce(t.unit_number,''),coalesce(o.driver_id=d.id,false),md5(to_jsonb(d)::text||coalesce(to_jsonb(a)::text,''))
 FROM drivers d LEFT JOIN truck_driver_assignments a ON a.driver_id=d.id AND a.unassigned_at IS NULL
 LEFT JOIN trucks t ON t.id=a.truck_id LEFT JOIN investors o ON o.id=t.owner_id
 WHERE NOT d.active AND (d.dispatcher_id IS NOT NULL OR a.id IS NOT NULL) ORDER BY d.id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v TerminatedDriverAssignments
		if err = rows.Scan(&v.DriverID, &v.DriverName, &v.DispatcherID, &v.TruckID, &v.TruckUnit, &v.RetainTruck, &v.Revision); err != nil {
			rows.Close()
			return out, err
		}
		out.Drivers = append(out.Drivers, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// Include full before snapshots, including current crew/ownership revisions.
	data, _ := json.Marshal(out)
	h := sha256.New()
	h.Write(data)
	for _, truck := range out.Trucks {
		h.Write(truck.Snapshot)
	}
	out.Revision = hex.EncodeToString(h.Sum(nil))
	return out, nil
}

// Operator-only correction: exact snapshot fence, one atomic commit, no jobs or
// external provider calls. Historical boundaries use today's date, never a
// guessed retrospective termination date.
func (r *FleetRepository) ApplyTerminationCleanup(ctx context.Context, revision string) (TerminationCleanup, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return TerminationCleanup{}, err
	}
	defer tx.Rollback(ctx)
	if err = setBoardActor(ctx, tx, "", "termination_cleanup"); err != nil {
		return TerminationCleanup{}, err
	}
	loc, _ := time.LoadLocation("America/New_York")
	if err = setAssignmentWeek(ctx, tx, time.Now().In(loc).Format(time.DateOnly)); err != nil {
		return TerminationCleanup{}, err
	}
	out, err := previewTerminationCleanup(ctx, tx)
	if err != nil {
		return out, err
	}
	if revision == "" || out.Revision != revision {
		return out, ErrDriverBoardConflict
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mserp.assignment_source','termination_cleanup',true)`); err != nil {
		return out, err
	}
	for _, driver := range out.Drivers {
		if _, err = tx.Exec(ctx, `UPDATE drivers SET dispatcher_id=NULL,updated_at=now() WHERE id=$1 AND dispatcher_id IS NOT NULL`, driver.DriverID); err != nil {
			return out, err
		}
		if !driver.RetainTruck && driver.TruckID != "" {
			if err = releaseDriverTruck(ctx, tx, driver.DriverID); err != nil {
				return out, err
			}
		}
	}
	if err = recordTerminatedOwnerTrucks(ctx, tx, "", "", "termination_cleanup"); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return r.PreviewTerminationCleanup(ctx)
}
