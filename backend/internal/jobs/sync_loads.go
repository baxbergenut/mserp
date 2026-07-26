package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"mserp/internal/datatruck"
	"mserp/internal/repository"
)

const loadReconciliationLookbackDays = 45

var loadReconciliationDateColumns = []string{
	"pickup_time",
	"pickup_appointment_time",
	"delivery_time",
	"delivery_appointment_time",
}

type loadSyncClient interface {
	FetchLoadsAfterID(context.Context, int) ([]datatruck.Load, error)
	FetchLoadsByDateSince(context.Context, string, time.Time) ([]datatruck.Load, error)
}

type loadSyncRepository interface {
	MaxLoadID(context.Context) (int, error)
	UpsertLoads(context.Context, []repository.LoadRecord) error
}

type SyncLoadsJob struct {
	client loadSyncClient
	repo   loadSyncRepository
	logger *slog.Logger
}

type SyncLoadsResult struct {
	Fetched int       `json:"fetched"`
	Saved   int       `json:"saved"`
	Since   time.Time `json:"since"`
}

func NewSyncLoadsJob(client loadSyncClient, repo loadSyncRepository, logger *slog.Logger) *SyncLoadsJob {
	return &SyncLoadsJob{client: client, repo: repo, logger: logger}
}

func (j *SyncLoadsJob) Run(ctx context.Context) (SyncLoadsResult, error) {
	maxLoadID, err := j.repo.MaxLoadID(ctx)
	if err != nil {
		return SyncLoadsResult{}, err
	}

	// DataTruck does not expose a last-modified timestamp for orders. Fetch
	// every new upstream ID, then re-fetch a service-date window so status,
	// pay, mileage, driver, and appointment changes on older-created loads
	// are reconciled after dispatch and invoicing.
	since := time.Now().UTC().AddDate(0, 0, -loadReconciliationLookbackDays)
	loadsByID := make(map[int]datatruck.Load)
	newLoads, err := j.client.FetchLoadsAfterID(ctx, maxLoadID)
	if err != nil {
		return SyncLoadsResult{}, err
	}
	addLoadsByID(loadsByID, newLoads)

	for _, column := range loadReconciliationDateColumns {
		loads, fetchErr := j.client.FetchLoadsByDateSince(ctx, column, since)
		if fetchErr != nil {
			return SyncLoadsResult{}, fetchErr
		}
		addLoadsByID(loadsByID, loads)
	}

	loadIDs := make([]int, 0, len(loadsByID))
	for id := range loadsByID {
		loadIDs = append(loadIDs, id)
	}
	sort.Ints(loadIDs)

	records := make([]repository.LoadRecord, 0, len(loadsByID))
	syncedAt := time.Now().UTC()
	for _, id := range loadIDs {
		load := loadsByID[id]
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

	result := SyncLoadsResult{Fetched: len(loadsByID), Saved: len(records), Since: since}
	j.logger.Info(
		"sync loads complete",
		"after_id", maxLoadID,
		"reconciliation_since", since,
		"fetched", result.Fetched,
		"saved", result.Saved,
	)
	return result, nil
}

func addLoadsByID(target map[int]datatruck.Load, loads []datatruck.Load) {
	for _, load := range loads {
		target[load.ID] = load
	}
}
