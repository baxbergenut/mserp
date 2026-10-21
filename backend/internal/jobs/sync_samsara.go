package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"mserp/internal/repository"
	"mserp/internal/samsara"
)

type LocationRefresher interface {
	Run(context.Context) (repository.FiveELDSyncResult, error)
}

var ErrTelemetrySyncInProgress = errors.New("Telematics refresh is already in progress")

type samsaraClient interface {
	Snapshot(context.Context, time.Time) (samsara.Snapshot, error)
}
type samsaraRepository interface {
	Store(context.Context, samsara.Snapshot, time.Time) error
	RecordFailure(context.Context, time.Time, error) error
}
type SyncSamsaraJob struct {
	client  samsaraClient
	repo    samsaraRepository
	running atomic.Bool
	now     func() time.Time
}

func NewSyncSamsaraJob(client samsaraClient, repo samsaraRepository) *SyncSamsaraJob {
	return &SyncSamsaraJob{client: client, repo: repo, now: time.Now}
}
func (j *SyncSamsaraJob) Run(ctx context.Context) (repository.FiveELDSyncResult, error) {
	if !j.running.CompareAndSwap(false, true) {
		return repository.FiveELDSyncResult{}, ErrTelemetrySyncInProgress
	}
	defer j.running.Store(false)
	now := j.now().UTC()
	snapshot, err := j.client.Snapshot(ctx, now)
	if err == nil {
		err = j.repo.Store(ctx, snapshot, now)
	}
	if err != nil {
		_ = j.repo.RecordFailure(ctx, now, err)
		return repository.FiveELDSyncResult{}, err
	}
	return repository.FiveELDSyncResult{Fetched: snapshot.Fetched, Saved: len(snapshot.Vehicles), Ambiguous: len(snapshot.Ambiguous), Invalid: snapshot.Invalid, SyncedAt: now}, nil
}

type TelemetryRefreshJob struct {
	Samsara, FiveELD LocationRefresher
	Logger           *slog.Logger
}

func (j *TelemetryRefreshJob) Run(ctx context.Context) (repository.FiveELDSyncResult, error) {
	var result repository.FiveELDSyncResult
	var failures []error
	success := false
	for _, provider := range []struct {
		name string
		job  LocationRefresher
	}{{"Samsara", j.Samsara}, {"Five ELD", j.FiveELD}} {
		if provider.job == nil {
			continue
		}
		value, err := provider.job.Run(ctx)
		if err != nil {
			failures = append(failures, err)
			if j.Logger != nil {
				j.Logger.Warn("telematics refresh failed", "provider", provider.name, "error", err)
			}
			continue
		}
		success = true
		result.Fetched += value.Fetched
		result.Saved += value.Saved
		result.Ambiguous += value.Ambiguous
		result.Invalid += value.Invalid
		result.Unmatched += value.Unmatched
		if value.SyncedAt.After(result.SyncedAt) {
			result.SyncedAt = value.SyncedAt
		}
	}
	if !success {
		return result, errors.Join(failures...)
	}
	return result, nil
}
