package httpapi

import (
	"context"
	"mserp/internal/db"
	"sync"
	"time"
)

const boardListenerIdleGrace = 30 * time.Second

// Full-fleet reads compete for CPU and the API's ten PostgreSQL connections.
// Limit concurrent HTTP and live board builds before either acquires a connection.
// Edits, load drawers, presence and financial commands never wait on this channel.
var boardReadSlots = make(chan struct{}, 2)

func acquireBoardRead(ctx context.Context) (func(), error) {
	end := db.Phase(ctx, "board_read_admission")
	defer end()
	select {
	case boardReadSlots <- struct{}{}:
		return func() { <-boardReadSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Only callers present before the SQL read begins may share its snapshot.
type boardReadKey struct {
	week   time.Time
	viewer string
	admin  bool
}

type boardSnapshotReads[T any] struct {
	mu      sync.Mutex
	pending map[boardReadKey]*boardRead[T]
}

type boardRead[T any] struct {
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	waiters int
	board   T
	err     error
}

func (r *boardSnapshotReads[T]) get(ctx context.Context, key boardReadKey, read func(context.Context, time.Time) (T, error)) (T, error) {
	r.mu.Lock()
	if r.pending == nil {
		r.pending = make(map[boardReadKey]*boardRead[T])
	}
	batch := r.pending[key]
	if batch == nil || batch.ctx.Err() != nil {
		workCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		batch = &boardRead[T]{done: make(chan struct{}), ctx: workCtx, cancel: cancel}
		r.pending[key] = batch
		go r.run(key, batch, read)
	}
	batch.waiters++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		batch.waiters--
		if batch.waiters == 0 {
			batch.cancel()
		}
	}()
	select {
	case <-ctx.Done():
		var empty T
		return empty, ctx.Err()
	case <-batch.done:
		return batch.board, batch.err
	}
}

func (r *boardSnapshotReads[T]) run(key boardReadKey, batch *boardRead[T], read func(context.Context, time.Time) (T, error)) {
	// Coalesce callers until a slot is available. A request arriving after
	// the read starts belongs to a newer batch and observes later commits.
	timer := time.NewTimer(2 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-batch.ctx.Done():
	}
	release, err := acquireBoardRead(batch.ctx)
	if err == nil {
		defer release()
	}
	r.mu.Lock()
	if r.pending[key] == batch {
		delete(r.pending, key)
	}
	r.mu.Unlock()
	if err == nil {
		err = batch.ctx.Err()
	}
	if err != nil {
		batch.err = err
	} else {
		batch.board, batch.err = read(batch.ctx, key.week)
	}
	close(batch.done)
	batch.cancel()
}
