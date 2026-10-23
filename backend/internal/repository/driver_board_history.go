package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type DriverBoardEvent struct {
	ID         int64             `json:"id"`
	DriverID   string            `json:"driverId"`
	DriverName string            `json:"driverName"`
	ActorID    string            `json:"actorId"`
	ActorName  string            `json:"actorName"`
	Source     string            `json:"source"`
	UndoOf     *int64            `json:"undoOf"`
	Before     map[string]string `json:"before"`
	After      map[string]string `json:"after"`
	CreatedAt  time.Time         `json:"createdAt"`
}

type DriverBoardHistory struct {
	Items      []DriverBoardEvent `json:"items"`
	NextCursor int64              `json:"nextCursor"`
}

func setBoardActor(ctx context.Context, tx pgx.Tx, actor, source string) error {
	if _, err := tx.Exec(ctx, `SELECT fleet_status_lock()`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT set_config('mserp.board_actor',$1,true),set_config('mserp.board_source',$2,true)`, actor, source)
	return err
}

// Only return events produced by this write, never another user's latest event.
func attachBoardUndoID(ctx context.Context, tx pgx.Tx, entry *DriverBoardEntry) error {
	err := tx.QueryRow(ctx, `SELECT id FROM driver_board_history WHERE driver_id=$1 AND transaction_id=txid_current() AND source='board' AND undo_of IS NULL`, entry.DriverID).Scan(&entry.UndoID)
	if errors.Is(err, pgx.ErrNoRows) {
		entry.UndoID = 0
		return nil
	}
	return err
}

func (r *DriverBoardRepository) History(ctx context.Context, ids []string, before int64, fields ...string) (DriverBoardHistory, error) {
	result := DriverBoardHistory{Items: []DriverBoardEvent{}}
	field := ""
	if len(fields) > 0 {
		field = fields[0]
	}
	rows, err := r.pool.Query(ctx, `SELECT id,driver_id,driver_name,actor_id,actor_name,source,undo_of,before_values,after_values,created_at
 FROM driver_board_history WHERE (coalesce(cardinality($1::uuid[]),0)=0 OR driver_id=ANY($1::uuid[])) AND ($2::bigint=0 OR id<$2) AND ($3::text='' OR after_values ? $3) ORDER BY id DESC LIMIT 51`, ids, before, field)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var e DriverBoardEvent
		if err = rows.Scan(&e.ID, &e.DriverID, &e.DriverName, &e.ActorID, &e.ActorName, &e.Source, &e.UndoOf, &e.Before, &e.After, &e.CreatedAt); err != nil {
			return result, err
		}
		result.Items = append(result.Items, e)
	}
	if len(result.Items) > 50 {
		result.Items = result.Items[:50]
		result.NextCursor = result.Items[49].ID
	}
	return result, rows.Err()
}

// Undo is a new audited correction. A later edit of any affected field prevents
// reversing the older event, even if that field has since returned to its value.
func (r *DriverBoardRepository) Undo(ctx context.Context, id int64, driver string, version int, actor string, personal ...bool) (DriverBoardEntry, error) {
	var entry DriverBoardEntry
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return entry, err
	}
	defer tx.Rollback(ctx)
	var active bool
	err = tx.QueryRow(ctx, `SELECT active FROM drivers WHERE id=$1 FOR UPDATE`, driver).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return entry, ErrDriverBoardConflict
	}
	if err != nil {
		return entry, err
	}
	if !active {
		return entry, ErrDriverBoardConflict
	}
	var before, after map[string]string
	mine := len(personal) > 0 && personal[0]
	err = tx.QueryRow(ctx, `SELECT before_values,after_values FROM driver_board_history h WHERE id=$1 AND driver_id=$2
 AND (NOT $3::boolean OR (actor_id=$4 AND actor_id<>'' AND source='board' AND undo_of IS NULL
 AND NOT EXISTS(SELECT 1 FROM driver_board_history u WHERE u.undo_of=h.id)))`, id, driver, mine, actor).Scan(&before, &after)
	if errors.Is(err, pgx.ErrNoRows) {
		return entry, ErrNotFound
	}
	if err != nil {
		return entry, err
	}
	// The retired home field is immutable history; it cannot restore a second address.
	if _, retired := after["driverHome"]; retired {
		return entry, ErrDriverBoardConflict
	}
	keys := make([]string, 0, len(after))
	for k := range after {
		keys = append(keys, k)
	}
	var changed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_board_history h WHERE driver_id=$1 AND id>$2 AND after_values ?| $3::text[]
 AND (NOT $4::boolean OR NOT (
 (h.actor_id=$5 AND h.undo_of IS NOT NULL AND EXISTS(SELECT 1 FROM driver_board_history original WHERE original.id=h.undo_of AND original.actor_id=$5))
 OR (h.actor_id=$5 AND EXISTS(SELECT 1 FROM driver_board_history u WHERE u.undo_of=h.id AND u.actor_id=$5))
 )))`, driver, id, keys, mine, actor).Scan(&changed); err != nil {
		return entry, err
	}
	if changed {
		return entry, ErrDriverBoardConflict
	}
	err = tx.QueryRow(ctx, `SELECT d.id,coalesce(b.current_load,''),coalesce(b.trailer_number,''),coalesce(b.trailer_note,''),coalesce(b.status,''),coalesce(b.destination,''),coalesce(b.eta,''),coalesce(b.notes,''),coalesce(b.home_time,''),coalesce(b.version,0),coalesce(b.leave_plan,'{}'::jsonb)
 FROM drivers d LEFT JOIN driver_board b ON b.driver_id=d.id WHERE d.id=$1`, driver).Scan(&entry.DriverID, &entry.CurrentLoad, &entry.TrailerNumber, &entry.TrailerNote, &entry.Status, &entry.Destination, &entry.ETA, &entry.Notes, &entry.HomeTime, &entry.Version, &entry.LeavePlan)
	if err != nil {
		return entry, err
	}
	if entry.Version != version {
		return entry, ErrDriverBoardConflict
	}
	raw, _ := json.Marshal(entry)
	var current map[string]any
	_ = json.Unmarshal(raw, &current)
	_, stateRaw, err := loadState(ctx, tx, driver)
	if err != nil {
		return entry, err
	}
	current["loadPlan"] = stateRaw
	planRaw, _ := json.Marshal(entry.LeavePlan)
	current["leavePlan"] = string(planRaw)
	for k, v := range after {
		if k == "leavePlan" {
			var expected DriverLeavePlan
			if err = json.Unmarshal([]byte(v), &expected); err != nil {
				return entry, err
			}
			normalized, _ := json.Marshal(expected)
			v = string(normalized)
		}
		if current[k] != v {
			return entry, ErrDriverBoardConflict
		}
		current[k] = before[k]
	}
	if value, ok := current["leavePlan"].(string); ok {
		current["leavePlan"] = json.RawMessage(value)
	}
	raw, _ = json.Marshal(current)
	if err = json.Unmarshal(raw, &entry); err != nil {
		return entry, err
	}
	if err = setBoardActor(ctx, tx, actor, "undo"); err != nil {
		return entry, err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mserp.board_undo',$1,true)`, strconv.FormatInt(id, 10)); err != nil {
		return entry, err
	}
	saved, err := saveDriverBoardEntries(ctx, tx, []DriverBoardEntry{entry})
	if err != nil {
		return entry, err
	}
	if previous, ok := before["loadPlan"]; ok {
		var restored boardLoadState
		if err = json.Unmarshal([]byte(previous), &restored); err != nil {
			return entry, err
		}
		if err = saveLoadState(ctx, tx, driver, restored); err != nil {
			return entry, err
		}
	}
	if _, etaChanged := after["eta"]; etaChanged {
		views, readErr := readBoardLoads(ctx, tx, []string{driver}, r.loadStartDate(""))
		if readErr != nil {
			return entry, readErr
		}
		if a := views[driver].Automation; a != nil {
			if _, err = tx.Exec(ctx, `INSERT INTO driver_board_automation(driver_id,eta_context) VALUES($1,$2) ON CONFLICT(driver_id) DO UPDATE SET eta_context=EXCLUDED.eta_context`, driver, a.Context); err != nil {
				return entry, err
			}
		}
	}
	return saved[0], tx.Commit(ctx)
}
