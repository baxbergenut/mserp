package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"mserp/internal/repository"
	"mserp/internal/samsara"
)

type samsaraFixtureClient struct {
	snapshot samsara.Snapshot
	err      error
}

func (f samsaraFixtureClient) Snapshot(context.Context, time.Time) (samsara.Snapshot, error) {
	return f.snapshot, f.err
}

type samsaraFixtureRepository struct{ stored, failed int }

func (f *samsaraFixtureRepository) Store(context.Context, samsara.Snapshot, time.Time) error {
	f.stored++
	return nil
}
func (f *samsaraFixtureRepository) RecordFailure(context.Context, time.Time, error) error {
	f.failed++
	return nil
}
func TestSamsaraJobDoesNotApplyPartialFailure(t *testing.T) {
	repo := &samsaraFixtureRepository{}
	job := NewSyncSamsaraJob(samsaraFixtureClient{err: errors.New("fixture failure")}, repo)
	if _, err := job.Run(context.Background()); err == nil || repo.stored != 0 || repo.failed != 1 {
		t.Fatal("failed request changed cache", repo, err)
	}
	job.client = samsaraFixtureClient{snapshot: samsara.Snapshot{Vehicles: []samsara.Vehicle{{VIN: "1M8GDM9AXKP042788"}}, Fetched: 1}}
	result, err := job.Run(context.Background())
	if err != nil || repo.stored != 1 || result.Saved != 1 {
		t.Fatal(result, err)
	}
}

type fixtureRefresher struct {
	err   error
	calls int
}

func (f *fixtureRefresher) Run(context.Context) (repository.FiveELDSyncResult, error) {
	f.calls++
	return repository.FiveELDSyncResult{Saved: 1}, f.err
}
func TestTelemetryRefreshRetainsAvailableProvider(t *testing.T) {
	a, b := &fixtureRefresher{err: errors.New("fixture provider failure")}, &fixtureRefresher{}
	job := &TelemetryRefreshJob{Samsara: a, FiveELD: b}
	result, err := job.Run(context.Background())
	if err != nil || result.Saved != 1 || a.calls != 1 || b.calls != 1 {
		t.Fatal(result, err)
	}
	b.err = errors.New("fixture other provider failure")
	if _, err = job.Run(context.Background()); err == nil {
		t.Fatal("both failures hidden")
	}
}
