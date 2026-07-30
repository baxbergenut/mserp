package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"mserp/internal/prepass"
	"mserp/internal/repository"
)

type tollFetchCall struct {
	start time.Time
	end   time.Time
}

type fakeTollClient struct {
	calls []tollFetchCall
}

func (f *fakeTollClient) FetchTransactions(
	_ context.Context,
	start time.Time,
	end time.Time,
) ([]prepass.Transaction, error) {
	f.calls = append(f.calls, tollFetchCall{start: start, end: end})
	values := make([]prepass.Transaction, 0)
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		values = append(values, prepass.Transaction{
			TollID:       int64(day.Day()),
			PostDateTime: day.Add(12 * time.Hour).Format(time.RFC3339),
		})
	}
	return values, nil
}

type tollUpsertCall struct {
	day          time.Time
	transactions []prepass.Transaction
	markComplete bool
}

type fakeTollStore struct {
	completed  map[string]struct{}
	upserts    []tollUpsertCall
	reconciled bool
}

func (f *fakeTollStore) CompletedDays(
	context.Context,
	string,
	time.Time,
	time.Time,
) (map[string]struct{}, error) {
	return f.completed, nil
}

func (f *fakeTollStore) ReconcileTruckAssignments(context.Context) error {
	f.reconciled = true
	return nil
}

func (f *fakeTollStore) UpsertDay(
	_ context.Context,
	_ string,
	day time.Time,
	transactions []prepass.Transaction,
	_ time.Time,
	markComplete bool,
) (repository.TollSyncDayResult, error) {
	f.upserts = append(f.upserts, tollUpsertCall{
		day: day, transactions: transactions, markComplete: markComplete,
	})
	return repository.TollSyncDayResult{Saved: len(transactions)}, nil
}

func TestSyncTollsBatchesMissingDaysAndLeavesCurrentDayOpen(t *testing.T) {
	client := &fakeTollClient{}
	store := &fakeTollStore{completed: map[string]struct{}{"2026-07-03": {}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	job := NewSyncTollsJob(
		client,
		store,
		"nonproduction",
		time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		logger,
	)
	job.now = func() time.Time {
		return time.Date(2026, time.July, 5, 15, 0, 0, 0, time.UTC)
	}

	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !store.reconciled {
		t.Fatal("truck assignments were not reconciled")
	}
	if len(client.calls) != 2 {
		t.Fatalf("fetch calls = %#v", client.calls)
	}
	if got := client.calls[0]; got.start.Day() != 1 || got.end.Day() != 3 {
		t.Fatalf("first fetch = %#v", got)
	}
	if got := client.calls[1]; got.start.Day() != 4 || got.end.Day() != 6 {
		t.Fatalf("second fetch = %#v", got)
	}
	if result.DaysFetched != 4 || result.DaysSkipped != 1 || result.Saved != 4 {
		t.Fatalf("result = %+v", result)
	}
	if len(store.upserts) != 4 {
		t.Fatalf("upserts = %#v", store.upserts)
	}
	if store.upserts[len(store.upserts)-1].markComplete {
		t.Fatal("current UTC day was marked complete")
	}
	for _, call := range store.upserts[:len(store.upserts)-1] {
		if !call.markComplete {
			t.Fatalf("past day %s was not marked complete", call.day)
		}
	}
}

func TestSyncTollsLimitsFetchRangesToThirtyOneDays(t *testing.T) {
	client := &fakeTollClient{}
	store := &fakeTollStore{completed: map[string]struct{}{}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	job := NewSyncTollsJob(
		client,
		store,
		"production",
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		logger,
	)
	job.now = func() time.Time {
		return time.Date(2026, time.March, 5, 0, 0, 0, 0, time.UTC)
	}

	if _, err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range client.calls {
		if days := int(call.end.Sub(call.start).Hours() / 24); days > maxPrePassRangeDays {
			t.Fatalf("fetch range = %d days", days)
		}
	}
}
