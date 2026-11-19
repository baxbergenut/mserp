package httpapi

import (
	"context"
	"sync"
	"time"
)

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
	// Hold no database connection during this short window. A request arriving
	// during an expensive query can start the next read without waiting for it.
	timer := time.NewTimer(2 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-batch.ctx.Done():
	}
	r.mu.Lock()
	if r.pending[key] == batch {
		delete(r.pending, key)
	}
	r.mu.Unlock()
	if err := batch.ctx.Err(); err != nil {
		batch.err = err
	} else {
		batch.board, batch.err = read(batch.ctx, key.week)
	}
	close(batch.done)
	batch.cancel()
}
