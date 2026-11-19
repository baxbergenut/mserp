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
	week := time.Now()
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
	launch := func() { go func() { value, _ := reads.get(ctx, week, read); results <- value }() }
	launch()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for i := 0; i < 4; i++ {
		launch()
	}
	for {
		reads.mu.Lock()
		tail := reads.tail[week]
		queued := tail != nil && !tail.started && tail.waiters == 4
		reads.mu.Unlock()
		if queued {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("followers never queued")
		case <-time.After(time.Millisecond):
		}
	}
	close(gate)
	counts := map[int]int{}
	for i := 0; i < 5; i++ {
		select {
		case n := <-results:
			counts[n]++
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if counts[1] != 1 || counts[2] != 4 || calls.Load() != 2 {
		t.Fatalf("unexpected snapshot groups: %v", counts)
	}
}
