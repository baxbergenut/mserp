package httpapi

import (
	"context"
	"sync"
	"time"
)

// Multiple tabs often request the same board after a commit. While a read is
// running, queue new callers for the next snapshot. A caller can never receive
// a snapshot that started before its request. Results live only for that batch.
type boardSnapshotReads[T any] struct {
	mu   sync.Mutex
	tail map[time.Time]*boardRead[T]
}

type boardRead[T any] struct {
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	waiters int
	board   T
	err     error
}

func (r *boardSnapshotReads[T]) get(ctx context.Context, week time.Time, read func(context.Context, time.Time) (T, error)) (T, error) {
	r.mu.Lock()
	if r.tail == nil {
		r.tail = make(map[time.Time]*boardRead[T])
	}
	batch := r.tail[week]
	if batch == nil || batch.started || batch.ctx.Err() != nil {
		previous := batch
		workCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		batch = &boardRead[T]{done: make(chan struct{}), ctx: workCtx, cancel: cancel}
		r.tail[week] = batch
		go r.run(week, batch, previous, read)
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

func (r *boardSnapshotReads[T]) run(week time.Time, batch, previous *boardRead[T], read func(context.Context, time.Time) (T, error)) {
	if previous != nil {
		<-previous.done
	}
	r.mu.Lock()
	batch.started = true
	r.mu.Unlock()
	if err := batch.ctx.Err(); err != nil {
		batch.err = err
	} else {
		batch.board, batch.err = read(batch.ctx, week)
	}
	r.mu.Lock()
	if r.tail[week] == batch {
		delete(r.tail, week)
	}
	close(batch.done)
	r.mu.Unlock()
	batch.cancel()
}
