package datatruck

import (
	"context"
	"sync"
	"time"
)

type discoveryPriorityKey struct{}

// WithDiscoveryPriority lets new-load pages pass queued reconciliation requests.
// All requests, including retries and payroll refreshes, still share one budget.
func WithDiscoveryPriority(ctx context.Context) context.Context {
	return context.WithValue(ctx, discoveryPriorityKey{}, true)
}

type requestWaiter struct {
	ready   chan struct{}
	high    bool
	granted bool
}

// requestGate serializes HTTP attempts and spaces their starts. No tokens build
// up while idle, so a wakeup cannot burst through DataTruck's minute allowance.
type requestGate struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
	busy     bool
	waiters  []*requestWaiter
	timer    *time.Timer
}

func (g *requestGate) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := &requestWaiter{ready: make(chan struct{})}
	w.high, _ = ctx.Value(discoveryPriorityKey{}).(bool)
	g.mu.Lock()
	g.waiters = append(g.waiters, w)
	g.dispatch()
	g.mu.Unlock()
	select {
	case <-w.ready:
		return g.release, nil
	case <-ctx.Done():
		g.mu.Lock()
		if w.granted {
			g.busy = false
		} else {
			for i, pending := range g.waiters {
				if pending == w {
					g.waiters = append(g.waiters[:i], g.waiters[i+1:]...)
					break
				}
			}
		}
		g.dispatch()
		g.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (g *requestGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.busy = false
	g.dispatch()
}

func (g *requestGate) cooldown(delay time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if until := time.Now().Add(delay); until.After(g.next) {
		g.next = until
	}
}

// Caller holds mu. At most one timer or HTTP attempt is active.
func (g *requestGate) dispatch() {
	if g.busy || g.timer != nil || len(g.waiters) == 0 {
		return
	}
	if delay := time.Until(g.next); delay > 0 {
		g.timer = time.AfterFunc(delay, func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.timer = nil
			g.dispatch()
		})
		return
	}
	i := 0
	for n, w := range g.waiters {
		if w.high {
			i = n
			break
		}
	}
	w := g.waiters[i]
	g.waiters = append(g.waiters[:i], g.waiters[i+1:]...)
	g.busy, w.granted = true, true
	g.next = time.Now().Add(g.interval)
	close(w.ready)
}
