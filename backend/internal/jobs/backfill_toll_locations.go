package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"mserp/internal/prepass"
)

type tollLocationStore interface {
	UpdateTollLocations(context.Context, string, []prepass.Transaction, time.Time) (int64, error)
}

// Re-fetch the current year's source metadata without resetting completed sync
// dates or inserting/updating any financial transactions.
func BackfillTollLocations(ctx context.Context, client tollClient, store tollLocationStore, environment string, now time.Time, logger *slog.Logger) error {
	end := utcDay(now).AddDate(0, 0, 1)
	for start := time.Date(now.UTC().Year(), 1, 1, 0, 0, 0, 0, time.UTC); start.Before(end); {
		rangeEnd := start.AddDate(0, 0, maxPrePassRangeDays)
		if rangeEnd.After(end) {
			rangeEnd = end
		}
		values, err := client.FetchTransactions(ctx, start, rangeEnd)
		if err != nil {
			return fmt.Errorf("fetch toll locations for %s: %w", start.Format(time.DateOnly), err)
		}
		updated, err := store.UpdateTollLocations(ctx, environment, values, now)
		if err != nil {
			return fmt.Errorf("save toll locations for %s: %w", start.Format(time.DateOnly), err)
		}
		logger.Info("toll location range refreshed", "from", start.Format(time.DateOnly), "until", rangeEnd.Format(time.DateOnly), "fetched", len(values), "updated", updated)
		start = rangeEnd
	}
	logger.Info("toll location backfill complete")
	return nil
}
