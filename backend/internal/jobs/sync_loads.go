package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"mserp/internal/datatruck"
	"mserp/internal/repository"
)

type SyncLoadsJob struct {
	client *datatruck.Client
	repo   *repository.LoadRepository
	logger *slog.Logger
}

type SyncLoadsResult struct {
	Fetched int       `json:"fetched"`
	Saved   int       `json:"saved"`
	Since   time.Time `json:"since"`
}

func NewSyncLoadsJob(client *datatruck.Client, repo *repository.LoadRepository, logger *slog.Logger) *SyncLoadsJob {
	return &SyncLoadsJob{client: client, repo: repo, logger: logger}
}

func (j *SyncLoadsJob) Run(ctx context.Context) (SyncLoadsResult, error) {
	// Re-fetch a rolling one-week window so recent changes in DataTruck are
	// reflected locally as well as newly created loads.
	since := time.Now().UTC().AddDate(0, 0, -7)
	loads, err := j.client.FetchLoadsSince(ctx, since)
	if err != nil {
		return SyncLoadsResult{}, err
	}

	records := make([]repository.LoadRecord, 0, len(loads))
	syncedAt := time.Now().UTC()
	for _, load := range loads {
		payload, err := json.Marshal(load)
		if err != nil {
			return SyncLoadsResult{}, err
		}

		record, err := repository.LoadToRecord(load, payload, syncedAt)
		if err != nil {
			return SyncLoadsResult{}, err
		}
		records = append(records, record)
	}

	if err := j.repo.UpsertLoads(ctx, records); err != nil {
		return SyncLoadsResult{}, err
	}

	result := SyncLoadsResult{Fetched: len(loads), Saved: len(records), Since: since}
	j.logger.Info("sync loads complete", "since", since, "fetched", result.Fetched, "saved", result.Saved)
	return result, nil
}
