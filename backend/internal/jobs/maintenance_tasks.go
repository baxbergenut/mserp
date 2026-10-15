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
	due, err := repo.Due(ctx, "", today)
	if err != nil {
		return err
	}
	live := map[string]repository.MaintenanceReading{}
	if eld != nil {
		telemetryCtx, stopTelemetry := context.WithTimeout(ctx, 40*time.Second)
		defer stopTelemetry()
		ids := []string{}
		seen := map[string]bool{}
		for _, d := range due {
			if !seen[d.TruckID] && d.Schedule != nil && d.Schedule.Enabled && d.Schedule.IntervalMiles != nil && d.LastMiles != nil {
				ids = append(ids, d.TruckID)
				seen[d.TruckID] = true
			}
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		next := 0
		for i := 0; i < min(3, len(ids)); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					mu.Lock()
					if next >= len(ids) {
						mu.Unlock()
						return
					}
					id := ids[next]
					next++
					mu.Unlock()
					bounded, cancel := context.WithTimeout(telemetryCtx, 10*time.Second)
					vin, e := repo.OdometerVIN(bounded, id)
					if e == nil {
						reading, e := eld.MaintenanceOdometer(bounded, vin)
						if e == nil {
							mu.Lock()
							live[id] = repository.MaintenanceReading{Mileage: reading.Mileage, Date: reading.ReportedAt.In(zone).Format(time.DateOnly)}
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
	}
	return repo.RefreshMaintenanceTasks(ctx, today, live)
}
