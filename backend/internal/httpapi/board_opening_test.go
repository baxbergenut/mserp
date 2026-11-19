package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"mserp/internal/repository"
)

func TestBoardOpeningRegistration(t *testing.T) {
	const id = "e4500000-0000-0000-0000-000000000002"
	scope := func(path, user string) boardOpeningKey {
		r := httptest.NewRequest("GET", path+"&opening="+id, nil)
		session := repository.AuthSession{User: repository.AuthUser{ID: user}}
		key, ok := boardOpeningScope(r.WithContext(context.WithValue(r.Context(), authContextKey{}, session)))
		if !ok {
			t.Fatal("missing scope", path)
		}
		return key
	}
	first := scope("/gross-board?weekStart=2026-11-16", "first")
	h := &boardOpenings{}
	ctx := context.Background()
	result := make(chan bool, 1)
	go func() { result <- h.wait(ctx, first, time.Second) }()
	deadline := time.Now().Add(time.Second)
	for {
		h.mu.Lock()
		waiting := h.changed != nil
		h.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("GET did not wait for the stream")
		}
		time.Sleep(time.Millisecond)
	}
	stop := h.register(scope("/gross-board/events?weekStart=2026-11-16", "first"))
	if !<-result || !h.wait(ctx, first, time.Second) {
		t.Fatal("paired read did not proceed")
	}
	for _, other := range []boardOpeningKey{
		scope("/gross-board?weekStart=2026-11-16", "second"),
		scope("/gross-board?weekStart=2026-11-09", "first"),
		scope("/driver-board?overview=1", "first"),
	} {
		if h.wait(ctx, other, time.Millisecond) {
			t.Fatal("matched wrong scope")
		}
	}
	otherTab := first
	otherTab.id = "e4500000-0000-0000-0000-000000000003"
	if h.wait(ctx, otherTab, time.Millisecond) {
		t.Fatal("matched another tab")
	}
	stopSecond := h.register(first)
	stop()
	if !h.wait(ctx, first, time.Second) {
		t.Fatal("overlapping reconnect lost registration")
	}
	stopSecond()
	if h.wait(ctx, first, time.Millisecond) {
		t.Fatal("closed stream admitted read")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if h.wait(cancelled, first, time.Second) || len(h.active) != 0 {
		t.Fatal("abandoned registration retained")
	}
}
