package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
// one database connection per browser. PostgreSQL notifications contain no record
// data; authorized subscribers receive shared snapshots over their HTTP streams.
type taskEvents struct {
	mu              sync.Mutex
	clients         map[chan struct{}]struct{}
	cancel          context.CancelFunc
	config          *pgx.ConnConfig
	logger          *slog.Logger
	stopped         chan struct{}
	channel         string
	permission      string
	revision        uint64
	snapshot        func(context.Context) ([]byte, error)
	snapshotEvent   string
	snapshotKey     func(context.Context) string
	snapshotMu      sync.Mutex
	snapshots       map[string]eventSnapshot
	shutdownContext context.Context
	stopSnapshots   context.CancelFunc
}

type eventSnapshot struct {
	revision uint64
	body     []byte
	at       time.Time
}

func newTaskEvents(pool *pgxpool.Pool, logger *slog.Logger) *taskEvents {
	return newDatabaseEvents(pool, logger, "mserp_tasks", "tasks.read")
}

func newDatabaseEvents(pool *pgxpool.Pool, logger *slog.Logger, channel, permission string) *taskEvents {
	h := &taskEvents{clients: map[chan struct{}]struct{}{}, logger: logger, stopped: make(chan struct{}), channel: channel, permission: permission}
	h.shutdownContext, h.stopSnapshots = context.WithCancel(context.Background())
	if pool != nil {
		h.config = pool.Config().ConnConfig.Copy()
	}
	return h
}
func (h *taskEvents) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.stopped:
		return
	default:
		close(h.stopped)
	}
	if h.cancel != nil {
		h.cancel()
	}
	h.stopSnapshots()
}

func (h *taskEvents) subscribe() (chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan struct{}, 1)
	select {
	case <-h.stopped:
		return ch, func() {}
	default:
	}
	h.clients[ch] = struct{}{}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		go h.listen(ctx)
	}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, exists := h.clients[ch]; !exists {
			return
		}
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
	h.revision++
	for ch := range h.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Share a committed snapshot among subscribers to the same notification. The
// session is checked separately for each subscriber before any frame is sent.
func (h *taskEvents) frame(ctx context.Context, fallback string) string {
	if h.snapshot == nil {
		return "event: " + fallback + "\ndata: {}\n\n"
	}
	h.snapshotMu.Lock()
	defer h.snapshotMu.Unlock()
	h.mu.Lock()
	revision := h.revision
	h.mu.Unlock()
	key := ""
	if h.snapshotKey != nil {
		key = h.snapshotKey(ctx)
	}
	cached := h.snapshots[key]
	if cached.body == nil || cached.revision != revision || time.Since(cached.at) > time.Second {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		stop := context.AfterFunc(h.shutdownContext, cancel)
		body, err := h.snapshot(ctx)
		stop()
		cancel()
		if err != nil {
			return "event: " + fallback + "\ndata: {}\n\n"
		}
		if h.snapshots == nil || (len(h.snapshots) >= 16 && cached.body == nil) {
			h.snapshots = map[string]eventSnapshot{}
		}
		cached = eventSnapshot{revision, body, time.Now()}
		h.snapshots[key] = cached
	}
	body := cached.body
	if h.snapshotEvent == "board" || h.snapshotEvent == "grossBoard" {
		// Project per subscriber, never cache a user's projected response.
		var value any
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return "event: " + fallback + "\ndata: {}\n\n"
		}
		session, _ := authSessionFromContext(ctx)
		path := "/driver-board/events"
		if h.snapshotEvent == "grossBoard" {
			path = "/gross-board/events"
		}
		projectFields(value, session.User, path)
		body, _ = json.Marshal(value)
	}
	return "event: " + h.snapshotEvent + "\ndata: " + string(body) + "\n\n"
}
func (h *taskEvents) listen(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		err := h.receive(ctx)
		if ctx.Err() != nil {
			return
		}
		h.logger.Warn("database notifications reconnecting", "channel", h.channel, "error", err)
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
	if _, err = conn.Exec(ctx, "LISTEN "+pgx.Identifier{h.channel}.Sanitize()); err != nil {
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

func (h *taskEvents) serve(auth *authHandler, presence ...*taskEvents) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			return
		}
		changes, unsubscribe := h.subscribe()
		defer unsubscribe()
		var presenceChanges chan struct{}
		if len(presence) > 0 {
			var closePresence func()
			presenceChanges, closePresence = presence[0].subscribe()
			defer closePresence()
		}
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
		if !send("retry: 5000\n" + h.frame(r.Context(), "changed")) {
			return
		}
		if presenceChanges != nil && !send(presence[0].frame(r.Context(), "presence")) {
			return
		}
		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()
		lifetime := time.NewTimer(10 * time.Minute)
		defer lifetime.Stop()
		for {
			changed, presenceChanged := false, false
			select {
			case <-r.Context().Done():
				return
			case <-h.stopped:
				return
			case <-lifetime.C:
				return
			case <-heartbeat.C:
			case <-changes:
				changed = true
			case <-presenceChanges:
				presenceChanged = true
			}
			// Role changes, disabled accounts and revoked/expired sessions take effect
			// on this connection too. Every data fetch also performs normal authorization.
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			session, err := auth.store.FindSessionByTokenHash(ctx, hashToken(cookie.Value))
			cancel()
			if err != nil && !errors.Is(err, repository.ErrAuthRecordNotFound) {
				return
			}
			if err != nil || !slices.Contains(session.User.Permissions, h.permission) {
				send("event: unauthorized\ndata: {}\n\n")
				return
			}
			if changed {
				if !send(h.frame(context.WithValue(r.Context(), authContextKey{}, session), "changed")) {
					return
				}
			} else if presenceChanged {
				if !send(presence[0].frame(r.Context(), "presence")) {
					return
				}
			} else if !send(": keepalive\n\n") {
				return
			}
		}
	}
}
