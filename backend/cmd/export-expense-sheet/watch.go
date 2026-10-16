package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// LISTEN first, then drain: saves racing startup are either in the durable
// backlog or buffered notifications. Lost notifications never lose an entry.
func watchExports(ctx context.Context, dsn string, retry time.Duration, deliver func(context.Context) error) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return errors.New("cannot connect expense sheet notification listener")
	}
	defer conn.Close(context.Background())
	if _, err = conn.Exec(ctx, "LISTEN mserp_expense_sheet_export"); err != nil {
		return err
	}
	log.Print("expense sheet exporter listening for committed entries")
	for ctx.Err() == nil {
		batchCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		err = deliver(batchCtx)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Printf("expense sheet delivery retained for retry: %v", err)
			// Incoming saves must not bypass outage backoff. Their durable entries
			// are included on the next attempt, even if notifications are lost.
			timer := time.NewTimer(retry)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
			continue
		} else {
			var pending bool
			if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expense_sheet_exports WHERE delivered_at IS NULL) AND (SELECT enabled FROM expense_sheet_export_config WHERE singleton)`).Scan(&pending); err != nil {
				return errors.New("expense sheet listener lost its database connection")
			}
			if pending {
				continue
			}
		}
		waitCtx, waitCancel := context.WithTimeout(ctx, retry)
		_, err = conn.WaitForNotification(waitCtx)
		waitCancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			// systemd reconnects; the next startup drains the durable queue.
			return errors.New("expense sheet notification connection lost")
		}
	}
	return nil
}

// Share pacing across batches and both Sheets read/write requests. Fifty total
// requests/minute leaves headroom under the per-user quotas during bulk entry.
var sheetRequests = requestGate{interval: 1200 * time.Millisecond}

type requestGate struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

type pacedTransport struct {
	base http.RoundTripper
	gate *requestGate
}

func (t *pacedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.gate.mu.Lock()
	at := t.gate.next
	if now := time.Now(); now.After(at) {
		at = now
	}
	t.gate.next = at.Add(t.gate.interval)
	t.gate.mu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-timer.C:
		return t.base.RoundTrip(req)
	}
}
