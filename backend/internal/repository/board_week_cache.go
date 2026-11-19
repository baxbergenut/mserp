package repository

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// The key and the data must be read in the same read-only repeatable snapshot.
// The four source tables advance their revisions on INSERT, UPDATE, DELETE and
// TRUNCATE. Availability and reset records have independent write paths.
func boardWeekRevision(ctx context.Context, tx pgx.Tx, week time.Time, openings bool) (string, error) {
	var key string
	err := tx.QueryRow(ctx, `SELECT CASE WHEN count(*)=4 AND NOT bool_or(payroll_resource_table(table_name)) THEN
 jsonb_build_array(current_schema(),$1::date,array_agg(revision ORDER BY table_name),
 (SELECT array_agg(load_id ORDER BY load_id) FROM load_source_availability WHERE NOT present),
 CASE WHEN $2 THEN (SELECT jsonb_agg(jsonb_build_array(id,driver_id,week_start,target,week_change) ORDER BY id)
 FROM gross_board_rate_resets WHERE week_start<=$1::date) END)::text ELSE '' END
 FROM payroll_cache_revisions WHERE table_name IN ('gross_board_entries','gross_board_extra_entries','gross_board_transfers','loads')`, week, openings).Scan(&key)
	return key, err
}

// Each repository retains at most the last displayed week. Concurrent readers
// with the same committed inputs can share the SQL work. A canceled reader
// releases the other readers to use their own transactions.
type boardWeekCache[T any] struct {
	mu      sync.Mutex
	key     string
	data    []byte
	pending map[string]chan struct{}
}

func (c *boardWeekCache[T]) get(ctx context.Context, key string, read func() (T, error)) (T, error) {
	if c == nil || key == "" {
		return read()
	}
	for {
		c.mu.Lock()
		if c.key == key {
			data := c.data
			c.mu.Unlock()
			var result T
			err := json.Unmarshal(data, &result)
			return result, err
		}
		if done := c.pending[key]; done != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				var zero T
				return zero, ctx.Err()
			case <-done:
			}
			continue
		}
		if c.pending == nil {
			c.pending = make(map[string]chan struct{})
		}
		done := make(chan struct{})
		c.pending[key] = done
		c.mu.Unlock()
		result, err := read()
		var data []byte
		if err == nil {
			data, err = json.Marshal(result)
		}
		c.mu.Lock()
		if err == nil {
			c.key, c.data = key, data
		}
		delete(c.pending, key)
		close(done)
		c.mu.Unlock()
		return result, err
	}
}

func readBoardWeekEntries(ctx context.Context, tx pgx.Tx, week time.Time) ([]GrossBoardEntry, error) {
	rows, err := tx.Query(ctx, driverBoardCurrentEntries+"SELECT "+grossBoardEntryColumns+` FROM effective ORDER BY driver_id,service_date,slot`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []GrossBoardEntry{}
	for rows.Next() {
		var e GrossBoardEntry
		if err := scanGrossBoardEntry(rows, &e); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
