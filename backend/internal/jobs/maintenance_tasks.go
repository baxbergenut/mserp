package jobs

import (
	"context"
	"sync"
	"time"

	"mserp/internal/repository"
)

// Share the existing exact-VIN, one-hour-freshness telemetry reader. Bound
// vendor concurrency; missing readings leave dated manual/service data intact.
func RefreshMaintenanceTasks(ctx context.Context, repo *repository.MaintenanceRepository, eld *SyncFiveELDJob) error {
	zone, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(zone)
	today, _ := time.Parse(time.DateOnly, now.Format(time.DateOnly))
	trucks, err := repo.MaintenanceTelemetryCandidates(ctx)
	if err != nil {
		return err
	}
	if eld != nil {
		telemetryCtx, stopTelemetry := context.WithTimeout(ctx, 40*time.Second)
		defer stopTelemetry()
		var mu sync.Mutex
		var wg sync.WaitGroup
		var storeErr error
		next := 0
		for i := 0; i < min(3, len(trucks)); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					mu.Lock()
					if next >= len(trucks) {
						mu.Unlock()
						return
					}
					truck := trucks[next]
					next++
					mu.Unlock()
					bounded, cancel := context.WithTimeout(telemetryCtx, 10*time.Second)
					var value *repository.MaintenanceTelemetry
					reading, e := eld.MaintenanceOdometer(bounded, truck.VIN)
					if e == nil {
						value = &repository.MaintenanceTelemetry{Mileage: reading.Mileage, ReportedAt: reading.ReportedAt}
					}
					if telemetryCtx.Err() == nil {
						if e := repo.StoreMaintenanceTelemetry(telemetryCtx, truck, value); e != nil {
							mu.Lock()
							if storeErr == nil {
								storeErr = e
							}
							mu.Unlock()
						}
					}
					cancel()
					if telemetryCtx.Err() != nil {
						return
					}
				}
			}()
		}
		wg.Wait()
		if storeErr != nil {
			return storeErr
		}
	}
	return repo.RefreshMaintenanceTasks(ctx, today, nil)
}
