package httpapi

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"mserp/internal/db"
)

// Pair an opening read with its already-authorized SSE subscription before any
// SQL snapshot starts. The two requests may arrive in either order.
type boardOpeningKey struct {
	viewer, path, week, id string
}
type boardOpenings struct {
	mu      sync.Mutex
	active  map[boardOpeningKey]int
	changed chan struct{}
}

func boardOpeningScope(r *http.Request) (boardOpeningKey, bool) {
	q := r.URL.Query()
	id := q.Get("opening")
	session, ok := authSessionFromContext(r.Context())
	if !ok || session.User.ID == "" || !isUUID(id) {
		return boardOpeningKey{}, false
	}
	path := strings.TrimSuffix(r.URL.Path, "/events")
	week := ""
	if path == "/gross-board" {
		week = q.Get("weekStart")
	} else if path != "/driver-board" {
		return boardOpeningKey{}, false
	}
	return boardOpeningKey{session.User.ID, path, week, id}, true
}

func (b *boardOpenings) register(key boardOpeningKey) func() {
	b.mu.Lock()
	if b.active == nil {
		b.active = make(map[boardOpeningKey]int)
	}
	b.active[key]++
	if b.changed != nil {
		close(b.changed)
		b.changed = nil
	}
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.active[key]--
		if b.active[key] == 0 {
			delete(b.active, key)
		}
	}
}

func (b *boardOpenings) wait(ctx context.Context, key boardOpeningKey, limit time.Duration) bool {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for {
		b.mu.Lock()
		if b.active[key] > 0 {
			b.mu.Unlock()
			return true
		}
		if b.changed == nil {
			b.changed = make(chan struct{})
		}
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-changed:
			// A stream registered. Check this viewer, tab and board again.
		}
	}
}

func (b *boardOpenings) admit(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("opening") == "" {
		return true
	}
	key, ok := boardOpeningScope(r)
	if b != nil && ok && b.wait(r.Context(), key, 150*time.Millisecond) {
		db.Event(r.Context(), "board_open_paired")
		return true
	}
	// No SQL board build. The browser uses its existing stream/fallback logic.
	db.Event(r.Context(), "board_open_fallback")
	w.WriteHeader(http.StatusNoContent)
	return false
}

func writeBoardSnapshot(w http.ResponseWriter, r *http.Request, value any) {
	if opening := r.URL.Query().Get("opening"); opening != "" {
		value = map[string]any{"opening": opening, "board": value}
	}
	writeJSON(w, http.StatusOK, value)
}
