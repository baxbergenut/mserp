package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"mserp/internal/datatruck"
	"mserp/internal/repository"
)

type modifiedClientFake struct {
	fakeLoadSyncClient
	pages  map[int][]datatruck.Load
	failID int
	calls  []int
	since  []time.Time
}

func (f *modifiedClientFake) LoadSyncScope() string { return "test-tenant" }
func (f *modifiedClientFake) FetchModifiedLoadsPage(_ context.Context, since time.Time, id int) ([]datatruck.Load, error) {
	f.calls = append(f.calls, id)
	f.since = append(f.since, since)
	if id == f.failID {
		return nil, errors.New("page failed")
	}
	return f.pages[id], nil
}

type modifiedRepoFake struct {
	fakeLoadSyncRepository
	checkpoint  time.Time
	completed   int
	saveErr     error
	completeErr error
}

func (f *modifiedRepoFake) LoadModifiedCheckpoint(_ context.Context, scope string, initial time.Time) (time.Time, error) {
	if scope != "test-tenant" {
		panic(scope)
	}
	if f.checkpoint.IsZero() {
		f.checkpoint = initial
	}
	return f.checkpoint, nil
}
func (f *modifiedRepoFake) CompleteModifiedSweep(_ context.Context, _ string, old, next time.Time) error {
	if f.completeErr != nil {
		return f.completeErr
	}
	if !old.Equal(f.checkpoint) {
		return errors.New("cursor conflict")
	}
	f.checkpoint = next
	f.completed++
	return nil
}
func (f *modifiedRepoFake) RefreshLoads(ctx context.Context, records []repository.LoadRecord) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	return f.fakeLoadSyncRepository.RefreshLoads(ctx, records)
}

func TestModifiedSweepCheckpointReplayAndIndependentReconciliation(t *testing.T) {
	checkpoint := time.Now().UTC().Add(-time.Hour)
	client := &modifiedClientFake{pages: map[int][]datatruck.Load{}, failID: 25}
	for id := 1; id <= 25; id++ {
		client.pages[0] = append(client.pages[0], datatruck.Load{ID: id})
	}
	client.pages[25] = []datatruck.Load{{ID: 30}}
	repo := &modifiedRepoFake{checkpoint: checkpoint}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	job.refreshGate <- struct{}{} // Slow reconciliation must not block the fast feed.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := job.RunModified(ctx); err == nil {
		t.Fatal("expected failed second page")
	}
	if !repo.checkpoint.Equal(checkpoint) || repo.completed != 0 || repo.refreshes != 1 {
		t.Fatal("partial sweep advanced checkpoint")
	}
	client.failID = -1
	before := time.Now().UTC().Truncate(time.Microsecond)
	result, err := job.RunModified(ctx)
	if err != nil || result.Fetched != 26 || repo.completed != 1 || repo.checkpoint.Before(before) || repo.checkpoint.After(time.Now()) {
		t.Fatalf("%+v %v cursor %s", result, err, repo.checkpoint)
	}
	if len(client.calls) != 4 || client.calls[2] != 0 || client.calls[3] != 25 {
		t.Fatal("retry did not replay from committed checkpoint", client.calls)
	}
	for _, since := range client.since {
		if !since.Equal(checkpoint.Add(-modifiedLoadOverlap)) {
			t.Fatal("moving lower bound", since)
		}
	}
	if repo.maxID != 0 || len(repo.lookups) != 0 {
		t.Fatal("modified feed advanced discovery or inferred a deletion")
	}
}

func TestModifiedSweepSaveAndCheckpointFailure(t *testing.T) {
	for _, phase := range []string{"save", "checkpoint"} {
		t.Run(phase, func(t *testing.T) {
			old := time.Now().Add(-time.Hour)
			client := &modifiedClientFake{pages: map[int][]datatruck.Load{0: {{ID: 1}}}, failID: -1}
			repo := &modifiedRepoFake{checkpoint: old}
			if phase == "save" {
				repo.saveErr = errors.New("write failed")
			} else {
				repo.completeErr = errors.New("cursor failed")
			}
			job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if _, err := job.RunModified(context.Background()); err == nil {
				t.Fatal("expected failure")
			}
			if !repo.checkpoint.Equal(old) || repo.completed != 0 {
				t.Fatal("failed write moved checkpoint")
			}
		})
	}
}

func TestModifiedSweepEmptyAndCancellation(t *testing.T) {
	client := &modifiedClientFake{failID: -1}
	repo := &modifiedRepoFake{}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := job.RunModified(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 || repo.completed != 1 {
		t.Fatal("empty sweep must use one request")
	}
	job.modifiedGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := job.RunModified(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestReconciliationKeepsNewestRepeatedSource(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Minute)
	loads := make(map[int]datatruck.Load)
	addLoadsByID(loads, []datatruck.Load{{ID: 1, Status: "delivered", ModifiedDatetime: &now}})
	addLoadsByID(loads, []datatruck.Load{{ID: 1, Status: "booked", ModifiedDatetime: &old}, {ID: 1, Status: "unknown"}})
	if loads[1].Status != "delivered" {
		t.Fatal("older date-window response replaced newer evidence")
	}
}
