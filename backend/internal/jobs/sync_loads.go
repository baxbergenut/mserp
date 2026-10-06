package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"mserp/internal/datatruck"
	"mserp/internal/repository"
)

const loadReconciliationLookbackDays = 21

var loadReconciliationDateColumns = []string{
	"pickup_time",
	"pickup_appointment_time",
	"delivery_time",
	"delivery_appointment_time",
}

type loadSyncClient interface {
	FetchLoadsAfterID(context.Context, int) ([]datatruck.Load, error)
	FetchLoadsByDateSinceThroughID(context.Context, string, time.Time, int) ([]datatruck.Load, error)
}

type loadSyncRepository interface {
	MaxLoadID(context.Context) (int, error)
	UpsertLoads(context.Context, []repository.LoadRecord) error
	RefreshLoads(context.Context, []repository.LoadRecord) error
	ReconcileLoads(context.Context, []repository.LoadRecord) error
	OperationalLoadIDs(context.Context, time.Time, time.Time) ([]int, error)
}

type SyncLoadsJob struct {
	client      loadSyncClient
	repo        loadSyncRepository
	logger      *slog.Logger
	newGate     chan struct{}
	refreshGate chan struct{}
}

type SyncLoadsResult struct {
	Fetched int       `json:"fetched"`
	Saved   int       `json:"saved"`
	Since   time.Time `json:"since"`
}

func NewSyncLoadsJob(client loadSyncClient, repo loadSyncRepository, logger *slog.Logger) *SyncLoadsJob {
	return &SyncLoadsJob{client: client, repo: repo, logger: logger, newGate: make(chan struct{}, 1), refreshGate: make(chan struct{}, 1)}
}

func (j *SyncLoadsJob) Run(ctx context.Context) (SyncLoadsResult, error) {
	unlock, err := lockLoadSync(ctx, j.refreshGate)
	if err != nil {
		return SyncLoadsResult{}, err
	}
	defer unlock()
	maxLoadID, discovery, err := j.discover(ctx)
	if err != nil {
		return SyncLoadsResult{}, err
	}

	// DataTruck does not expose a last-modified timestamp for orders. Fetch
	// every new upstream ID, then re-fetch a service-date window so status,
	// pay, mileage, driver, and appointment changes on older-created loads
	// are reconciled after dispatch and invoicing.
	since := time.Now().UTC().AddDate(0, 0, -loadReconciliationLookbackDays)
	loadsByID := make(map[int]datatruck.Load)

	for _, column := range loadReconciliationDateColumns {
		if maxLoadID == 0 {
			break // The initial fetch already includes every upstream ID.
		}
		loads, fetchErr := j.client.FetchLoadsByDateSinceThroughID(ctx, column, since, maxLoadID)
		if fetchErr != nil {
			return SyncLoadsResult{}, fmt.Errorf("reconcile loads by %s: %w", column, fetchErr)
		}
		addLoadsByID(loadsByID, loads)
		j.logger.Info("sync loads reconciled records", "column", column, "fetched", len(loads))
	}
	saved, err := j.save(ctx, loadsByID, j.repo.ReconcileLoads)
	if err != nil {
		return SyncLoadsResult{}, err
	}
	result := SyncLoadsResult{Fetched: discovery.Fetched + len(loadsByID), Saved: discovery.Saved + saved, Since: since}
	j.logger.Info("sync loads complete", "after_id", maxLoadID, "reconciliation_since", since, "fetched", result.Fetched, "saved", result.Saved)
	return result, nil
}

func lockLoadSync(ctx context.Context, gate chan struct{}) (func(), error) {
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Discovery and reconciliation have separate locks: a long morning scan cannot
// hold up new loads. Reconciliation is capped at its pre-discovery watermark.
func (j *SyncLoadsJob) RunNew(ctx context.Context) (SyncLoadsResult, error) {
	_, result, err := j.discover(ctx)
	return result, err
}

func (j *SyncLoadsJob) discover(ctx context.Context) (int, SyncLoadsResult, error) {
	unlock, err := lockLoadSync(ctx, j.newGate)
	if err != nil {
		return 0, SyncLoadsResult{}, err
	}
	defer unlock()
	maxID, err := j.repo.MaxLoadID(ctx)
	if err != nil {
		return 0, SyncLoadsResult{}, err
	}
	loads, err := j.client.FetchLoadsAfterID(datatruck.WithDiscoveryPriority(ctx), maxID)
	if err != nil {
		return maxID, SyncLoadsResult{}, fmt.Errorf("fetch loads after record %d: %w", maxID, err)
	}
	byID := make(map[int]datatruck.Load, len(loads))
	addLoadsByID(byID, loads)
	saved, err := j.save(ctx, byID, j.repo.UpsertLoads)
	if err != nil {
		return maxID, SyncLoadsResult{}, err
	}
	result := SyncLoadsResult{Fetched: len(byID), Saved: saved}
	j.logger.Info("sync new loads complete", "after_id", maxID, "saved", saved)
	return maxID, result, nil
}

// Calendar boundaries use today's New York date, but source schedule dates are
// encoded in UTC by DataTruck and must not be shifted into the previous day.
func operationalLoadWindow(now time.Time) (time.Time, time.Time) {
	loc, _ := time.LoadLocation("America/New_York")
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -7), day.AddDate(0, 0, 4) // exclusive end, includes day +3
}

