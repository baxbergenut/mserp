package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type DailyJob struct {
	Name   string
	Hour   int
	Minute int
	Run    func(context.Context) error
}

func RunDailyScheduler(ctx context.Context, logger *slog.Logger, location *time.Location, dailyJobs ...DailyJob) {
	var workers sync.WaitGroup
	workers.Add(len(dailyJobs))
	for _, dailyJob := range dailyJobs {
		go func(job DailyJob) {
			defer workers.Done()
			runDailyJob(ctx, logger, location, job)
		}(dailyJob)
	}
	workers.Wait()
}

func runDailyJob(ctx context.Context, logger *slog.Logger, location *time.Location, job DailyJob) {
	for {
		nextRun := nextDailyRun(time.Now(), location, job.Hour, job.Minute)
		logger.Info(
			"daily sync scheduled",
			"job", job.Name,
			"next_run", nextRun.Format(time.RFC3339),
			"timezone", location.String(),
		)

		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		startedAt := time.Now()
		logger.Info("daily sync starting", "job", job.Name)
		if err := job.Run(ctx); err != nil {
			if ctx.Err() != nil && errors.Is(err, context.Canceled) {
				logger.Info("daily sync canceled", "job", job.Name)
				return
			}
			logger.Error("daily sync failed", "job", job.Name, "error", err, "duration", time.Since(startedAt))
			continue
		}
		logger.Info("daily sync finished", "job", job.Name, "duration", time.Since(startedAt))
	}
}

func nextDailyRun(now time.Time, location *time.Location, hour, minute int) time.Time {
	localNow := now.In(location)
	nextRun := time.Date(
		localNow.Year(),
		localNow.Month(),
		localNow.Day(),
		hour,
		minute,
		0,
		0,
		location,
	)
	if !nextRun.After(localNow) {
		nextRun = time.Date(
			localNow.Year(),
			localNow.Month(),
			localNow.Day()+1,
			hour,
			minute,
			0,
			0,
			location,
		)
	}
	return nextRun
}
