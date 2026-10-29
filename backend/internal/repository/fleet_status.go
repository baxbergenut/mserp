package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type FleetStatusRule struct {
	BoardStatus    string `json:"boardStatus"`
	Availability   string `json:"availability"`
	TruckCondition string `json:"truckCondition"`
	RequiresActive bool   `json:"requiresActive"`
	RequiresReady  bool   `json:"requiresReady"`
}
type FleetStatusCatalog struct {
	Enabled bool              `json:"enabled"`
	Rules   []FleetStatusRule `json:"rules"`
}

func (r *FleetRepository) FleetStatusCatalog(ctx context.Context) (FleetStatusCatalog, error) {
	result := FleetStatusCatalog{Rules: []FleetStatusRule{}}
	if err := r.pool.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&result.Enabled); err != nil {
		return result, err
	}
	rows, err := r.pool.Query(ctx, `SELECT board_status,availability,truck_condition,requires_active,requires_ready FROM fleet_status_rules ORDER BY ordinal`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var v FleetStatusRule
		if err = rows.Scan(&v.BoardStatus, &v.Availability, &v.TruckCondition, &v.RequiresActive, &v.RequiresReady); err != nil {
			return result, err
		}
		result.Rules = append(result.Rules, v)
	}
	return result, rows.Err()
}

type FleetStatusChange struct {
	LeavePlan        *DriverLeavePlan `json:"leavePlan,omitempty"`
	LeavePlanVersion int              `json:"leavePlanVersion"`
	Status           string           `json:"status"`
	Version          int              `json:"version"`
	EffectiveAt      time.Time        `json:"effectiveAt"`
	Reason           string           `json:"reason"`
}

