package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"mserp/internal/relay"
)

type fuelClient interface {
	FetchTransactions(context.Context, time.Time, time.Time) ([]relay.Transaction, error)
}

type fuelStore interface {
	CompletedDays(context.Context, string, time.Time, time.Time) (map[string]struct{}, error)
	UpsertDay(context.Context, string, time.Time, []relay.Transaction, time.Time, bool) error
}

type SyncFuelJob struct {
	client      fuelClient
	store       fuelStore
	environment string
	startDate   time.Time
	logger      *slog.Logger
	now         func() time.Time
}

type SyncFuelResult struct {
	Fetched     int    `json:"fetched"`
	Saved       int    `json:"saved"`
	Excluded    int    `json:"excluded"`
	DaysFetched int    `json:"daysFetched"`
	DaysSkipped int    `json:"daysSkipped"`
	StartDate   string `json:"startDate"`
	EndDate     string `json:"endDate"`
}

func NewSyncFuelJob(
	client fuelClient,
	store fuelStore,
	environment string,
	startDate time.Time,
	logger *slog.Logger,
) *SyncFuelJob {
	return &SyncFuelJob{
		client:      client,
		store:       store,
		environment: environment,
		startDate:   utcDay(startDate),
		logger:      logger,
		now:         time.Now,
	}
}

func (j *SyncFuelJob) Run(ctx context.Context) (SyncFuelResult, error) {
	today := utcDay(j.now())
	startDate := j.startDate
	if startDate.After(today) {
		startDate = today
	}

	completed, err := j.store.CompletedDays(ctx, j.environment, startDate, today)
	if err != nil {
		return SyncFuelResult{}, fmt.Errorf("load completed Relay fuel sync days: %w", err)
	}

	result := SyncFuelResult{
		StartDate: startDate.Format(time.DateOnly),
		EndDate:   today.Format(time.DateOnly),
	}
	for day := startDate; !day.After(today); day = day.AddDate(0, 0, 1) {
		dateKey := day.Format(time.DateOnly)
		if day.Before(today) {
			if _, ok := completed[dateKey]; ok {
				result.DaysSkipped++
				continue
			}
		}

		fetchedTransactions, err := j.client.FetchTransactions(ctx, day, day.AddDate(0, 0, 1))
		if err != nil {
			return result, fmt.Errorf("fetch Relay fuel transactions for %s: %w", dateKey, err)
		}
		transactions := make([]relay.Transaction, 0, len(fetchedTransactions))
		for _, transaction := range fetchedTransactions {
			if len(transaction.FuelItems) == 0 {
				result.Excluded++
				continue
			}
			transactions = append(transactions, transaction)
		}

		syncedAt := j.now().UTC()
		if err := j.store.UpsertDay(
			ctx,
			j.environment,
			day,
			transactions,
			syncedAt,
			day.Before(today),
		); err != nil {
			return result, fmt.Errorf("save Relay fuel transactions for %s: %w", dateKey, err)
		}

		result.DaysFetched++
		result.Fetched += len(fetchedTransactions)
		result.Saved += len(transactions)
	}

	j.logger.Info(
		"sync Relay fuel complete",
		"environment", j.environment,
		"start_date", result.StartDate,
		"end_date", result.EndDate,
		"days_fetched", result.DaysFetched,
		"days_skipped", result.DaysSkipped,
		"transactions", result.Saved,
	)
	return result, nil
}

func utcDay(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
