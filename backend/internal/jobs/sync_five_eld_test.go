package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"mserp/internal/fiveeld"
	"mserp/internal/repository"
)

type fakeFiveELDClient struct {
	positions []fiveeld.Position
	err       error
}

func (f *fakeFiveELDClient) CurrentPositions(context.Context, string) ([]fiveeld.Position, error) {
	return f.positions, f.err
}

type fakeFiveELDRepo struct {
	active map[string]struct{}
	stored []repository.FiveELDLocation
	failed error
}

func (f *fakeFiveELDRepo) ActiveTruckVINs(context.Context) (map[string]struct{}, error) {
	return f.active, nil
}
func (f *fakeFiveELDRepo) StoreLocations(_ context.Context, values []repository.FiveELDLocation, _, _ []string, _ int, _ repository.FiveELDSyncResult) error {
	f.stored = values
	return nil
}
func (f *fakeFiveELDRepo) RecordFailure(_ context.Context, _ time.Time, err error) error {
	f.failed = err
	return nil
}

func TestSyncFiveELDMatchesVINAndStoresCurrentCoordinates(t *testing.T) {
	reported := time.Date(2026, 10, 5, 14, 30, 0, 0, time.UTC)
	client := &fakeFiveELDClient{
		positions: []fiveeld.Position{{VIN: "1m8g-dm9axkp042788", TruckNumber: "17", Latitude: 41.88, Longitude: -87.62, ReportedAt: reported}},
	}
	repo := &fakeFiveELDRepo{active: map[string]struct{}{"1M8GDM9AXKP042788": {}}}
	job := NewSyncFiveELDJob(client, repo, "2812942", slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := reported.Add(time.Minute)
	job.now = func() time.Time { return now }
	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Saved != 1 || len(repo.stored) != 1 || repo.stored[0].Latitude != 41.88 || repo.stored[0].Longitude != -87.62 {
		t.Fatalf("result=%+v stored=%+v", result, repo.stored)
	}
}

func TestSyncFiveELDPreservesCacheOnProviderFailure(t *testing.T) {
	providerErr := errors.New("provider unavailable")
	repo := &fakeFiveELDRepo{active: map[string]struct{}{}}
	job := NewSyncFiveELDJob(&fakeFiveELDClient{err: providerErr}, repo, "2812942", slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := job.Run(context.Background())
	if !errors.Is(err, providerErr) || !errors.Is(repo.failed, providerErr) || repo.stored != nil {
		t.Fatalf("err=%v failure=%v stored=%v", err, repo.failed, repo.stored)
	}
}

func TestNormalizeELDVIN(t *testing.T) {
	if vin, ok := normalizeELDVIN(" 1m8g-dm9axkp042788 "); !ok || vin != "1M8GDM9AXKP042788" {
		t.Fatalf("vin=%q ok=%v", vin, ok)
	}
	if _, ok := normalizeELDVIN("short"); ok {
		t.Fatal("short VIN accepted")
	}
}
