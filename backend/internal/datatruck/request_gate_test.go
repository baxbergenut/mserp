package datatruck

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func awaitQueued(t *testing.T, g *requestGate, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		n := len(g.waiters)
		g.mu.Unlock()
		if n == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("expected %d queued requests", count)
}

func TestGatePrioritizesDiscoveryAndCancelsWaiters(t *testing.T) {
	g := &requestGate{}
	release, err := g.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan string, 2)
	run := func(ctx context.Context, name string) {
		release, err := g.acquire(ctx)
		if err == nil {
			order <- name
			release()
		}
	}
	go run(context.Background(), "refresh")
	awaitQueued(t, g, 1)
	go run(WithDiscoveryPriority(context.Background()), "new")
	awaitQueued(t, g, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		release, err := g.acquire(ctx)
		if err == nil {
			release()
		}
		done <- err
	}()
	awaitQueued(t, g, 3)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	for _, want := range []string{"new", "refresh"} {
		select {
		case got := <-order:
			if got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("queue stalled")
		}
	}
}

func TestGateSpacesRequestsAndSharesCooldown(t *testing.T) {
	g := &requestGate{interval: 20 * time.Millisecond}
	release, err := g.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	g.cooldown(60 * time.Millisecond)
	start := time.Now()
	release()
	release, err = g.acquire(WithDiscoveryPriority(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("priority bypassed cooldown")
	}
	start = time.Now()
	release()
	release, err = g.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if time.Since(start) < 15*time.Millisecond {
		t.Fatal("requests were not paced")
	}
	if NewClient("test", "test").gate.interval != 4*time.Second {
		t.Fatal("production limit must be 15/minute")
	}
}

func TestRetryAfterLongerThanMinuteIsHonored(t *testing.T) {
	if got := retryDelay("120", 0); got != 2*time.Minute {
		t.Fatal(got)
	}
	if got := retryDelay(time.Now().Add(2*time.Minute).UTC().Format(http.TimeFormat), 0); got < 119*time.Second {
		t.Fatal(got)
	}
}
