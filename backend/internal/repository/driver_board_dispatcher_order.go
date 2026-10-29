package repository

import (
	"context"
	"errors"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
)

type DispatcherBoardOrderAction struct {
	DispatcherID       string `json:"dispatcherId"`
	TargetDispatcherID string `json:"targetDispatcherId"`
	After              bool   `json:"after"`
	Revision           string `json:"revision"`
}

type DispatcherBoardOrder struct {
	DispatcherIDs []string `json:"dispatcherIds"`
	Revision      string   `json:"revision"`
}

func boardDispatcherIDs(drivers []DriverBoardDriver) []string {
	ids := []string{}
	for _, d := range drivers {
		if !slices.Contains(ids, d.DispatcherID) {
			ids = append(ids, d.DispatcherID)
		}
	}
	return ids
}

func pinUnassigned(ids []string) []string {
	if i := slices.Index(ids, ""); i > 0 {
		ids = slices.Delete(ids, i, i+1)
		ids = slices.Insert(ids, 0, "")
	}
	return ids
}

func applyBoardDispatcherOrder(ctx context.Context, tx pgx.Tx, board *DriverBoard) error {
	ids := boardDispatcherIDs(board.Drivers)
	var saved []string
	var version int64
	err := tx.QueryRow(ctx, `SELECT dispatcher_ids,version FROM driver_board_dispatcher_order WHERE id=true`).Scan(&saved, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	ids = pinUnassigned(boardOrder(ids, saved))
	board.DispatcherOrderRevision = boardOrderRevision("dispatchers", version, ids)
	ranks := map[string]int{}
	for i, id := range ids {
		ranks[id] = i
	}
	sort.SliceStable(board.Drivers, func(i, j int) bool {
		return ranks[board.Drivers[i].DispatcherID] < ranks[board.Drivers[j].DispatcherID]
	})
	return nil
}

func (r *DriverBoardRepository) ReorderDispatchers(ctx context.Context, a DispatcherBoardOrderAction, actor string) (DispatcherBoardOrder, error) {
	result := DispatcherBoardOrder{}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('status-board-dispatcher-order',0))`); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT coalesce(d.dispatcher_id::text,''),coalesce(dp.full_name,'Unassigned'),d.id
 FROM drivers d LEFT JOIN dispatchers dp ON dp.id=d.dispatcher_id
 WHERE d.active AND payroll_driver_at(d.id,(now() AT TIME ZONE 'America/New_York')::date)=d.id
 ORDER BY dp.full_name NULLS LAST,dp.id,d.full_name,d.id FOR UPDATE OF d`)
	if err != nil {
		return result, err
	}
	ids := []string{}
	for rows.Next() {
		var dispatcher, name, driver string
		if err = rows.Scan(&dispatcher, &name, &driver); err != nil {
			rows.Close()
			return result, err
		}
		if !slices.Contains(ids, dispatcher) {
			ids = append(ids, dispatcher)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	var saved []string
	var version int64
	err = tx.QueryRow(ctx, `SELECT dispatcher_ids,version FROM driver_board_dispatcher_order WHERE id=true`).Scan(&saved, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	ids = pinUnassigned(boardOrder(ids, saved))
	if a.DispatcherID == "" || a.TargetDispatcherID == "" || boardOrderRevision("dispatchers", version, ids) != a.Revision || a.DispatcherID == a.TargetDispatcherID || !slices.Contains(ids, a.DispatcherID) || !slices.Contains(ids, a.TargetDispatcherID) {
		return result, ErrDriverBoardOrderConflict
	}
	before := append([]string(nil), ids...)
	index := slices.Index(ids, a.DispatcherID)
	ids = slices.Delete(ids, index, index+1)
	target := slices.Index(ids, a.TargetDispatcherID)
	if a.After {
		target++
	}
	ids = slices.Insert(ids, target, a.DispatcherID)
	if !slices.Equal(before, ids) {
		err = tx.QueryRow(ctx, `INSERT INTO driver_board_dispatcher_order(id,dispatcher_ids,updated_by)
 VALUES(true,$1,NULLIF($2,'')::uuid)
 ON CONFLICT(id) DO UPDATE SET dispatcher_ids=EXCLUDED.dispatcher_ids,version=driver_board_dispatcher_order.version+1,updated_by=EXCLUDED.updated_by,updated_at=now()
 RETURNING version`, ids, actor).Scan(&version)
		if err != nil {
			return result, err
		}
	}
	result.DispatcherIDs = ids
	result.Revision = boardOrderRevision("dispatchers", version, ids)
	return result, tx.Commit(ctx)
}
