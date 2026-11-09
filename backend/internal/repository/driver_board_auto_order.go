package repository

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
)

func lockBoardOrder(ctx context.Context, tx pgx.Tx, dispatcher string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('status-board-order:'||$1,0))`, dispatcher)
	return err
}

// Lock current members against termination and reassignment. New arrivals
// absent from the saved order append on the next read.
func readBoardOrderGroup(ctx context.Context, tx pgx.Tx, dispatcher string) ([]string, int64, bool, error) {
	rows, err := tx.Query(ctx, `SELECT d.id FROM drivers d WHERE d.active
 AND payroll_driver_at(d.id,(now() AT TIME ZONE 'America/New_York')::date)=d.id
 AND coalesce(d.dispatcher_id::text,'')=$1 ORDER BY d.full_name,d.id FOR UPDATE OF d`, dispatcher)
	if err != nil {
		return nil, 0, false, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, 0, false, err
	}
	var saved []string
	var version int64
	var autoOrder bool
	err = tx.QueryRow(ctx, `SELECT driver_ids,version,auto_order FROM driver_board_driver_order WHERE dispatcher_key=$1`, dispatcher).Scan(&saved, &version, &autoOrder)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, false, err
	}
	return boardOrder(ids, saved), version, autoOrder, nil
}

func boardAutoOrderRank(status string) int {
	switch status {
	case "READY":
		return 0
	case "DISPATCHED":
		return 1
	case "ENROUTE":
		return 2
	case "RESERVED":
		return 3
	case "REST":
		return 4
	case "HOME":
		return 5
	default:
		return 6
	}
}

func sortBoardOrderByStatus(ids []string, statuses map[string]string) {
	slices.SortStableFunc(ids, func(a, b string) int {
		return cmp.Compare(boardAutoOrderRank(statuses[a]), boardAutoOrderRank(statuses[b]))
	})
}

// Disabling Auto-order persists the same complete status ordering the board
// would currently display. The flag and the manual baseline change together.
func (r *DriverBoardRepository) SetDriverAutoOrder(ctx context.Context, dispatcher string, enabled bool, actor string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockBoardOrder(ctx, tx, dispatcher); err != nil {
		return err
	}
	if dispatcher != "" {
		var id string
		err = tx.QueryRow(ctx, `SELECT id FROM dispatchers WHERE id=$1 AND active FOR SHARE`, dispatcher).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDriverBoardOrderConflict
		}
		if err != nil {
			return err
		}
	}
	ids, _, wasEnabled, err := readBoardOrderGroup(ctx, tx, dispatcher)
	if err != nil {
		return err
	}
	if enabled == wasEnabled {
		return tx.Commit(ctx)
	}
	if !enabled && len(ids) > 0 {
		statuses, err := r.readBoardOrderStatuses(ctx, tx, ids)
		if err != nil {
			return err
		}
		sortBoardOrderByStatus(ids, statuses)
	}
	_, err = tx.Exec(ctx, `INSERT INTO driver_board_driver_order(dispatcher_key,dispatcher_id,driver_ids,auto_order,updated_by)
 VALUES($1,NULLIF($1,'')::uuid,$2,$3,NULLIF($4,'')::uuid)
 ON CONFLICT(dispatcher_key) DO UPDATE SET driver_ids=EXCLUDED.driver_ids,auto_order=EXCLUDED.auto_order,
 version=driver_board_driver_order.version+1,updated_by=EXCLUDED.updated_by,updated_at=now()`, dispatcher, ids, enabled, actor)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *DriverBoardRepository) readBoardOrderStatuses(ctx context.Context, tx pgx.Tx, ids []string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT driver_id,status FROM driver_board WHERE driver_id=ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := make(map[string]string, len(ids))
	var progressIDs []string
	for rows.Next() {
		var id, status string
		if err = rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		statuses[id] = status
		if status == "ENROUTE" || status == "RESERVED" {
			progressIDs = append(progressIDs, id)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(progressIDs) == 0 {
		return statuses, nil
	}
	var statusSync bool
	if err = tx.QueryRow(ctx, `SELECT fleet_status_enabled()`).Scan(&statusSync); err != nil || statusSync {
		return statuses, err
	}
	loads, err := readBoardLoads(ctx, tx, progressIDs, r.loadStartDate(""), r.now())
	if err != nil {
		return nil, err
	}
	for _, id := range progressIDs {
		statuses[id] = boardProgressStatus(statuses[id], loads[id].Next)
	}
	return statuses, nil
}
