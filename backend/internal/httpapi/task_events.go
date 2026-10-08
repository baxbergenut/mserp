package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mserp/internal/repository"
)

// One dedicated LISTEN connection per API process with active subscribers, not
// one database connection per browser. Notifications contain no task data.
type taskEvents struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
	cancel  context.CancelFunc
	config  *pgx.ConnConfig
	logger  *slog.Logger
}

func newTaskEvents(pool *pgxpool.Pool, logger *slog.Logger) *taskEvents {
	h := &taskEvents{clients: map[chan struct{}]struct{}{}, logger: logger}
	if pool != nil {
		h.config = pool.Config().ConnConfig.Copy()
	}
	return h
}
func (h *taskEvents) subscribe() (chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan struct{}, 1)
	h.clients[ch] = struct{}{}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		go h.listen(ctx)
	}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.clients, ch)
		if len(h.clients) == 0 {
			h.cancel()
			h.cancel = nil
		}
	}
}
func (h *taskEvents) publish() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (h *taskEvents) listen(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		err := h.receive(ctx)
		if ctx.Err() != nil {
			return
		}
		h.logger.Warn("task notifications reconnecting", "error", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}
func (h *taskEvents) receive(ctx context.Context) error {
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := pgx.ConnectConfig(connectCtx, h.config.Copy())
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = conn.Close(closeCtx)
	}()
	if _, err = conn.Exec(ctx, "LISTEN mserp_tasks"); err != nil {
		return err
	}
	// Reconcile anything missed during a connection interruption.
	h.publish()
	for {
		if _, err = conn.WaitForNotification(ctx); err != nil {
			return err
		}
		h.publish()
	}
}

func (h *taskEvents) serve(auth *authHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			return
		}
		changes, unsubscribe := h.subscribe()
		defer unsubscribe()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		controller := http.NewResponseController(w)
		send := func(body string) bool {
			if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return false
			}
			if _, err := fmt.Fprint(w, body); err != nil {
				return false
			}
			if err := controller.Flush(); err != nil {
				return false
			}
			// The idle gap is longer than a bounded frame write. Clear its deadline
			// before waiting: an expired HTTP write deadline cannot be extended.
			return controller.SetWriteDeadline(time.Time{}) == nil
		}
		if !send("retry: 5000\nevent: changed\ndata: {}\n\n") {
			return
		}
		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()
		lifetime := time.NewTimer(10 * time.Minute)
		defer lifetime.Stop()
		for {
			changed := false
			select {
			case <-r.Context().Done():
				return
			case <-lifetime.C:
				return
			case <-heartbeat.C:
			case <-changes:
				changed = true
			}
			// Role changes, disabled accounts and revoked/expired sessions take effect
			// on this connection too. Every data fetch also performs normal authorization.
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			session, err := auth.store.FindSessionByTokenHash(ctx, hashToken(cookie.Value))
			cancel()
			if err != nil && !errors.Is(err, repository.ErrAuthRecordNotFound) {
				return
			}
			if err != nil || !slices.Contains(session.User.Permissions, "tasks.read") {
				send("event: unauthorized\ndata: {}\n\n")
				return
			}
			if changed {
				if !send("event: changed\ndata: {}\n\n") {
					return
				}
			} else if !send(": keepalive\n\n") {
				return
			}
		}
	}
}
