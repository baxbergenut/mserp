package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"sync"
	"time"
)

type payrollCacheEntry struct {
	body    []byte
	expires time.Time
}
type payrollCache struct {
	mu       sync.Mutex
	entries  map[string]payrollCacheEntry
	inFlight map[string]chan struct{}
	bytes    int
}

// A PostgreSQL MVCC snapshot is a conservative database-wide dependency token.
// Equal tokens see the same committed transactions, including transactions that
// were in flight during the earlier fill. This avoids a fragile list of write
// hooks, a contended global revision row, and stale historical carry. Only public
// read-only entry points use this cache; writers must see their own new writes.
func (r *DriverPayRepository) cachedWeek(ctx context.Context, tx pgx.Tx, kind string, week time.Time, read func() (DriverPayWeek, error)) (DriverPayWeek, error) {
	var snapshot string
	if err := tx.QueryRow(ctx, `SELECT pg_current_snapshot()::text`).Scan(&snapshot); err != nil {
		return DriverPayWeek{}, err
	}
	key := kind + ":" + week.Format(time.DateOnly) + ":" + ChargeCurrentWeek() + ":" + snapshot
	now := time.Now()
	c := &r.cache
	for {
		c.mu.Lock()
		hit, ok := c.entries[key]
		if ok && time.Now().Before(hit.expires) {
			c.mu.Unlock()
			var report DriverPayWeek
			if err := json.Unmarshal(hit.body, &report); err != nil {
				return DriverPayWeek{}, err
			}
			return report, nil
		}
		if flight := c.inFlight[key]; flight != nil {
			c.mu.Unlock()
			select {
			case <-flight:
				continue
			case <-ctx.Done():
				return DriverPayWeek{}, ctx.Err()
			}
		}
		if c.inFlight == nil {
			c.inFlight = map[string]chan struct{}{}
		}
		c.inFlight[key] = make(chan struct{})
		c.mu.Unlock()
		break
	}
	defer func() { c.mu.Lock(); close(c.inFlight[key]); delete(c.inFlight, key); c.mu.Unlock() }()
	report, err := read()
	if err != nil {
		return report, err
	}
	body, err := json.Marshal(report)
	if err != nil {
		return report, err
	}
	if len(body) > 2*1024*1024 {
		return report, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]payrollCacheEntry{}
	}
	for k, v := range c.entries {
		if !now.Before(v.expires) {
			delete(c.entries, k)
			c.bytes -= len(v.body)
		}
	}
	if len(c.entries) >= 32 || c.bytes+len(body) > 8*1024*1024 {
		clear(c.entries)
		c.bytes = 0
	}
	if old, exists := c.entries[key]; exists {
		c.bytes -= len(old.body)
	}
	c.entries[key] = payrollCacheEntry{body: body, expires: now.Add(30 * time.Second)}
	c.bytes += len(body)
	return report, nil
}

func selectPayDriver(report DriverPayWeek, id string) DriverPayWeek {
	if id == "" {
		return report
	}
	selected := []DriverPayDriver{}
	for _, d := range report.Drivers {
		if d.ID == id {
			selected = append(selected, d)
		}
	}
	report.Drivers = selected
	report.Revision = payrollRevision(report)
	return report
}
