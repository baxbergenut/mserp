package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"mserp/internal/datatruck"
	"mserp/internal/repository"
)

type fakeLoadSyncClient struct {
	afterID     int
	dateColumns []string
	newLoads    []datatruck.Load
	dateLoads   map[string][]datatruck.Load
	throughIDs  []int
	dateErr     error
	dateHook    func()
	newHook     func(int) ([]datatruck.Load, error)
	batches     [][]int
}

func (f *fakeLoadSyncClient) FetchLoadsAfterID(_ context.Context, afterID int) ([]datatruck.Load, error) {
	f.afterID = afterID
	if f.newHook != nil {
		return f.newHook(afterID)
	}
	return f.newLoads, nil
}

func (f *fakeLoadSyncClient) FetchLoadsByDateSinceThroughID(
	_ context.Context,
	column string,
	_ time.Time,
	throughID int,
) ([]datatruck.Load, error) {
	f.dateColumns = append(f.dateColumns, column)
	f.throughIDs = append(f.throughIDs, throughID)
	if f.dateHook != nil {
		f.dateHook()
	}
	return f.dateLoads[column], f.dateErr
}

func (f *fakeLoadSyncClient) FetchLoadsByIDs(_ context.Context, ids []int) ([]datatruck.Load, error) {
	f.batches = append(f.batches, append([]int(nil), ids...))
	loads := []datatruck.Load{}
	for _, id := range ids {
		loads = append(loads, datatruck.Load{ID: id, Status: "delivered"})
	}
	return loads, nil
}

type fakeLoadSyncRepository struct {
	mu        sync.Mutex
	maxID     int
	records   []repository.LoadRecord
	ids       []int
	refreshes int
	upsertErr error
}

func (f *fakeLoadSyncRepository) MaxLoadID(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxID, nil
}

func (f *fakeLoadSyncRepository) UpsertLoads(_ context.Context, records []repository.LoadRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.records = append(f.records, records...)
	for _, record := range records {
		f.maxID = max(f.maxID, record.ID)
	}
	return nil
}

func (f *fakeLoadSyncRepository) RefreshLoads(ctx context.Context, records []repository.LoadRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, records...)
	f.refreshes++
	return nil
}

func (f *fakeLoadSyncRepository) OperationalLoadIDs(context.Context, time.Time, time.Time) ([]int, error) {
	return f.ids, nil
}

func (f *fakeLoadSyncRepository) ReconcileLoads(ctx context.Context, records []repository.LoadRecord) error {
	return f.RefreshLoads(ctx, records)
}

func TestSyncLoadsCombinesNewIDsWithServiceDateReconciliation(t *testing.T) {
	loadAID := "A"
	loadBID := "B"
	client := &fakeLoadSyncClient{
		newLoads: []datatruck.Load{{
			ID: 11, LoadID: &loadAID, Status: "dispatched",
		}},
		dateLoads: map[string][]datatruck.Load{
			"pickup_time": {{
				ID: 9, LoadID: &loadBID, Status: "dispatched",
			}},
			"delivery_time": {{
				ID: 9, LoadID: &loadBID, Status: "invoiced",
			}},
		},
	}
	repo := &fakeLoadSyncRepository{maxID: 10}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	job := NewSyncLoadsJob(client, repo, logger)

	before := time.Now().UTC().AddDate(0, 0, -loadReconciliationLookbackDays)
	result, err := job.Run(context.Background())
	after := time.Now().UTC().AddDate(0, 0, -loadReconciliationLookbackDays)
	if err != nil {
		t.Fatal(err)
	}

	if client.afterID != 10 {
		t.Fatalf("after ID = %d", client.afterID)
	}
	if len(client.dateColumns) != len(loadReconciliationDateColumns) {
		t.Fatalf("date columns = %#v", client.dateColumns)
	}
	for index, column := range loadReconciliationDateColumns {
		if client.throughIDs[index] != 10 {
			t.Fatalf("reconciliation watermark = %d, want 10", client.throughIDs[index])
		}
		if client.dateColumns[index] != column {
			t.Fatalf("date column %d = %q, want %q", index, client.dateColumns[index], column)
		}
	}
	if result.Fetched != 2 || result.Saved != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Since.Before(before) || result.Since.After(after) {
		t.Fatalf("since = %s, expected between %s and %s", result.Since, before, after)
	}
	if len(repo.records) != 2 {
		t.Fatalf("saved records = %d", len(repo.records))
	}

	statusByID := make(map[int]string)
	for _, record := range repo.records {
		statusByID[record.ID] = record.Status
	}
	if statusByID[11] != "dispatched" {
		t.Fatalf("updated load status = %q", statusByID[11])
	}
	if statusByID[9] != "invoiced" {
		t.Fatalf("reconciled load status = %q", statusByID[9])
	}
}

func TestSyncLoadsRetainsUnnumberedOrdersAlongsideOtherLoads(t *testing.T) {
	label := "NUMBERED"
	client := &fakeLoadSyncClient{newLoads: []datatruck.Load{
		{ID: 11, Status: "booked"},
		{ID: 12, LoadID: &label, Status: "invoiced"},
	}}
	repo := &fakeLoadSyncRepository{maxID: 10}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Fetched != 2 || result.Saved != 2 || len(repo.records) != 2 {
		t.Fatalf("result = %+v, records = %d", result, len(repo.records))
	}
	if repo.records[0].ID != 11 || repo.records[0].LoadID != "DataTruck #11" || repo.records[1].LoadID != label {
		t.Fatalf("records = %+v", repo.records)
	}
}

func TestSyncLoadsInitialImportDoesNotRefetchAllDates(t *testing.T) {
	client := &fakeLoadSyncClient{newLoads: []datatruck.Load{{ID: 1}}}
	repo := &fakeLoadSyncRepository{}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.dateColumns) != 0 || len(repo.records) != 1 {
		t.Fatalf("date requests = %v, records = %d", client.dateColumns, len(repo.records))
	}
}

func TestSyncLoadsRejectsMissingUpstreamIdentityWithoutSaving(t *testing.T) {
	client := &fakeLoadSyncClient{newLoads: []datatruck.Load{{ID: 0}, {ID: 1}}}
	repo := &fakeLoadSyncRepository{}
	job := NewSyncLoadsJob(client, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := job.Run(context.Background())
	if !errors.Is(err, repository.ErrMissingLoadRecordID) || !strings.Contains(err.Error(), "load 0") {
		t.Fatalf("error = %v", err)
	}
	if len(repo.records) != 0 {
		t.Fatal("invalid upstream identity must not advance the watermark")
	}
}
