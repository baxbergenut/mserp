package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"mserp/internal/relay"
)

type fakeFuelClient struct {
	days []string
}

func (f *fakeFuelClient) FetchTransactions(_ context.Context, start, _ time.Time) ([]relay.Transaction, error) {
	f.days = append(f.days, start.Format(time.DateOnly))
	return []relay.Transaction{
		{
			TransactionID: "txn_" + start.Format("20060102"),
			FuelItems:     []relay.FuelItem{{FuelType: "diesel"}},
		},
		{TransactionID: "deposit_" + start.Format("20060102")},
	}, nil
}

type storedFuelDay struct {
	day      string
	complete bool
}

type fakeFuelStore struct {
	completed map[string]struct{}
	stored    []storedFuelDay
}

func (f *fakeFuelStore) CompletedDays(context.Context, string, time.Time, time.Time) (map[string]struct{}, error) {
	return f.completed, nil
}

func (f *fakeFuelStore) UpsertDay(
	_ context.Context,
	_ string,
	day time.Time,
	_ []relay.Transaction,
	_ time.Time,
	complete bool,
) error {
	f.stored = append(f.stored, storedFuelDay{day: day.Format(time.DateOnly), complete: complete})
	return nil
}

func TestSyncFuelFetchesMissingDaysAndAlwaysRefreshesToday(t *testing.T) {
	client := &fakeFuelClient{}
	store := &fakeFuelStore{completed: map[string]struct{}{
		"2026-07-20": {},
		"2026-07-21": {}, // Even an accidental current-day marker must not skip today.
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	job := NewSyncFuelJob(
		client,
		store,
		"production",
		time.Date(2026, time.July, 19, 0, 0, 0, 0, time.UTC),
		logger,
	)
	job.now = func() time.Time {
		return time.Date(2026, time.July, 21, 16, 0, 0, 0, time.UTC)
	}

	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.DaysFetched != 2 || result.DaysSkipped != 1 || result.Fetched != 4 || result.Saved != 2 || result.Excluded != 2 {
		t.Fatalf("result = %#v", result)
	}
	if got := client.days; len(got) != 2 || got[0] != "2026-07-19" || got[1] != "2026-07-21" {
		t.Fatalf("fetched days = %#v", got)
	}
	if len(store.stored) != 2 || !store.stored[0].complete || store.stored[1].complete {
		t.Fatalf("stored days = %#v", store.stored)
	}
}