func (j *SyncLoadsJob) RunOperational(ctx context.Context) (SyncLoadsResult, error) {
	// Avoid competing with full scans or payroll refreshes; retry next interval.
	select {
	case j.refreshGate <- struct{}{}:
		defer func() { <-j.refreshGate }()
	default:
		j.logger.Info("operational load refresh skipped; reconciliation in progress")
		return SyncLoadsResult{}, nil
	}
	client, ok := j.client.(interface {
		FetchLoadsByIDs(context.Context, []int) ([]datatruck.Load, error)
	})
	if !ok {
		return SyncLoadsResult{}, fmt.Errorf("batch load lookup unavailable")
	}
	from, until := operationalLoadWindow(time.Now())
	ids, err := j.repo.OperationalLoadIDs(ctx, from, until)
	if err != nil {
		return SyncLoadsResult{}, err
	}
	result := SyncLoadsResult{Since: from}
	for start := 0; start < len(ids); start += 25 {
		loads, err := client.FetchLoadsByIDs(ctx, ids[start:min(start+25, len(ids))])
		if err != nil {
			return result, err
		}
		byID := make(map[int]datatruck.Load, len(loads))
		addLoadsByID(byID, loads)
		saved, err := j.save(ctx, byID, j.repo.RefreshLoads)
		if err != nil {
			return result, err
		}
		result.Fetched += len(byID)
		result.Saved += saved
	}
	j.logger.Info("operational load refresh complete", "selected", len(ids), "saved", result.Saved, "from", from, "until", until)
	return result, nil
}

func (j *SyncLoadsJob) save(ctx context.Context, loadsByID map[int]datatruck.Load, persist func(context.Context, []repository.LoadRecord) error) (int, error) {
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
			return 0, fmt.Errorf("encode datatruck load %d: %w", id, err)
		}

		record, err := repository.LoadToRecord(load, payload, syncedAt)
		if err != nil {
			return 0, fmt.Errorf("map datatruck load %d: %w", id, err)
		}
		if load.LoadID == nil || strings.TrimSpace(*load.LoadID) == "" {
			j.logger.Warn("sync load missing display number; using upstream record ID", "record_id", id)
		}
		records = append(records, record)
	}

	if err := persist(ctx, records); err != nil {
		return 0, err
	}
	return len(records), nil
}

func addLoadsByID(target map[int]datatruck.Load, loads []datatruck.Load) {
	for _, load := range loads {
		target[load.ID] = load
	}
}

// Refresh only source fields of already-linked payroll loads, including older
// weeks outside normal reconciliation. Never create records or change assignment.
func (j *SyncLoadsJob) RefreshLoadDetails(ctx context.Context, ids []int) error {
	unlock, err := lockLoadSync(ctx, j.refreshGate)
	if err != nil {
		return err
	}
	defer unlock()
	client, ok := j.client.(interface {
		FetchLoadByID(context.Context, int) (datatruck.Load, error)
	})
	if !ok {
		return fmt.Errorf("load detail lookup unavailable")
	}
	repo, ok := j.repo.(interface {
		UpdateLoadDetails(context.Context, repository.LoadRecord) error
	})
	if !ok {
		return fmt.Errorf("load detail storage unavailable")
	}
	for _, id := range ids {
		load, err := client.FetchLoadByID(ctx, id)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(load)
		if err != nil {
			return err
		}
		record, err := repository.LoadToRecord(load, payload, time.Now())
		if err != nil {
			return err
		}
		if err := repo.UpdateLoadDetails(ctx, record); err != nil {
			return err
		}
	}
	return nil
}
