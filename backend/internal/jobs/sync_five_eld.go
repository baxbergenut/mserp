package jobs

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"mserp/internal/fiveeld"
	"mserp/internal/repository"
)

var ErrFiveELDSyncInProgress = errors.New("Five ELD refresh is already in progress")

type fiveELDClient interface {
	CurrentPositions(context.Context, string) ([]fiveeld.Position, error)
	ActiveUnits(context.Context, string) ([]fiveeld.Unit, error)
	LatestTracking(context.Context, string, string, time.Time, time.Time) (fiveeld.Tracking, bool, error)
}

type fiveELDRepository interface {
	ActiveTruckVINs(context.Context) (map[string]struct{}, error)
	Locations(context.Context, []string) (map[string]repository.FiveELDLocation, error)
	StoreLocations(context.Context, []repository.FiveELDLocation, []string, []string, int, time.Duration, repository.FiveELDSyncResult) error
	RecordFailure(context.Context, time.Time, time.Duration, error) error
}

type SyncFiveELDJob struct {
	client         fiveELDClient
	repo           fiveELDRepository
	usdot          string
	staleAfter     time.Duration
	addressRefresh time.Duration
	maxAddresses   int
	logger         *slog.Logger
	running        atomic.Bool
	now            func() time.Time
}

func NewSyncFiveELDJob(client fiveELDClient, repo fiveELDRepository, usdot string, staleAfter, addressRefresh time.Duration, maxAddresses int, logger *slog.Logger) *SyncFiveELDJob {
	return &SyncFiveELDJob{client: client, repo: repo, usdot: usdot, staleAfter: staleAfter,
		addressRefresh: addressRefresh, maxAddresses: maxAddresses, logger: logger, now: time.Now}
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
	existing, err := j.repo.Locations(ctx, acceptedVINs)
	if err != nil {
		return fail(err)
	}

	type candidate struct {
		vin     string
		missing bool
		updated time.Time
	}
	candidates := []candidate{}
	for _, vin := range acceptedVINs {
		if _, active := activeVINs[vin]; !active {
			continue
		}
		old := existing[vin]
		due := old.AddressUpdatedAt == nil || now.Sub(*old.AddressUpdatedAt) >= j.addressRefresh
		if due {
			candidates = append(candidates, candidate{vin: vin, missing: old.Address == "", updated: pointerTime(old.AddressUpdatedAt)})
		}
	}
	sort.Slice(candidates, func(a, b int) bool {
		if candidates[a].missing != candidates[b].missing {
			return candidates[a].missing
		}
		return candidates[a].updated.Before(candidates[b].updated)
	})
	if len(candidates) > j.maxAddresses {
		candidates = candidates[:j.maxAddresses]
	}

	unitByVIN := map[string]fiveeld.Unit{}
	if len(candidates) > 0 {
		units, fetchErr := j.client.ActiveUnits(ctx, j.usdot)
		if fetchErr != nil {
			return fail(fetchErr)
		}
		for _, unit := range units {
			vin, ok := normalizeELDVIN(unit.VIN)
			if !ok || unit.ID == "" {
				continue
			}
			if _, exists := unitByVIN[vin]; exists {
				ambiguousSet[vin] = struct{}{}
				delete(unitByVIN, vin)
				continue
			}
			if _, ambiguous := ambiguousSet[vin]; !ambiguous {
				unit.VIN = vin
				unitByVIN[vin] = unit
			}
		}
	}

	locations := make([]repository.FiveELDLocation, 0, len(acceptedVINs))
	locationIndex := map[string]int{}
	for _, vin := range acceptedVINs {
		if _, ambiguous := ambiguousSet[vin]; ambiguous {
			continue
		}
		position := byVIN[vin][0]
		locationIndex[vin] = len(locations)
		locations = append(locations, repository.FiveELDLocation{VIN: vin, ProviderTruckNumber: position.TruckNumber,
			Latitude: position.Latitude, Longitude: position.Longitude, ReportedAt: position.ReportedAt, FetchedAt: now})
	}

	var wait sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	var resultMu sync.Mutex
	addressLookups := 0
	for _, item := range candidates {
		unit, ok := unitByVIN[item.vin]
		index, stored := locationIndex[item.vin]
		if !ok || !stored || hasVIN(ambiguousSet, item.vin) {
			continue
		}
		wait.Add(1)
		go func(index int, unit fiveeld.Unit) {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			position := locations[index]
			point, found, lookupErr := j.client.LatestTracking(ctx, j.usdot, unit.ID, position.ReportedAt.Add(-6*time.Hour), position.ReportedAt.Add(5*time.Minute))
			resultMu.Lock()
			addressLookups++
			if lookupErr != nil {
				j.logger.Warn("Five ELD address lookup failed", "vin", unit.VIN, "error", lookupErr)
			} else {
				locations[index].AddressUpdatedAt = &now
				if found && strings.TrimSpace(point.Address) != "" {
					locations[index].Address = strings.TrimSpace(point.Address)
					locations[index].AddressReportedAt = &point.ReportedAt
				}
			}
			resultMu.Unlock()
		}(index, unit)
	}
	wait.Wait()

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
		Ambiguous: len(ambiguous), Invalid: invalid, AddressLookups: addressLookups, SyncedAt: now}
	if err = j.repo.StoreLocations(ctx, locations, unmatched, ambiguous, invalid, j.staleAfter, result); err != nil {
		return fail(err)
	}
	j.logger.Info("Five ELD sync complete", "fetched", result.Fetched, "saved", result.Saved, "unmatched", result.Unmatched,
		"ambiguous", result.Ambiguous, "invalid", result.Invalid, "address_lookups", result.AddressLookups)
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

func pointerTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
func hasVIN(values map[string]struct{}, vin string) bool { _, ok := values[vin]; return ok }
