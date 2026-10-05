package jobs

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"mserp/internal/fiveeld"
	"mserp/internal/repository"
)

var ErrFiveELDSyncInProgress = errors.New("Five ELD refresh is already in progress")

type fiveELDClient interface {
	CurrentPositions(context.Context, string) ([]fiveeld.Position, error)
}

type fiveELDRepository interface {
	ActiveTruckVINs(context.Context) (map[string]struct{}, error)
	StoreLocations(context.Context, []repository.FiveELDLocation, []string, []string, int, time.Duration, repository.FiveELDSyncResult) error
	RecordFailure(context.Context, time.Time, time.Duration, error) error
}

type SyncFiveELDJob struct {
	client     fiveELDClient
	repo       fiveELDRepository
	usdot      string
	staleAfter time.Duration
	logger     *slog.Logger
	running    atomic.Bool
	now        func() time.Time
}

func NewSyncFiveELDJob(client fiveELDClient, repo fiveELDRepository, usdot string, staleAfter time.Duration, logger *slog.Logger) *SyncFiveELDJob {
	return &SyncFiveELDJob{client: client, repo: repo, usdot: usdot, staleAfter: staleAfter, logger: logger, now: time.Now}
}

func (j *SyncFiveELDJob) Run(ctx context.Context) (repository.FiveELDSyncResult, error) {
	if !j.running.CompareAndSwap(false, true) {
		return repository.FiveELDSyncResult{}, ErrFiveELDSyncInProgress
	}
	defer j.running.Store(false)
	now := j.now().UTC()
	fail := func(err error) (repository.FiveELDSyncResult, error) {
		if recordErr := j.repo.RecordFailure(ctx, now, j.staleAfter, err); recordErr != nil {
			j.logger.Error("record Five ELD sync failure", "error", recordErr)
		}
		return repository.FiveELDSyncResult{}, err
	}
	activeVINs, err := j.repo.ActiveTruckVINs(ctx)
	if err != nil {
		return fail(err)
	}
	positions, err := j.client.CurrentPositions(ctx, j.usdot)
	if err != nil {
		return fail(err)
	}

	invalid := 0
	byVIN := map[string][]fiveeld.Position{}
	for _, position := range positions {
		vin, ok := normalizeELDVIN(position.VIN)
		if !ok || !validCoordinate(position.Latitude, position.Longitude) || position.ReportedAt.IsZero() {
			invalid++
			continue
		}
		position.VIN = vin
		byVIN[vin] = append(byVIN[vin], position)
	}
	ambiguousSet := map[string]struct{}{}
	for vin, values := range byVIN {
		if len(values) > 1 {
			ambiguousSet[vin] = struct{}{}
		}
	}
	acceptedVINs := make([]string, 0, len(byVIN))
	for vin := range byVIN {
		if _, duplicate := ambiguousSet[vin]; !duplicate {
			acceptedVINs = append(acceptedVINs, vin)
		}
	}
	sort.Strings(acceptedVINs)

	locations := make([]repository.FiveELDLocation, 0, len(acceptedVINs))
	for _, vin := range acceptedVINs {
		if _, ambiguous := ambiguousSet[vin]; ambiguous {
			continue
		}
		position := byVIN[vin][0]
		locations = append(locations, repository.FiveELDLocation{VIN: vin, ProviderTruckNumber: position.TruckNumber,
			Latitude: position.Latitude, Longitude: position.Longitude, ReportedAt: position.ReportedAt, FetchedAt: now})
	}

	unmatched := []string{}
	for vin := range byVIN {
		if _, local := activeVINs[vin]; !local {
			unmatched = append(unmatched, vin)
		}
	}
	ambiguous := make([]string, 0, len(ambiguousSet))
	for vin := range ambiguousSet {
		ambiguous = append(ambiguous, vin)
	}
	sort.Strings(unmatched)
	sort.Strings(ambiguous)
	result := repository.FiveELDSyncResult{Fetched: len(positions), Saved: len(locations), Unmatched: len(unmatched),
		Ambiguous: len(ambiguous), Invalid: invalid, SyncedAt: now}
	if err = j.repo.StoreLocations(ctx, locations, unmatched, ambiguous, invalid, j.staleAfter, result); err != nil {
		return fail(err)
	}
	j.logger.Info("Five ELD sync complete", "fetched", result.Fetched, "saved", result.Saved, "unmatched", result.Unmatched,
		"ambiguous", result.Ambiguous, "invalid", result.Invalid)
	return result, nil
}

func normalizeELDVIN(value string) (string, bool) {
	var normalized strings.Builder
	for _, char := range strings.ToUpper(strings.TrimSpace(value)) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			normalized.WriteRune(char)
		}
	}
	result := normalized.String()
	return result, len(result) == 17
}

func validCoordinate(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsNaN(longitude) && !math.IsInf(latitude, 0) && !math.IsInf(longitude, 0) && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}
