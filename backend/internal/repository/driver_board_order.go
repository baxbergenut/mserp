package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
)

var ErrDriverBoardOrderConflict = errors.New("driver order or dispatcher roster changed; reload the board and try again")

type DriverBoardOrderAction struct {
	TargetDispatcherID *string `json:"targetDispatcherId,omitempty"`
	TargetRevision     string  `json:"targetRevision,omitempty"`
	DispatcherID       string  `json:"dispatcherId"`
	DriverID           string  `json:"driverId"`
	TargetDriverID     string  `json:"targetDriverId"`
	After              bool    `json:"after"`
	Revision           string  `json:"revision"`
}

type DriverBoardOrder struct {
	SourceRevision string   `json:"sourceRevision,omitempty"`
	DispatcherID   string   `json:"dispatcherId"`
	DriverIDs      []string `json:"driverIds"`
	Revision       string   `json:"revision"`
}

func boardOrder(ids, saved []string) []string {
	remaining := make(map[string]bool, len(ids))
	for _, id := range ids {
		remaining[id] = true
	}
	result := make([]string, 0, len(ids))
	for _, id := range saved {
		if remaining[id] {
			result = append(result, id)
			delete(remaining, id)
		}
	}
	for _, id := range ids {
		if remaining[id] {
			result = append(result, id)
		}
	}
	return result
}

func boardOrderRevision(dispatcher string, version int64, ids []string) string {
	body, _ := json.Marshal([]any{dispatcher, version, ids})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func applyBoardDriverOrder(ctx context.Context, tx pgx.Tx, board *DriverBoard) error {
	groups := map[string][]string{"": {}}
	for _, d := range board.Drivers {
		groups[d.DispatcherID] = append(groups[d.DispatcherID], d.ID)
	}
	versions := map[string]int64{}
	rows, err := tx.Query(ctx, `SELECT dispatcher_key,driver_ids,version FROM driver_board_driver_order`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var key string
		var ids []string
		var version int64
		if err = rows.Scan(&key, &ids, &version); err != nil {
			rows.Close()
			return err
		}
		if current, ok := groups[key]; ok {
			groups[key] = boardOrder(current, ids)
			versions[key] = version
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	board.DriverOrderRevisions = map[string]string{}
	ranks := map[string]int{}
	for key, ids := range groups {
		board.DriverOrderRevisions[key] = boardOrderRevision(key, versions[key], ids)
		for rank, id := range ids {
			ranks[id] = rank
		}
	}
	// Stable group boundaries retain the dispatcher sorting of the source query.
	for start := 0; start < len(board.Drivers); {
		end := start + 1
		for end < len(board.Drivers) && board.Drivers[end].DispatcherID == board.Drivers[start].DispatcherID {
			end++
		}
		group := board.Drivers[start:end]
		sort.SliceStable(group, func(i, j int) bool { return ranks[group[i].ID] < ranks[group[j].ID] })
		start = end
	}
	return nil
}

// Move relative to a visible row in the complete group, preserving filtered rows.
// The revision covers both the saved order and the current eligible roster.
func (r *DriverBoardRepository) ReorderDrivers(ctx context.Context, a DriverBoardOrderAction, actor string) (DriverBoardOrder, error) {
	result := DriverBoardOrder{DispatcherID: a.DispatcherID}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if a.TargetDispatcherID != nil && *a.TargetDispatcherID != a.DispatcherID {
		return r.transferBoardDriver(ctx, tx, a, actor)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('status-board-order:'||$1,0))`, a.DispatcherID); err != nil {
		return result, err
	}
	// Lock existing members against concurrent termination/reassignment. Newly
	// arriving members are safely appended on the next snapshot.
	rows, err := tx.Query(ctx, `SELECT d.id FROM drivers d WHERE d.active
 AND payroll_driver_at(d.id,(now() AT TIME ZONE 'America/New_York')::date)=d.id
 AND coalesce(d.dispatcher_id::text,'')=$1 ORDER BY d.full_name,d.id FOR UPDATE OF d`, a.DispatcherID)
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
	err = tx.QueryRow(ctx, `SELECT driver_ids,version FROM driver_board_driver_order WHERE dispatcher_key=$1`, a.DispatcherID).Scan(&saved, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	ids = boardOrder(ids, saved)
	if boardOrderRevision(a.DispatcherID, version, ids) != a.Revision || a.DriverID == a.TargetDriverID || !slices.Contains(ids, a.DriverID) || !slices.Contains(ids, a.TargetDriverID) {
		return result, ErrDriverBoardOrderConflict
	}
	before := append([]string(nil), ids...)
	index := slices.Index(ids, a.DriverID)
	ids = slices.Delete(ids, index, index+1)
	target := slices.Index(ids, a.TargetDriverID)
	if a.After {
		target++
	}
	ids = slices.Insert(ids, target, a.DriverID)
	if !slices.Equal(before, ids) {
		err = tx.QueryRow(ctx, `INSERT INTO driver_board_driver_order(dispatcher_key,dispatcher_id,driver_ids,updated_by)
 VALUES($1,NULLIF($1,'')::uuid,$2,NULLIF($3,'')::uuid)
 ON CONFLICT(dispatcher_key) DO UPDATE SET driver_ids=EXCLUDED.driver_ids,version=driver_board_driver_order.version+1,updated_by=EXCLUDED.updated_by,updated_at=now()
 RETURNING version`, a.DispatcherID, ids, actor).Scan(&version)
		if err != nil {
			return result, err
		}
	}
	result.DriverIDs = ids
	result.Revision = boardOrderRevision(a.DispatcherID, version, ids)
	return result, tx.Commit(ctx)
}
