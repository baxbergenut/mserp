package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrDriverBoardOrderLocal = errors.New("driver ordering is now personal to this browser; reload the page to reorder")

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
	// Keep legacy revisions solely for shared assignment concurrency checks.
	// Browser preferences own presentation; the source's alphabetical rows remain.
	for key, ids := range groups {
		board.DriverOrderRevisions[key] = boardOrderRevision(key, versions[key], ids)
	}
	return nil
}

// Only cross-dispatcher assignments are shared. Reject retired reorder commands
// before opening a transaction, including requests from older browser tabs.
func (r *DriverBoardRepository) ReorderDrivers(ctx context.Context, a DriverBoardOrderAction, actor string) (DriverBoardOrder, error) {
	if a.TargetDispatcherID == nil || *a.TargetDispatcherID == a.DispatcherID {
		return DriverBoardOrder{}, ErrDriverBoardOrderLocal
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return DriverBoardOrder{}, err
	}
	defer tx.Rollback(ctx)
	return r.transferBoardDriver(ctx, tx, a, actor)
}
