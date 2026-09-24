package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"mserp/internal/prepass"
)

type fakeLocationStore struct{ count int }

func (s *fakeLocationStore) UpdateTollLocations(_ context.Context, _ string, values []prepass.Transaction, _ time.Time) (int64, error) {
	s.count += len(values)
	return int64(len(values)), nil
}

func TestTollLocationBackfillWindows(t *testing.T) {
	client := &fakeTollClient{}
	store := &fakeLocationStore{}
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	if err := BackfillTollLocations(context.Background(), client, store, "production", now, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 3 || store.count != 64 {
		t.Fatalf("calls=%d records=%d", len(client.calls), store.count)
	}
	for i, call := range client.calls {
		if call.end.Sub(call.start) > 31*24*time.Hour {
			t.Fatal("window exceeds API limit")
		}
		if i > 0 && !call.start.Equal(client.calls[i-1].end) {
			t.Fatal("gap or overlap")
		}
	}
	if client.calls[0].start.Format(time.DateOnly) != "2026-01-01" || client.calls[2].end.Format(time.DateOnly) != "2026-03-06" {
		t.Fatal("wrong date boundaries")
	}
}
