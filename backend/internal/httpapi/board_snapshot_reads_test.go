package httpapi

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoardReadsFollowCommit(t *testing.T) {
	var reads boardSnapshotReads[int]
	var calls atomic.Int32
	gate := make(chan struct{})
	entered := make(chan struct{})
	key := boardReadKey{week: time.Now(), viewer: "one"}
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

func TestBoardReadsIsolateViewers(t *testing.T) {
	var reads boardSnapshotReads[string]
	week := time.Now()
	start := make(chan struct{})
	done := make(chan bool, 3)
	for _, key := range []boardReadKey{{week, "A", true}, {week, "A", false}, {week, "B", false}} {
		go func(key boardReadKey) {
			<-start
			want := fmt.Sprint(key.viewer, key.admin)
			got, err := reads.get(context.Background(), key, func(context.Context, time.Time) (string, error) { return want, nil })
			done <- err == nil && got == want
		}(key)
	}
	close(start)
	for range 3 {
		if !<-done {
			t.Fatal("board result crossed viewer or administrator scope")
		}
	}
}
