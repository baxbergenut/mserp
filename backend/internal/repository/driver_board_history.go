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
	_, err := tx.Exec(ctx, `SELECT set_config('mserp.board_actor',$1,true),set_config('mserp.board_source',$2,true)`, actor, source)
	return err
}

func (r *DriverBoardRepository) History(ctx context.Context, ids []string, before int64) (DriverBoardHistory, error) {
	result := DriverBoardHistory{Items: []DriverBoardEvent{}}
	rows, err := r.pool.Query(ctx, `SELECT id,driver_id,driver_name,actor_id,actor_name,source,undo_of,before_values,after_values,created_at
 FROM driver_board_history WHERE driver_id=ANY($1::uuid[]) AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 51`, ids, before)
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
func (r *DriverBoardRepository) Undo(ctx context.Context, id int64, driver string, version, homeVersion int, actor string) (DriverBoardEntry, error) {
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
	err = tx.QueryRow(ctx, `SELECT before_values,after_values FROM driver_board_history WHERE id=$1 AND driver_id=$2`, id, driver).Scan(&before, &after)
	if errors.Is(err, pgx.ErrNoRows) {
		return entry, ErrNotFound
	}
	if err != nil {
		return entry, err
	}
	keys := make([]string, 0, len(after))
	for k := range after {
		keys = append(keys, k)
	}
	var changed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM driver_board_history WHERE driver_id=$1 AND id>$2 AND after_values ?| $3::text[])`, driver, id, keys).Scan(&changed); err != nil {
		return entry, err
	}
	if changed {
		return entry, ErrDriverBoardConflict
	}
	err = tx.QueryRow(ctx, `SELECT d.id,coalesce(b.current_load,''),coalesce(b.trailer_number,''),coalesce(b.status,''),coalesce(b.destination,''),coalesce(b.eta,''),coalesce(b.notes,''),coalesce(b.home_time,''),d.driver_home,d.driver_home_version,coalesce(b.version,0)
 FROM drivers d LEFT JOIN driver_board b ON b.driver_id=d.id WHERE d.id=$1`, driver).Scan(&entry.DriverID, &entry.CurrentLoad, &entry.TrailerNumber, &entry.Status, &entry.Destination, &entry.ETA, &entry.Notes, &entry.HomeTime, &entry.DriverHome, &entry.HomeVersion, &entry.Version)
	if err != nil {
		return entry, err
	}
	if entry.Version != version || entry.HomeVersion != homeVersion {
		return entry, ErrDriverBoardConflict
	}
	raw, _ := json.Marshal(entry)
	var current map[string]any
	_ = json.Unmarshal(raw, &current)
	for k, v := range after {
		if current[k] != v {
			return entry, ErrDriverBoardConflict
		}
		current[k] = before[k]
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
	return saved[0], tx.Commit(ctx)
}
