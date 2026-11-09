package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	"mserp/internal/boardroutes"
)

// Operational cache only: never copied into saved destination/history or ETA.
type BoardReadyLocation struct {
	Context      string              `json:"context"`
	Source       string              `json:"source"`
	Address      string              `json:"address"`
	CalculatedAt time.Time           `json:"calculatedAt"`
	AttemptedAt  time.Time           `json:"attemptedAt"`
	ReportedAt   time.Time           `json:"reportedAt"`
	Position     *boardRoutePosition `json:"position,omitempty"`
}

func readyLocationContext(truck string, requested *time.Time) string {
	body, _ := json.Marshal([]any{"ready-city-state-v1", truck, requested})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (c BoardReadyLocation) due(key string, p *DriverBoardLocation, now time.Time) bool {
	if p == nil || p.Source != "five_eld" || p.ReportedAt.IsZero() || p.ReportedAt.After(now) ||
		math.IsNaN(p.Latitude) || math.IsNaN(p.Longitude) || math.IsInf(p.Latitude, 0) || math.IsInf(p.Longitude, 0) ||
		math.Abs(p.Latitude) > 90 || math.Abs(p.Longitude) > 180 || (p.Latitude == 0 && p.Longitude == 0) {
		return false
	}
	if c.Context != key || !sameTelemetrySource(c.Source, p.Source) {
		return true
	}
	if now.Sub(c.AttemptedAt) < boardRouteRetryInterval {
		return false
	}
	if c.Position == nil || c.CalculatedAt.IsZero() {
		return true
	}
	if !p.ReportedAt.After(c.ReportedAt) {
		return false
	}
	return now.Sub(c.CalculatedAt) >= boardRouteInterval && distanceMeters(c.Position.Latitude, c.Position.Longitude, p.Latitude, p.Longitude) >= boardRouteMovementMeters
}

func (c BoardReadyLocation) stale(now time.Time, gpsAt *time.Time) bool {
	return gpsAt == nil || gpsAt.IsZero() || gpsAt.After(now) || now.Sub(*gpsAt) > 10*time.Minute || c.ReportedAt.IsZero() || c.ReportedAt.After(now) || now.Sub(c.ReportedAt) > boardRouteMaxAge
}

// RefreshReadyLocation is triggered after a committed transition, independent of
// the minute sweep and its global ETA lock. Both runners share a per-driver lock.
func (r *DriverBoardRepository) RefreshReadyLocation(ctx context.Context, routes *boardroutes.Client, id string) error {
	if routes == nil || !routes.Options.Enabled || routes.Options.TomTomAPIKey == "" {
		return nil
	}
	week, _ := time.Parse(time.DateOnly, boardLoadToday(r.now()))
	board, err := r.Get(ctx, week)
	if err != nil {
		return err
	}
	for _, d := range board.Drivers {
		if d.ID == id {
			return r.refreshReadyLocation(ctx, routes, d, board.Loads[id])
		}
	}
	return nil
}

func (r *DriverBoardRepository) refreshReadyLocation(ctx context.Context, routes *boardroutes.Client, d DriverBoardDriver, view BoardLoads) error {
	if routes == nil || !routes.Options.Enabled || routes.Options.TomTomAPIKey == "" || view.Automation == nil || !view.Automation.ready {
		return nil
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(718504117,hashtext($1))`, d.ID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock(718504117,hashtext($1))`, d.ID)
	}()
	// Re-read after locking: a listener may have completed while the sweep queued.
	latest, err := r.Loads(ctx, d.ID, "")
	if err != nil {
		return err
	}
	a := latest.Automation
	if a == nil || !a.ready || !a.TelemetrySafe || a.Context != view.Automation.Context {
		return nil
	}
	now := r.now().UTC()
	cache := a.readyLocation
	if !cache.due(a.Context, d.Location, now) {
		return nil
	}
	if cache.Context != a.Context || !sameTelemetrySource(cache.Source, d.Location.Source) {
		cache = BoardReadyLocation{Context: a.Context, Source: d.Location.Source}
	}
	cache.AttemptedAt = now
	// Persist the attempt before HTTP. A crash/timeout must not cause immediate replay.
	accepted, err := r.storeReadyLocation(ctx, d.ID, d.TruckID, a.Context, cache)
	if err != nil || !accepted {
		return err
	}
	address, lookupErr := routes.ReverseGeocode(ctx, d.Location.Latitude, d.Location.Longitude)
	if lookupErr != nil {
		return nil
	} // Keep last-known address, retry with normal backoff.
	cache.Address = address
	cache.CalculatedAt = r.now().UTC()
	cache.ReportedAt = d.Location.ReportedAt
	cache.Position = &boardRoutePosition{Latitude: d.Location.Latitude, Longitude: d.Location.Longitude}
	_, err = r.storeReadyLocation(ctx, d.ID, d.TruckID, a.Context, cache)
	return err
}

func (r *DriverBoardRepository) storeReadyLocation(ctx context.Context, id, truck, key string, cache BoardReadyLocation) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var currentTruck string
	if err = tx.QueryRow(ctx, `SELECT coalesce((SELECT truck_id::text FROM truck_driver_assignments WHERE driver_id=d.id AND unassigned_at IS NULL),'') FROM drivers d WHERE d.id=$1 FOR UPDATE`, id).Scan(&currentTruck); err != nil {
		return false, err
	}
	if currentTruck != truck {
		return false, nil
	}
	views, err := readBoardLoads(ctx, tx, []string{id}, r.loadStartDate(""), r.now())
	if err != nil {
		return false, err
	}
	a := views[id].Automation
	if a == nil || !a.ready || !a.TelemetrySafe || a.Context != key {
		return false, nil
	}
	body, _ := json.Marshal(cache)
	if _, err = tx.Exec(ctx, `INSERT INTO driver_board_automation(driver_id,ready_location) VALUES($1,$2) ON CONFLICT(driver_id) DO UPDATE SET ready_location=EXCLUDED.ready_location,updated_at=now() WHERE driver_board_automation.ready_location IS DISTINCT FROM EXCLUDED.ready_location`, id, body); err != nil {
		return false, err
	}
	err = tx.Commit(ctx)
	return err == nil, err
}
