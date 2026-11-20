package repository

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type boardWeekKeys struct {
	entries  string
	openings string
	queues   string
}

// The queue cache is independent of weekly load revisions. A missing counter,
// resource-scoped writes or incomplete invalidation triggers require a direct
// read. Trigger versions also distinguish a disable/enable DDL cycle.
const boardQueueRevisionSQL = `coalesce((
 SELECT jsonb_build_array(r.tableoid,t.oid,r.revision,r.xmin::text,t.trigger_versions,fleet_status_enabled())::text
 FROM payroll_cache_revisions r CROSS JOIN (
  SELECT tgrelid AS oid, array_agg(xmin::text ORDER BY tgname) AS trigger_versions
  FROM pg_trigger WHERE tgrelid=to_regclass('driver_board_load_state')
   AND tgfoid=to_regprocedure('invalidate_payroll_cache()') AND tgenabled IN ('O','A')
   AND tgqual IS NULL AND tgattr=''::int2vector
   AND (tgname,tgtype::int) IN (('payroll_cache_insert',4),('payroll_cache_update',16),
                                ('payroll_cache_delete',8),('payroll_cache_truncate',32))
  GROUP BY tgrelid HAVING count(*)=4
 ) t WHERE r.table_name='driver_board_load_state' AND NOT payroll_resource_table(r.table_name)
   AND (SELECT relnamespace FROM pg_class WHERE oid=r.tableoid)=(SELECT relnamespace FROM pg_class WHERE oid=t.oid)
),'')`

// The key and the data must be read in the same read-only repeatable snapshot.
// The four source tables advance their revisions on INSERT, UPDATE, DELETE and
// TRUNCATE. Availability and reset records have independent write paths.
func boardWeekRevision(ctx context.Context, tx pgx.Tx, week time.Time, openings bool) (boardWeekKeys, error) {
	// Some callers run against a partial schema or transaction-local board
	// tables. A counter for a shadowed table cannot certify those rows.
	var available bool
	if err := tx.QueryRow(ctx, `SELECT set_config('jit','off',true),set_config('plan_cache_mode','force_custom_plan',true),
 to_regclass('payroll_cache_revisions') IS NOT NULL AND to_regprocedure('payroll_resource_table(text)') IS NOT NULL AND
 (SELECT count(*)=16 FROM pg_trigger WHERE tgrelid IN ('gross_board_entries'::regclass,'gross_board_extra_entries'::regclass,'gross_board_transfers'::regclass,'loads'::regclass)
 AND tgname IN ('payroll_cache_insert','payroll_cache_update','payroll_cache_delete','payroll_cache_truncate')
 AND tgenabled IN ('O','A'))`).Scan(nil, nil, &available); err != nil || !available {
		return boardWeekKeys{}, err
	}
	var keys boardWeekKeys
	var resets string
	queueRevision := "''::text"
	if !openings {
		queueRevision = boardQueueRevisionSQL
	}
	err := tx.QueryRow(ctx, `SELECT CASE WHEN count(*)=4 AND NOT bool_or(payroll_resource_table(table_name)) THEN
 jsonb_build_array(current_schema(),$1::date,array_agg(revision ORDER BY table_name),
 (SELECT array_agg(load_id ORDER BY load_id) FROM load_source_availability WHERE NOT present))::text ELSE '' END,
 CASE WHEN $2 THEN coalesce((SELECT jsonb_agg(jsonb_build_array(id,driver_id,week_start,target,week_change) ORDER BY id)
 FROM gross_board_rate_resets WHERE week_start<=$1::date)::text,'') ELSE '' END, `+queueRevision+`
 FROM payroll_cache_revisions WHERE table_name IN ('gross_board_entries','gross_board_extra_entries','gross_board_transfers','loads')`, week, openings).Scan(&keys.entries, &resets, &keys.queues)
	if keys.entries != "" && openings {
		keys.openings = keys.entries + "\x00" + resets
	}
	return keys, err
}

// Each repository retains at most the last displayed week. Concurrent readers
// with the same committed inputs can share the SQL work. A canceled reader
// releases the other readers to use their own transactions.
type boardWeekCache[T any] struct {
	mu      sync.Mutex
	key     string
	data    T
	pending map[string]chan struct{}
}

func (c *boardWeekCache[T]) get(ctx context.Context, key string, clone func(T) T, read func() (T, error)) (T, error) {
	if c == nil || key == "" {
		return read()
	}
	for {
		c.mu.Lock()
		if c.key == key {
			data := c.data
			c.mu.Unlock()
			return clone(data), nil
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
		c.mu.Lock()
		if err == nil {
			c.key, c.data = key, result
		}
		delete(c.pending, key)
		close(done)
		c.mu.Unlock()
		if err != nil {
			return result, err
		}
		return clone(result), nil
	}
}

// All cached values are owned by their cache. HTTP, live streams and workers
// receive independent copies, including fields used internally for stop lookup.
func copyBoardPointer[T any](v *T) *T {
	if v == nil {
		return nil
	}
	value := *v
	return &value
}

func cloneBoardPlans(src map[string][]BoardLoad) map[string][]BoardLoad {
	out := make(map[string][]BoardLoad, len(src))
	for driver, plans := range src {
		rows := slices.Clone(plans)
		for i := range rows {
			p := &rows[i]
			p.LoadID = copyBoardPointer(p.LoadID)
			p.Stops = slices.Clone(p.Stops)
			for j := range p.Stops {
				s := &p.Stops[j]
				s.geocode = copyBoardPointer(s.geocode)
				s.Latitude = copyBoardPointer(s.Latitude)
				s.Longitude = copyBoardPointer(s.Longitude)
				s.Schedule = copyBoardPointer(s.Schedule)
			}
		}
		out[driver] = rows
	}
	return out
}

func cloneWeekEntries(src []GrossBoardEntry) []GrossBoardEntry {
	out := slices.Clone(src)
	for i := range out {
		e := &out[i]
		// readBoardWeekEntries scans saved display rows. Write intents
		// (Fields, PreviousLoadNumber and SplitReview) are never populated.
		e.LoadRecordID = copyBoardPointer(e.LoadRecordID)
		e.LoadTripID = copyBoardPointer(e.LoadTripID)
		e.Note = copyBoardPointer(e.Note)
		e.Trips = slices.Clone(e.Trips)
	}
	return out
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
