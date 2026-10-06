package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"mserp/internal/datatruck"
)

func TestNewLoadsPersistEvenWhenMorningReconciliationFails(t *testing.T) {
	client := &fakeLoadSyncClient{newLoads: []datatruck.Load{{ID: 11}}, dateErr: errors.New("upstream unavailable")}
	repo := &fakeLoadSyncRepository{maxID: 10}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("expected reconciliation failure")
	}
	if repo.maxID != 11 || len(repo.records) != 1 || repo.refreshes != 0 {
		t.Fatalf("new records were lost: %+v", repo.records)
	}
}

func TestNewLoadFailureDoesNotAdvanceWatermark(t *testing.T) {
	client := &fakeLoadSyncClient{newLoads: []datatruck.Load{{ID: 11}}}
	repo := &fakeLoadSyncRepository{maxID: 10, upsertErr: errors.New("database unavailable")}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := job.RunNew(context.Background()); err == nil {
		t.Fatal("expected save failure")
	}
	if repo.maxID != 10 {
		t.Fatal("failed save advanced discovery")
	}
}

func TestNewLoadsContinueDuringMorningScan(t *testing.T) {
	started, resume := make(chan struct{}, 4), make(chan struct{})
	client := &fakeLoadSyncClient{
		newHook:  func(id int) ([]datatruck.Load, error) { return []datatruck.Load{{ID: id + 1}}, nil },
		dateHook: func() { started <- struct{}{}; <-resume },
	}
	repo := &fakeLoadSyncRepository{maxID: 10}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := job.Run(ctx); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		close(resume)
		t.Fatal("scan did not start")
	}
	result, err := job.RunNew(ctx)
	if err != nil || result.Saved != 1 {
		close(resume)
		t.Fatalf("discovery was blocked: %+v %v", result, err)
	}
	if result, err := job.RunOperational(ctx); err != nil || result.Saved != 0 || len(client.batches) != 0 {
		close(resume)
		t.Fatal("overlapping refresh was not skipped")
	}
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if repo.maxID != 12 {
		t.Fatal("newer load was not persisted during reconciliation")
	}
	for _, id := range client.throughIDs {
		if id != 10 {
			t.Fatal("reconciliation fetched concurrently discovered records")
		}
	}
}

func TestNewLoadChecksSerializeAndWaitCanCancel(t *testing.T) {
	job := NewSyncLoadsJob(&fakeLoadSyncClient{}, &fakeLoadSyncRepository{}, slog.Default())
	job.newGate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := job.RunNew(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-job.newGate
	if _, err := job.RunNew(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOperationalRefreshBatchesExistingIDs(t *testing.T) {
	client := &fakeLoadSyncClient{}
	repo := &fakeLoadSyncRepository{maxID: 100}
	for id := 1; id <= 61; id++ {
		repo.ids = append(repo.ids, id)
	}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := job.RunOperational(context.Background())
	if err != nil || result.Saved != 61 || repo.maxID != 100 || repo.refreshes != 3 {
		t.Fatalf("result %+v, err %v", result, err)
	}
	if len(client.batches) != 3 || len(client.batches[0]) != 25 || len(client.batches[1]) != 25 || len(client.batches[2]) != 11 {
		t.Fatal(client.batches)
	}
	if len(client.dateColumns) != 0 {
		t.Fatal("operational refresh repeated broad date queries")
	}
}

func TestOperationalWindowUsesNYTodayAndEncodedUTCDates(t *testing.T) {
	for _, tc := range []struct{ now, from, until string }{
		{"2026-10-06T02:00:00Z", "2026-09-28", "2026-10-09"},
		{"2026-10-06T14:00:00Z", "2026-09-29", "2026-10-10"},
		{"2026-11-01T07:00:00Z", "2026-10-25", "2026-11-05"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		from, until := operationalLoadWindow(now)
		if from.Format(time.RFC3339) != tc.from+"T00:00:00Z" || until.Format(time.RFC3339) != tc.until+"T00:00:00Z" {
			t.Fatalf("%s: %s / %s", tc.now, from, until)
		}
	}
}
