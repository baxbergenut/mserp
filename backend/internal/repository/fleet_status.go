package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	Status      string    `json:"status"`
	Version     int       `json:"version"`
	EffectiveAt time.Time `json:"effectiveAt"`
	Reason      string    `json:"reason"`
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
	command, err := tx.Exec(ctx, `UPDATE drivers SET availability_status=$2,updated_at=CASE WHEN availability_status<>$2 THEN now() ELSE updated_at END WHERE id=$1 AND status_version=$3 AND active`, id, in.Status, in.Version)
	if err != nil {
		return Driver{}, err
	}
	if command.RowsAffected() != 1 {
		return Driver{}, ErrDriverBoardConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return Driver{}, err
	}
	return r.GetDriver(ctx, id)
}
func (r *FleetRepository) ChangeTruckCondition(ctx context.Context, id, actor string, in FleetStatusChange) (Truck, error) {
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
	if in.Status == "ready" {
		var held bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM maintenance_records WHERE truck_id=$1 AND dispatch_status<>'')`, id).Scan(&held); err != nil {
			return Truck{}, err
		}
		if held {
			return Truck{}, chargeInvalid("Release the remaining work-order restrictions first")
		}
	}
	_, err = tx.Exec(ctx, `UPDATE trucks SET status=CASE $2 WHEN 'ready' THEN CASE WHEN EXISTS(SELECT 1 FROM truck_driver_assignments WHERE truck_id=$1 AND unassigned_at IS NULL) THEN 'assigned' ELSE 'available' END WHEN 'shop' THEN 'maintenance' ELSE $2 END,updated_at=now() WHERE id=$1`, id, in.Status)
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
	rows, err := r.pool.Query(ctx, `WITH periods AS (
 SELECT h.*,lead(effective_at) OVER(PARTITION BY entity_kind,entity_id,track ORDER BY effective_at,id) AS ended_at
 FROM fleet_status_history h WHERE id<=$1
 ), assignments AS (
 SELECT truck_id,assigned_at,unassigned_at FROM truck_driver_assignments WHERE driver_id=$3::uuid
 UNION ALL SELECT a.truck_id,greatest(a.assigned_at,team.start_week::timestamp AT TIME ZONE 'America/New_York'),
 least(a.unassigned_at,team.end_week::timestamp AT TIME ZONE 'America/New_York')
 FROM driver_teams team JOIN truck_driver_assignments a ON a.driver_id=team.main_driver_id
 WHERE team.team_driver_id=$3::uuid AND a.assigned_at<coalesce(team.end_week::timestamp AT TIME ZONE 'America/New_York','infinity'::timestamptz)
 AND coalesce(a.unassigned_at,'infinity'::timestamptz)>team.start_week::timestamp AT TIME ZONE 'America/New_York'
 ), visible AS (
 SELECT p.*,p.track AS shown_track,p.effective_at AS started,p.ended_at AS ended FROM periods p WHERE entity_kind=$2 AND entity_id=$3::uuid AND p.track<>'dispatch'
 UNION ALL SELECT p.*,'assigned_truck',greatest(p.effective_at,a.assigned_at),least(p.ended_at,a.unassigned_at)
 FROM periods p JOIN assignments a ON a.truck_id=p.entity_id
 WHERE $2='driver' AND p.entity_kind='truck' AND p.track='condition'
 AND p.effective_at<coalesce(a.unassigned_at,'infinity'::timestamptz) AND coalesce(p.ended_at,'infinity'::timestamptz)>a.assigned_at
 ) SELECT id,entity_kind,entity_id::text,entity_name,shown_track,previous_status,status,started,ended,recorded_at,actor_name,source,source_ref,related_truck_name,action_id,baseline,reason
 FROM visible WHERE ($4='' OR shown_track=$4) AND ($5='' OR status=$5)
 AND ($6='' OR coalesce(ended,'infinity'::timestamptz)>=($6::date::timestamp AT TIME ZONE 'America/New_York'))
 AND ($7='' OR started<(($7::date+1)::timestamp AT TIME ZONE 'America/New_York'))
 ORDER BY started DESC,id DESC LIMIT 51 OFFSET $8`, result.Snapshot, kind, id, track, status, from, to, (page-1)*50)
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
 UPDATE trucks t SET status=CASE WHEN t.status='out_of_service' THEN t.status WHEN t.status='hold' AND r.truck_condition='shop' THEN t.status WHEN r.truck_condition='shop' THEN 'maintenance' ELSE r.truck_condition END
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
