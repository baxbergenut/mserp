package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func TestSyncFiveELDRefreshesValidUnitsWhenProviderTimestampIsNull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"units":[{"truck_number":"3065","vin":"1M8GDM9AXKP042788","coordinates":{"lat":41.88,"lng":-87.62},"timestamp":null},{"truck_number":"17","vin":"1M8GDM9AXKP042789","coordinates":{"lat":40.7,"lng":-74.0},"timestamp":"2026-11-02T14:30:00Z"}]}`)
	}))
	defer server.Close()
	repo := &fakeFiveELDRepo{active: map[string]struct{}{"1M8GDM9AXKP042788": {}, "1M8GDM9AXKP042789": {}}}
	client := fiveeld.NewClientWithHTTPClient(server.URL, "key", "provider", server.Client())
	job := NewSyncFiveELDJob(client, repo, "1", slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := job.Run(context.Background())
	if err != nil || repo.failed != nil {
		t.Fatalf("err=%v failure=%v", err, repo.failed)
	}
	if result.Fetched != 2 || result.Invalid != 1 || result.Saved != 1 {
		t.Fatalf("result=%+v", result)
	}
	if len(repo.stored) != 1 || repo.stored[0].ProviderTruckNumber != "17" || repo.stored[0].ReportedAt.IsZero() {
		t.Fatalf("only the valid GPS observation should be written: %+v", repo.stored)
	}
}

func TestSyncFiveELDHeading(t *testing.T) {
	for _, value := range []float64{-1, 0, 90, 359, 360} {
		repo := &fakeFiveELDRepo{active: map[string]struct{}{}}
		job := NewSyncFiveELDJob(&fakeFiveELDClient{positions: []fiveeld.Position{{VIN: "1M8GDM9AXKP042788", Heading: &value, ReportedAt: time.Now()}}}, repo, "1", slog.New(slog.NewTextHandler(io.Discard, nil)))
		if _, err := job.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := repo.stored[0].Heading
		if value >= 0 && value < 360 {
			if got == nil || *got != value {
				t.Fatalf("heading %v lost", value)
			}
		} else if got != nil {
			t.Fatalf("invalid heading %v accepted", value)
		}
	}
}