func setFleetStatusContext(ctx context.Context, tx pgx.Tx, actor, source string, effective time.Time, reason string) error {
	if err := setBoardActor(ctx, tx, actor, source); err != nil {
		return err
	}
	when := ""
	if !effective.IsZero() {
		when = effective.UTC().Format(time.RFC3339Nano)
	}
	_, err := tx.Exec(ctx, `SELECT set_config('mserp.status_effective_at',$1,true),set_config('mserp.status_reason',$2,true)`, when, reason)
	return err
}
func (r *FleetRepository) ChangeAvailability(ctx context.Context, id, actor string, in FleetStatusChange) (Driver, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Driver{}, err
	}
	defer tx.Rollback(ctx)
	if err = setFleetStatusContext(ctx, tx, actor, "profile", in.EffectiveAt, in.Reason); err != nil {
		return Driver{}, err
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&enabled); err != nil {
		return Driver{}, err
	}
	if !enabled {
		return Driver{}, chargeInvalid("Status reconciliation must be activated first")
	}
	if err = in.LeavePlan.Validate(); err != nil {
		return Driver{}, err
	}
	if in.LeavePlan != nil {
		var driverVersion int
		if err = tx.QueryRow(ctx, `SELECT status_version FROM drivers WHERE id=$1 FOR UPDATE`, id).Scan(&driverVersion); err != nil {
			return Driver{}, mapNotFound(err)
		}
		entry, readErr := boardEntry(ctx, tx, id)
		if readErr != nil {
			return Driver{}, readErr
		}
		if entry.Version != in.LeavePlanVersion {
			return Driver{}, ErrDriverBoardConflict
		}
	}
	command, err := tx.Exec(ctx, `UPDATE drivers SET availability_status=$2,updated_at=CASE WHEN availability_status<>$2 THEN now() ELSE updated_at END WHERE id=$1 AND status_version=$3 AND active`, id, in.Status, in.Version)
	if err != nil {
		return Driver{}, err
	}
	if command.RowsAffected() != 1 {
		return Driver{}, ErrDriverBoardConflict
	}
	if in.LeavePlan != nil {
		_, err = tx.Exec(ctx, `INSERT INTO driver_board(driver_id,leave_plan) VALUES($1,$2) ON CONFLICT(driver_id) DO UPDATE SET leave_plan=EXCLUDED.leave_plan,version=driver_board.version+1,updated_at=now() WHERE driver_board.leave_plan IS DISTINCT FROM EXCLUDED.leave_plan`, id, in.LeavePlan)
		if err != nil {
			return Driver{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Driver{}, err
	}
	return r.GetDriver(ctx, id)
}
func (r *FleetRepository) ChangeTruckCondition(ctx context.Context, id, actor string, in FleetStatusChange) (Truck, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || len(in.Reason) > 1000 {
		return Truck{}, chargeInvalid("A status change reason is required (maximum 1000 characters)")
	}
	switch in.Status {
	case "ready", "in_use", "shop", "out_of_service", "terminated":
	default:
		return Truck{}, chargeInvalid("Invalid truck status")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Truck{}, err
	}
	defer tx.Rollback(ctx)
	if err = setFleetStatusContext(ctx, tx, actor, "profile", in.EffectiveAt, in.Reason); err != nil {
		return Truck{}, err
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&enabled); err != nil {
		return Truck{}, err
	}
	if !enabled {
		return Truck{}, chargeInvalid("Status reconciliation must be activated first")
	}
	var version int
	if err = tx.QueryRow(ctx, `SELECT status_version FROM trucks WHERE id=$1 FOR UPDATE`, id).Scan(&version); err != nil {
		return Truck{}, mapNotFound(err)
	}
	if version != in.Version {
		return Truck{}, ErrDriverBoardConflict
	}
	if in.Status == "ready" || in.Status == "in_use" {
		var held bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM maintenance_records WHERE truck_id=$1 AND dispatch_status<>'')`, id).Scan(&held); err != nil {
			return Truck{}, err
		}
		if held {
			return Truck{}, chargeInvalid("Release the remaining work-order restrictions first")
		}
	}
	_, err = tx.Exec(ctx, `UPDATE trucks SET status=CASE WHEN $2 IN ('ready','in_use') THEN CASE WHEN fleet_truck_has_operator($1) THEN 'assigned' ELSE 'available' END WHEN $2='shop' THEN 'maintenance' ELSE $2 END,updated_at=now() WHERE id=$1`, id, in.Status)
	if err != nil {
		return Truck{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Truck{}, err
	}
	return r.GetTruck(ctx, id)
}

type FleetStatusHistoryEntry struct {
	ID               int64      `json:"id"`
	EntityKind       string     `json:"entityKind"`
	EntityID         string     `json:"entityId"`
	EntityName       string     `json:"entityName"`
	Track            string     `json:"track"`
	PreviousStatus   string     `json:"previousStatus"`
	Status           string     `json:"status"`
	EffectiveAt      time.Time  `json:"effectiveAt"`
	EndedAt          *time.Time `json:"endedAt"`
	RecordedAt       time.Time  `json:"recordedAt"`
	ActorName        string     `json:"actorName"`
	Source           string     `json:"source"`
	SourceRef        string     `json:"sourceRef"`
	RelatedTruckName string     `json:"relatedTruckName"`
	ActionID         int64      `json:"actionId"`
	Baseline         bool       `json:"baseline"`
	Reason           string     `json:"reason"`
}
type FleetStatusHistory struct {
	Items    []FleetStatusHistoryEntry `json:"items"`
	HasMore  bool                      `json:"hasMore"`
	Snapshot int64                     `json:"snapshot"`
}

func (r *FleetRepository) StatusHistory(ctx context.Context, kind, id, track, status, from, to string, page int, snapshot int64) (FleetStatusHistory, error) {
	result := FleetStatusHistory{Items: []FleetStatusHistoryEntry{}, Snapshot: snapshot}
	if snapshot == 0 {
		if err := r.pool.QueryRow(ctx, `SELECT coalesce(max(id),0) FROM fleet_status_history`).Scan(&result.Snapshot); err != nil {
			return result, err
		}
	}
	rows, err := r.pool.Query(ctx, `WITH status_history AS MATERIALIZED (
 SELECT * FROM fleet_status_history WHERE id<=$1 AND entity_kind=$2 AND entity_id=$3::uuid AND track<>'dispatch'
 ), `+effectiveFleetStatusHistoryCTE+`, periods AS (
 SELECT h.*,lead(effective_at) OVER(PARTITION BY entity_kind,entity_id,track ORDER BY effective_at,id) AS ended_at
 FROM effective_status_history h
 ) SELECT id,entity_kind,entity_id::text,entity_name,track,previous_status,status,effective_at,ended_at,recorded_at,actor_name,source,source_ref,related_truck_name,action_id,baseline,reason
 FROM periods WHERE entity_kind=$2 AND entity_id=$3::uuid AND track<>'dispatch'
 AND ($4='' OR track=$4) AND ($5='' OR status=$5)
 AND ($6='' OR coalesce(ended_at,'infinity'::timestamptz)>=($6::date::timestamp AT TIME ZONE 'America/New_York'))
 AND ($7='' OR effective_at<(($7::date+1)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY effective_at DESC,id DESC LIMIT 51 OFFSET $8`, result.Snapshot, kind, id, track, status, from, to, (page-1)*50)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var v FleetStatusHistoryEntry
		if err = rows.Scan(&v.ID, &v.EntityKind, &v.EntityID, &v.EntityName, &v.Track, &v.PreviousStatus, &v.Status, &v.EffectiveAt, &v.EndedAt, &v.RecordedAt, &v.ActorName, &v.Source, &v.SourceRef, &v.RelatedTruckName, &v.ActionID, &v.Baseline, &v.Reason); err != nil {
			return result, err
		}
		result.Items = append(result.Items, v)
	}
	if len(result.Items) > 50 {
		result.Items = result.Items[:50]
		result.HasMore = true
	}
	return result, rows.Err()
}

// Operator activation is fenced by the hash of the complete current snapshot.
// Preview never writes and activation never terminates drivers from board labels.
type FleetStatusPreview struct {
	Enabled  bool            `json:"enabled"`
	Revision string          `json:"revision"`
	Rows     json.RawMessage `json:"rows"`
}

func statusPreview(ctx context.Context, tx pgx.Tx) (FleetStatusPreview, error) {
	var p FleetStatusPreview
	err := tx.QueryRow(ctx, `SELECT fleet_status_enabled(),coalesce(jsonb_agg(row_data ORDER BY row_data->>'id'),'[]'::jsonb) FROM (
 SELECT jsonb_build_object('id',d.id,'driver',d.full_name,'profile',d.status,'availability',d.availability_status,'board',coalesce(b.status,''),'boardVersion',coalesce(b.version,0),'updatedAt',d.updated_at,'truckId',t.id,'truck',t.unit_number,'truckStatus',t.status,'condition',fleet_truck_operating_status(t.id),'truckVersion',t.status_version,'truckUpdatedAt',t.updated_at,'teamVersion',d.team_version,'assignments',coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM truck_driver_assignments a WHERE a.truck_id=t.id AND a.unassigned_at IS NULL),'[]'::jsonb),'workOrderHolds',coalesce((SELECT jsonb_agg(jsonb_build_object('id',m.id,'version',m.version,'status',m.dispatch_status,'updatedAt',m.updated_at) ORDER BY m.id) FROM maintenance_records m WHERE m.truck_id=t.id AND m.dispatch_status<>''),'[]'::jsonb)) row_data
 FROM drivers d LEFT JOIN driver_board b ON b.driver_id=d.id LEFT JOIN trucks t ON t.id=fleet_status_driver_truck(d.id) WHERE d.active) q`).Scan(&p.Enabled, &p.Rows)
	sum := sha256.Sum256(p.Rows)
	p.Revision = hex.EncodeToString(sum[:])
	return p, err
}
func (r *FleetRepository) PreviewStatuses(ctx context.Context) (FleetStatusPreview, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return FleetStatusPreview{}, err
	}
	defer tx.Rollback(ctx)
	return statusPreview(ctx, tx)
}
func (r *FleetRepository) ActivateStatuses(ctx context.Context, revision string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT fleet_status_lock(); LOCK TABLE drivers,trucks,driver_board,maintenance_records,truck_driver_assignments,driver_teams IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	p, err := statusPreview(ctx, tx)
	if err != nil {
		return err
	}
	if p.Enabled {
		return errors.New("status synchronization is already active")
	}
	if p.Revision != revision {
		return errors.New("fleet changed after preview; review a fresh snapshot")
	}
	var unsupported bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_board b JOIN drivers d ON d.id=b.driver_id LEFT JOIN fleet_status_rules r ON r.board_status=b.status WHERE d.active AND b.status<>'' AND (b.status='Left' OR r.board_status IS NULL OR (r.truck_condition<>'' AND fleet_status_driver_truck(d.id) IS NULL)))`).Scan(&unsupported); err != nil {
		return err
	}
	if unsupported {
		return errors.New("resolve departure, retired, or unassigned truck restriction statuses before activation")
	}
	if err = setFleetStatusContext(ctx, tx, "", "activation", time.Time{}, "Reviewed initial reconciliation; earlier status start unknown"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mserp.status_actor_name','Operator (reviewed activation)',true);
 UPDATE drivers d SET availability_status=CASE WHEN r.availability<>'' THEN r.availability ELSE 'active' END FROM driver_board b JOIN fleet_status_rules r ON r.board_status=b.status WHERE b.driver_id=d.id AND d.active AND (r.availability<>'' OR r.requires_active);
 UPDATE trucks t SET status=CASE WHEN t.status IN ('out_of_service','terminated') THEN t.status WHEN r.truck_condition='shop' THEN 'maintenance' ELSE r.truck_condition END
 FROM driver_board b JOIN fleet_status_rules r ON r.board_status=b.status JOIN drivers d ON d.id=b.driver_id
 WHERE d.active AND r.truck_condition<>'' AND t.id=fleet_status_driver_truck(d.id);
 UPDATE trucks SET condition_status=fleet_truck_operating_status(id);
 UPDATE fleet_status_control SET enabled=true;`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT fleet_status_record('driver',id,'employment','',CASE WHEN active THEN 'active' ELSE 'terminated' END,true) FROM drivers;
 SELECT fleet_status_record('driver',id,'availability','',availability_status,true) FROM drivers;
 SELECT fleet_status_record('truck',id,'condition','',condition_status,true) FROM trucks;
 SELECT fleet_status_record('driver',driver_id,'dispatch','',status,true) FROM driver_board;
 SELECT fleet_status_project_driver(id) FROM drivers WHERE active`); err != nil {
		return fmt.Errorf("activation: %w", err)
	}
	return tx.Commit(ctx)
}

func syncFleetStatusProgress(ctx context.Context, tx pgx.Tx, ids []string) error {
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	views, err := readBoardLoads(ctx, tx, ids, boardLoadToday(time.Now()))
	if err != nil {
		return err
	}
	for id, view := range views {
		var current string
		if err = tx.QueryRow(ctx, `SELECT status FROM driver_board WHERE driver_id=$1`, id).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		next := boardProgressStatus(current, view.Next)
		if next != current {
			if _, err = tx.Exec(ctx, `UPDATE driver_board SET status=$2,version=version+1,updated_at=now() WHERE driver_id=$1`, id, next); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r *FleetRepository) ReconcileStatusProgress(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = setBoardActor(ctx, tx, "", "system_queue"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT fleet_status_refresh_truck(id) FROM trucks WHERE condition_status IS DISTINCT FROM fleet_truck_operating_status(id) ORDER BY id`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT driver_id::text FROM driver_board WHERE status IN ('ENROUTE','RESERVED') ORDER BY driver_id`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		if err = syncFleetStatusProgress(ctx, tx, ids); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
