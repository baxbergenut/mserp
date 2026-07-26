package jobs

import (
	"context"
	"io"
	"log/slog"
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
}

func (f *fakeLoadSyncClient) FetchLoadsAfterID(_ context.Context, afterID int) ([]datatruck.Load, error) {
	f.afterID = afterID
	return f.newLoads, nil
}

func (f *fakeLoadSyncClient) FetchLoadsByDateSince(
	_ context.Context,
	column string,
	_ time.Time,
) ([]datatruck.Load, error) {
	f.dateColumns = append(f.dateColumns, column)
	return f.dateLoads[column], nil
}

type fakeLoadSyncRepository struct {
	maxID   int
	records []repository.LoadRecord
}

func (f *fakeLoadSyncRepository) MaxLoadID(context.Context) (int, error) {
	return f.maxID, nil
}

func (f *fakeLoadSyncRepository) UpsertLoads(_ context.Context, records []repository.LoadRecord) error {
	f.records = append([]repository.LoadRecord(nil), records...)
	return nil
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
				ID: 9, LoadID: &loadBID, Status: "invoiced",
			}},
			"delivery_time": {{
				ID: 11, LoadID: &loadAID, Status: "invoiced",
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
	if statusByID[11] != "invoiced" {
		t.Fatalf("updated load status = %q", statusByID[11])
	}
	if statusByID[9] != "invoiced" {
		t.Fatalf("reconciled load status = %q", statusByID[9])
	}
}
