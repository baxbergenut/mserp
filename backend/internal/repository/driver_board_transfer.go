package repository

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
)

// A board drop takes effect at the database clock, never at a payroll week
// boundary. The existing assignment trigger retains the previous interval and
// actor attribution. Both complete rosters are fenced, including hidden rows.
func (r *DriverBoardRepository) transferBoardDriver(ctx context.Context, tx pgx.Tx, a DriverBoardOrderAction, actor string) (DriverBoardOrder, error) {
	destination := *a.TargetDispatcherID
	result := DriverBoardOrder{DispatcherID: destination}
	if err := setBoardActor(ctx, tx, actor, "board"); err != nil {
		return result, err
	}
	keys := []string{a.DispatcherID, destination}
	slices.Sort(keys)
	for _, key := range keys {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('status-board-order:'||$1,0))`, key); err != nil {
			return result, err
		}
	}
	if destination != "" {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM dispatchers WHERE id=$1 AND active FOR SHARE`, destination).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return result, ErrDriverBoardOrderConflict
			}
			return result, err
		}
	}
	groups := map[string][]string{}
	versions := map[string]int64{}
	for _, key := range keys {
		rows, err := tx.Query(ctx, `SELECT d.id FROM drivers d WHERE d.active
 AND payroll_driver_at(d.id,(now() AT TIME ZONE 'America/New_York')::date)=d.id
 AND coalesce(d.dispatcher_id::text,'')=$1 ORDER BY d.full_name,d.id FOR UPDATE OF d`, key)
		if err != nil {
			return result, err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return result, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		var saved []string
		var version int64
		err = tx.QueryRow(ctx, `SELECT driver_ids,version FROM driver_board_driver_order WHERE dispatcher_key=$1`, key).Scan(&saved, &version)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, err
		}
		groups[key], versions[key] = boardOrder(ids, saved), version
	}
	source, target := groups[a.DispatcherID], groups[destination]
	if boardOrderRevision(a.DispatcherID, versions[a.DispatcherID], source) != a.Revision || boardOrderRevision(destination, versions[destination], target) != a.TargetRevision || !slices.Contains(source, a.DriverID) || (a.TargetDriverID != "" && !slices.Contains(target, a.TargetDriverID)) {
		return result, ErrDriverBoardOrderConflict
	}
	// Explicitly clear effective-week settings: this action is immediate.
	if _, err := tx.Exec(ctx, `SELECT set_config('mserp.assignment_week','',true),set_config('mserp.assignment_source','Status Board drag',true)`); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE drivers SET dispatcher_id=NULLIF($2,'')::uuid,updated_at=clock_timestamp() WHERE id=$1`, a.DriverID, destination); err != nil {
		return result, err
	}
	index := slices.Index(source, a.DriverID)
	groups[a.DispatcherID] = slices.Delete(source, index, index+1)
	index = len(target)
	if a.TargetDriverID != "" {
		index = slices.Index(target, a.TargetDriverID)
		if a.After {
			index++
		}
	}
	groups[destination] = slices.Insert(target, index, a.DriverID)
	for _, key := range keys {
		var version int64
		err := tx.QueryRow(ctx, `INSERT INTO driver_board_driver_order(dispatcher_key,dispatcher_id,driver_ids,updated_by)
 VALUES($1,NULLIF($1,'')::uuid,$2,NULLIF($3,'')::uuid)
 ON CONFLICT(dispatcher_key) DO UPDATE SET driver_ids=EXCLUDED.driver_ids,version=driver_board_driver_order.version+1,updated_by=EXCLUDED.updated_by,updated_at=now() RETURNING version`, key, groups[key], actor).Scan(&version)
		if err != nil {
			return result, err
		}
		versions[key] = version
	}
	result.DriverIDs = groups[destination]
	result.Revision = boardOrderRevision(destination, versions[destination], result.DriverIDs)
	result.SourceRevision = boardOrderRevision(a.DispatcherID, versions[a.DispatcherID], groups[a.DispatcherID])
	return result, tx.Commit(ctx)
}
