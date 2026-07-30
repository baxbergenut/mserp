package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"mserp/internal/prepass"
	"mserp/internal/repository"
)

const maxPrePassRangeDays = 31

type tollClient interface {
	FetchTransactions(context.Context, time.Time, time.Time) ([]prepass.Transaction, error)
}

type tollStore interface {
	CompletedDays(context.Context, string, time.Time, time.Time) (map[string]struct{}, error)
	ReconcileTruckAssignments(context.Context) error
	UpsertDay(
		context.Context,
		string,
		time.Time,
		[]prepass.Transaction,
		time.Time,
		bool,
	) (repository.TollSyncDayResult, error)
}

type SyncTollsJob struct {
	client      tollClient
	store       tollStore
	environment string
	startDate   time.Time
	logger      *slog.Logger
	now         func() time.Time
}

type SyncTollsResult struct {
	Fetched     int    `json:"fetched"`
	Saved       int    `json:"saved"`
	Unmatched   int    `json:"unmatched"`
	DaysFetched int    `json:"daysFetched"`
	DaysSkipped int    `json:"daysSkipped"`
	StartDate   string `json:"startDate"`
	EndDate     string `json:"endDate"`
}

func NewSyncTollsJob(
	client tollClient,
	store tollStore,
	environment string,
	startDate time.Time,
	logger *slog.Logger,
) *SyncTollsJob {
	return &SyncTollsJob{
		client:      client,
		store:       store,
		environment: environment,
		startDate:   utcDay(startDate),
		logger:      logger,
		now:         time.Now,
	}
}

func (j *SyncTollsJob) Run(ctx context.Context) (SyncTollsResult, error) {
	today := utcDay(j.now())
	currentYearStart := time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	startDate := j.startDate
	if startDate.Before(currentYearStart) {
		startDate = currentYearStart
	}
	if startDate.After(today) {
		startDate = today
	}

	if err := j.store.ReconcileTruckAssignments(ctx); err != nil {
		return SyncTollsResult{}, fmt.Errorf("reconcile PrePass toll truck assignments: %w", err)
	}
	completed, err := j.store.CompletedDays(ctx, j.environment, startDate, today)
	if err != nil {
		return SyncTollsResult{}, fmt.Errorf("load completed PrePass toll sync days: %w", err)
	}

	result := SyncTollsResult{
		StartDate: startDate.Format(time.DateOnly),
		EndDate:   today.Format(time.DateOnly),
	}
	for day := startDate; !day.After(today); {
		if day.Before(today) {
			if _, ok := completed[day.Format(time.DateOnly)]; ok {
				result.DaysSkipped++
				day = day.AddDate(0, 0, 1)
				continue
			}
		}

		rangeEnd := day
		for count := 0; count < maxPrePassRangeDays && !rangeEnd.After(today); count++ {
			if rangeEnd.After(day) && rangeEnd.Before(today) {
				if _, ok := completed[rangeEnd.Format(time.DateOnly)]; ok {
					break
				}
			}
			rangeEnd = rangeEnd.AddDate(0, 0, 1)
		}

		transactions, err := j.client.FetchTransactions(ctx, day, rangeEnd)
		if err != nil {
			return result, fmt.Errorf(
				"fetch PrePass toll transactions for %s through %s: %w",
				day.Format(time.DateOnly),
				rangeEnd.AddDate(0, 0, -1).Format(time.DateOnly),
				err,
			)
		}
		byDay, err := groupPrePassTransactions(day, rangeEnd, transactions)
		if err != nil {
			return result, err
		}
		result.Fetched += len(transactions)

		for syncDay := day; syncDay.Before(rangeEnd); syncDay = syncDay.AddDate(0, 0, 1) {
			dayResult, err := j.store.UpsertDay(
				ctx,
				j.environment,
				syncDay,
				byDay[syncDay.Format(time.DateOnly)],
				j.now().UTC(),
				syncDay.Before(today),
			)
			if err != nil {
				return result, fmt.Errorf(
					"save PrePass toll transactions for %s: %w",
					syncDay.Format(time.DateOnly),
					err,
				)
			}
			result.DaysFetched++
			result.Saved += dayResult.Saved
			result.Unmatched += dayResult.Unmatched
		}
		day = rangeEnd
	}

	j.logger.Info(
		"sync PrePass tolls complete",
		"environment", j.environment,
		"start_date", result.StartDate,
		"end_date", result.EndDate,
		"days_fetched", result.DaysFetched,
		"days_skipped", result.DaysSkipped,
		"transactions", result.Saved,
		"unmatched", result.Unmatched,
	)
	return result, nil
}

func groupPrePassTransactions(
	start time.Time,
	end time.Time,
	transactions []prepass.Transaction,
) (map[string][]prepass.Transaction, error) {
	byDay := make(map[string][]prepass.Transaction)
	for _, transaction := range transactions {
		postDateTime, err := prepass.ParseTimestamp(transaction.PostDateTime)
		if err != nil {
			return nil, fmt.Errorf(
				"PrePass toll %d has invalid postDateTime %q",
				transaction.TollID,
				transaction.PostDateTime,
			)
		}
		day := time.Date(
			postDateTime.Year(),
			postDateTime.Month(),
			postDateTime.Day(),
			0,
			0,
			0,
			0,
			time.UTC,
		)
		if day.Before(start) || !day.Before(end) {
			return nil, fmt.Errorf(
				"PrePass toll %d posted outside requested date range",
				transaction.TollID,
			)
		}
		key := day.Format(time.DateOnly)
		byDay[key] = append(byDay[key], transaction)
	}
	return byDay, nil
}
