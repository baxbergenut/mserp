package jobs

import (
	"context"
	"mserp/internal/fiveeld"
	"time"
)

func (j *SyncFiveELDJob) MaintenanceOdometer(ctx context.Context, vin string) (fiveeld.OdometerReading, error) {
	if j == nil {
		return fiveeld.OdometerReading{}, fiveeld.ErrOdometerUnavailable
	}
	c, ok := j.client.(interface {
		CurrentOdometer(context.Context, string, string, time.Time) (fiveeld.OdometerReading, error)
	})
	if !ok {
		return fiveeld.OdometerReading{}, fiveeld.ErrOdometerUnavailable
	}
	return c.CurrentOdometer(ctx, j.usdot, vin, j.now())
}
