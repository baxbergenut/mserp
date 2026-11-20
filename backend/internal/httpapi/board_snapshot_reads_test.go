package httpapi

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoardReadsFollowCommit(t *testing.T) {
	var reads boardSnapshotReads[int]
	var calls atomic.Int32
	gate := make(chan struct{})
	entered := make(chan struct{})
	key := time.Now()
	read := func(ctx context.Context, _ time.Time) (int, error) {
		n := int(calls.Add(1))
		if n == 1 {
			close(entered)
			select {
			case <-gate:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		return n, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results := make(chan int, 5)
	launch := func() { go func() { value, _ := reads.get(ctx, key, read); results <- value }() }
	launch()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for i := 0; i < 4; i++ {
		launch()
	}
	// The next snapshot must complete even while the original SQL is blocked.
	select {
	case n := <-results:
		if n == 1 {
			t.Fatal("late caller joined an earlier snapshot")
		}
	case <-ctx.Done():
		t.Fatal("held behind prior snapshot")
	}
	close(gate)
	counts := map[int]int{}
	for i := 0; i < 4; i++ {
		select {
		case n := <-results:
			counts[n]++
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if counts[1] != 1 || calls.Load() < 2 {
		t.Fatalf("unexpected snapshot groups: %v", counts)
	}
}

func TestBoardReadsCoalesceQueuedWeeks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Hold all slots to force requests from both transports into the queue.
	first, err := acquireBoardRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := acquireBoardRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondHeld := true
	defer func() {
		if secondHeld {
			second()
		}
	}()

	var reads boardSnapshotReads[time.Time]
	var calls atomic.Int32
	week := time.Now().UTC()
	next := week.AddDate(0, 0, 7)
	results := make(chan bool, 5)
	for _, w := range []time.Time{week, week, next, week, next} {
		go func() {
			got, err := reads.get(ctx, w, func(_ context.Context, target time.Time) (time.Time, error) {
				calls.Add(1)
				return target, nil
			})
			results <- err == nil && got == w
		}()
	}
	for {
		reads.mu.Lock()
		a, b := reads.pending[week], reads.pending[next]
		queued := a != nil && a.waiters == 3 && b != nil && b.waiters == 2
		reads.mu.Unlock()
		if queued {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	second()
	secondHeld = false
	for range 5 {
		select {
		case ok := <-results:
			if !ok {
				t.Fatal("wrong board week")
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("five reads ran %d builds for two weeks", calls.Load())
	}
}
