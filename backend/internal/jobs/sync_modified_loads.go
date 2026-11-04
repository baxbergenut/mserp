package jobs

import (
	"context"
	"fmt"
	"time"

	"mserp/internal/datatruck"
)

const modifiedLoadOverlap = 2 * time.Minute

type modifiedLoadClient interface {
	LoadSyncScope() string
	FetchModifiedLoadsPage(context.Context, time.Time, int) ([]datatruck.Load, error)
}

type modifiedLoadRepository interface {
	LoadModifiedCheckpoint(context.Context, string, time.Time) (time.Time, error)
	CompleteModifiedSweep(context.Context, string, time.Time, time.Time) error
}

// RunModified is independent of slow reconciliation. The shared request gate
// prioritizes these small pages and discovery; repository provider-version checks
// prevent an older in-flight scan from overwriting their newer source evidence.
func (j *SyncLoadsJob) RunModified(ctx context.Context) (SyncLoadsResult, error) {
	unlock, err := lockLoadSync(ctx, j.modifiedGate)
	if err != nil {
		return SyncLoadsResult{}, err
	}
	defer unlock()
	client, ok := j.client.(modifiedLoadClient)
	if !ok {
		return SyncLoadsResult{}, fmt.Errorf("modified-load lookup unavailable")
	}
	repo, ok := j.repo.(modifiedLoadRepository)
	if !ok {
		return SyncLoadsResult{}, fmt.Errorf("modified-load checkpoint unavailable")
	}
	// First activation catches the preceding day, including older service dates.
	// Later restarts/outages use the durable cursor without any age cutoff.
	started := time.Now().UTC().Truncate(time.Microsecond)
	checkpoint, err := repo.LoadModifiedCheckpoint(ctx, client.LoadSyncScope(), started.Add(-24*time.Hour))
	if err != nil {
		return SyncLoadsResult{}, err
	}
	if started.Before(checkpoint) {
		return SyncLoadsResult{}, fmt.Errorf("modified-load checkpoint is in the future")
	}
	since := checkpoint.Add(-modifiedLoadOverlap)
	result := SyncLoadsResult{Since: since}
	afterID := 0
	for {
		loads, err := client.FetchModifiedLoadsPage(datatruck.WithDiscoveryPriority(ctx), since, afterID)
		if err != nil {
			return result, err
		}
		byID := make(map[int]datatruck.Load, len(loads))
		addLoadsByID(byID, loads)
		// New IDs remain discovery's responsibility, so partial modified sweeps
		// cannot leap over undiscovered orders by advancing MAX(loads.id).
		saved, err := j.save(ctx, byID, j.repo.RefreshLoads)
		if err != nil {
			return result, err
		}
		result.Fetched += len(loads)
		result.Saved += saved
		if len(loads) < 25 {
			break
		}
		afterID = loads[len(loads)-1].ID
	}
	if err := repo.CompleteModifiedSweep(ctx, client.LoadSyncScope(), checkpoint, started); err != nil {
		return result, err
	}
	j.logger.Info("modified load sync complete", "since", since, "through", started, "fetched", result.Fetched)
	return result, nil
}
