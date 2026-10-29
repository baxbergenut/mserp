package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"mserp/internal/boardroutes"
	"mserp/internal/fiveeld"
)

type BoardTravel struct {
	Source            string              `json:"source,omitempty"`
	Enabled           bool                `json:"enabled"`
	Context           string              `json:"context"`
	CalculatedAt      time.Time           `json:"calculatedAt"`
	AttemptedAt       time.Time           `json:"attemptedAt"`
	ReportedAt        time.Time           `json:"reportedAt"`
	ArrivalAt         time.Time           `json:"arrivalAt"`
	Miles             float64             `json:"miles"`
	Motion            string              `json:"motion"`
	Error             string              `json:"error"`
	Assumptions       string              `json:"assumptions,omitempty"`
	LastKnownPosition bool                `json:"lastKnownPosition,omitempty"`
	Position          *boardRoutePosition `json:"position,omitempty"`
	Mode              string              `json:"mode,omitempty"`
}

// Position belongs to the last successful Google route, never to a skipped poll.
type boardRoutePosition struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

const (
	boardRouteInterval       = 30 * time.Minute
	boardRouteRetryInterval  = 5 * time.Minute
	boardRouteMaxAge         = boardRouteInterval + 5*time.Minute
	boardRouteMovementMeters = 1609.344
)

func (t BoardTravel) moved(lat, lng float64) bool {
	return t.Position == nil || distanceMeters(t.Position.Latitude, t.Position.Longitude, lat, lng) >= boardRouteMovementMeters
}

func (t BoardTravel) due(key string, location *DriverBoardLocation, now time.Time) bool {
	if t.Context != key || !sameTelemetrySource(t.Source, location.Source) || t.Mode != boardroutes.TravelMode {
		return true
	}
	if t.Position != nil && !t.moved(location.Latitude, location.Longitude) {
		return false
	}
	valid := !t.CalculatedAt.IsZero() && !t.CalculatedAt.After(now)
	// Fresh movement can replace a last-known distance immediately after rest.
	if valid && t.LastKnownPosition && now.Sub(location.ReportedAt) <= 10*time.Minute && (t.Error == "" || now.Sub(t.AttemptedAt) >= boardRouteRetryInterval) {
		return true
	}
	if now.Sub(t.AttemptedAt) < boardRouteRetryInterval {
		return false
	}
	return !valid || now.Sub(t.CalculatedAt) >= boardRouteInterval
}

func (t BoardTravel) stale(now, gpsAt time.Time) bool {
	return t.LastKnownPosition || gpsAt.IsZero() || gpsAt.After(now) || now.Sub(gpsAt) > 10*time.Minute || now.Sub(t.CalculatedAt) > boardRouteMaxAge
}

func (t BoardTravel) display(key string, now, gpsAt time.Time) (value, reason string) {
	defer func() {
		if value != "" && t.Assumptions != "" {
			if reason != "" {
				reason += " · "
			}
			reason += t.Assumptions
		}
	}()
	if !t.Enabled {
		return "", "Automatic travel estimates are not enabled"
	}
	if t.Context != key || t.ReportedAt.IsZero() || t.ReportedAt.After(now) || t.CalculatedAt.IsZero() || t.CalculatedAt.After(now) {
		if t.Error != "" {
			return "", t.Error
		}
		return "", "Automatic estimate unavailable or stale"
	}
	if t.stale(now, gpsAt) {
		loc, _ := time.LoadLocation("America/New_York")
		freshness := "GPS data stale"
		if !gpsAt.IsZero() && !gpsAt.After(now) && now.Sub(gpsAt) <= 10*time.Minute {
			freshness = "Route estimate stale"
		}
		reason = "Last known miles remaining · " + freshness + " · last observed " + t.ReportedAt.In(loc).Format("Jan 2, 2006 15:04 MST")
		if t.Error != "" {
			reason += " · " + t.Error
		}
		return fmt.Sprintf("%.0fmi", math.Ceil(t.Miles)), reason
	}
	reason = t.Error
	if reason != "" {
		reason = "Previous estimate (stale refresh): " + reason
	}
	switch t.Motion {
	case "moving":
		if t.Miles > 600 {
			return fmt.Sprintf("%.0fmi", math.Ceil(t.Miles)), reason
		}
		loc, _ := time.LoadLocation("America/New_York")
		return "ETA " + t.ArrivalAt.Round(15*time.Minute).In(loc).Format("15:04"), reason
	case "stopped":
		return fmt.Sprintf("%.0fmi", math.Ceil(t.Miles)), reason
	}
	return "", "Truck movement is not yet established"
}

func trackingMotion(points []fiveeld.TrackingPoint, unit, previous string) (string, *fiveeld.TrackingPoint) {
	var last *fiveeld.TrackingPoint
	var since *fiveeld.TrackingPoint
	motion := previous
	for i := range points {
		p := &points[i]
		if last != nil && !p.Date.After(last.Date) {
			continue
		}
		speed := -1.0
		if p.Speed != nil && !math.IsNaN(*p.Speed) && !math.IsInf(*p.Speed, 0) {
			speed = *p.Speed
			if unit == "kph" {
				speed /= 1.609344
			}
		}
		moved := false
		if last != nil {
			moved = distanceMeters(last.Coordinates.Lat, last.Coordinates.Lng, p.Coordinates.Lat, p.Coordinates.Lng) >= 100
		}
		if speed >= 5 || moved {
			motion = "moving"
			since = nil
		} else if speed >= 0 && speed <= 2 {
			if since == nil || distanceMeters(since.Coordinates.Lat, since.Coordinates.Lng, p.Coordinates.Lat, p.Coordinates.Lng) >= 100 || p.Date.Sub(last.Date) > 10*time.Minute {
				since = p
			}
			if p.Date.Sub(since.Date) >= 3*time.Minute {
				motion = "stopped"
			}
		} else {
			since = nil
		}
		last = p
	}
	return motion, last
}

// ReconcileAutomation is a background-only operation. Provider results are fenced
// against a fresh versioned stop snapshot before they can enter the cache.
func (r *DriverBoardRepository) ReconcileAutomation(ctx context.Context, routes *boardroutes.Client, eld *fiveeld.Client, usdot string) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(718504115)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock(718504115)`)
	}()
	now := r.now().UTC()
	week, _ := time.Parse(time.DateOnly, boardLoadToday(now))
	board, err := r.Get(ctx, week)
	if err != nil {
		return err
	}
	type target struct {
		driver    DriverBoardDriver
		view      BoardLoads
		stop      BoardStop
		travel    BoardTravel
		vin       string
		lastKnown bool
		urgent    bool
	}
	targets := []target{}
	for _, d := range board.Drivers {
		view := board.Loads[d.ID]
		a := view.Automation
		if a == nil {
			continue
		}
		var stop BoardStop
		if view.Current != nil {
			for _, s := range view.Current.Stops {
				if s.Key == view.StopKey {
					stop = s
				}
			}
		}
		evidence := a.Evidence
		if a.Context == "" || a.RequiresConfirmation || !a.TelemetrySafe {
			evidence = ArrivalEvidence{}
		} else {
			evidence = evaluateArrival(evidence, a.Context, d.Location, stop, now)
		}
		body, _ := json.Marshal(evidence)
		_, err = conn.Exec(ctx, `INSERT INTO driver_board_automation(driver_id,evidence) VALUES($1,$2)
   ON CONFLICT(driver_id) DO UPDATE SET evidence=EXCLUDED.evidence,updated_at=now() WHERE driver_board_automation.evidence IS DISTINCT FROM EXCLUDED.evidence`, d.ID, body)
		if err != nil {
			return err
		}
		if a.Context != "" {
			_, err = conn.Exec(ctx, `UPDATE driver_board_automation SET eta_context=$2 WHERE driver_id=$1 AND eta_context=''`, d.ID, a.Context)
			if err != nil {
				return err
			}
		}
		if routes == nil || !routes.Options.Enabled {
			if _, err = conn.Exec(ctx, `UPDATE driver_board_automation SET travel=jsonb_set(travel,'{enabled}','false'::jsonb) WHERE driver_id=$1 AND travel->>'enabled'='true'`, d.ID); err != nil {
				return err
			}
		}
		if routes == nil || !routes.Options.Enabled || a.Context == "" || a.RequiresConfirmation {
			continue
		}
		var t BoardTravel
		var vin string
		if err = conn.QueryRow(ctx, `SELECT travel,coalesce(t.vin,'') FROM driver_board_automation c LEFT JOIN trucks t ON t.id::text=$2 WHERE c.driver_id=$1`, d.ID, d.TruckID).Scan(&t, &vin); err != nil {
			return err
		}
		unavailable := ""
		switch {
		case !a.TelemetrySafe:
			unavailable = "GPS automation requires a unique assigned truck VIN"
		case stop.Latitude == nil || stop.Longitude == nil:
			unavailable = "Selected stop coordinates are unavailable"
		case d.Location == nil:
			unavailable = "Truck GPS location is unavailable"
		case d.Location.ReportedAt.IsZero() || d.Location.ReportedAt.After(now):
			unavailable = "Truck GPS location has an invalid timestamp"
		}
		if unavailable != "" {
			if t.Context != a.Context {
				t = BoardTravel{Context: a.Context}
			}
			t.Enabled, t.Error = true, unavailable
			if !a.TelemetrySafe || stop.Latitude == nil || stop.Longitude == nil {
				t.CalculatedAt = time.Time{}
			}
			// Do not mark this as a route attempt: fresh GPS should queue immediately.
			if err = r.storeBoardTravel(ctx, d.ID, d.TruckID, view.Revision, a.Context, t); err != nil {
				return err
			}
			continue
		}
		lastKnown := now.Sub(d.Location.ReportedAt) > 10*time.Minute
		if !t.due(a.Context, d.Location, now) {
			if !t.Enabled {
				t.Enabled = true
				if err = r.storeBoardTravel(ctx, d.ID, d.TruckID, view.Revision, a.Context, t); err != nil {
					return err
				}
			}
			continue
		}
		urgent := t.Context != a.Context || !sameTelemetrySource(t.Source, d.Location.Source) || t.Mode != boardroutes.TravelMode
		targets = append(targets, target{d, view, stop, t, vin, lastKnown, urgent})
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].urgent != targets[j].urgent {
			return targets[i].urgent
		}
		return targets[i].travel.AttemptedAt.Before(targets[j].travel.AttemptedAt)
	})
	for _, job := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a := job.view.Automation
		t := job.travel
		if job.urgent {
			t = BoardTravel{Context: a.Context, Source: job.driver.Location.Source, Mode: boardroutes.TravelMode}
		}
		t.Enabled = true
		t.AttemptedAt = now
		var point *fiveeld.TrackingPoint
		motion := t.Motion
		if job.lastKnown {
			// Staleness does not establish stopped/ignition state. Route only distance
			// from the shared last-known location, retaining its provider timestamp.
			point = &fiveeld.TrackingPoint{Date: job.driver.Location.ReportedAt}
			point.Coordinates.Lat = job.driver.Location.Latitude
			point.Coordinates.Lng = job.driver.Location.Longitude
		} else if eld == nil {
			t.Error = "Telematics tracking unavailable"
			t.CalculatedAt = time.Time{}
		} else {
			points, e := eld.RecentTracking(ctx, usdot, job.vin, now)
			if e != nil {
				t.Error = "Telematics tracking unavailable"
			} else {
				previous := t.Motion
				if now.Sub(t.ReportedAt) > 10*time.Minute && previous != "stopped" {
					previous = ""
				}
				motion, point = trackingMotion(points, routes.Options.SpeedUnit, previous)
				if point == nil {
					t.Error = "No fresh tracking points"
				}
			}
		}
		// Tracking may be older than the shared GPS cache. Check the actual route
		// origin too, so an unchanged point never produces a paid refresh.
		if point != nil && t.Position != nil && !t.moved(point.Coordinates.Lat, point.Coordinates.Lng) {
			point = nil
		}
		if point != nil {
			estimate, e := routes.Route(ctx, point.Coordinates.Lat, point.Coordinates.Lng, *job.stop.Latitude, *job.stop.Longitude)
			if e != nil {
				t.Error = e.Error()
				if errors.Is(e, boardroutes.ErrRouteUnavailable) {
					t.CalculatedAt = time.Time{}
				}
			} else {
				t.Error, t.Motion = "", motion
				t.ReportedAt, t.CalculatedAt = point.Date, r.now().UTC()
				t.Position = &boardRoutePosition{Latitude: point.Coordinates.Lat, Longitude: point.Coordinates.Lng}
				t.ArrivalAt = time.Time{}
				if !job.lastKnown {
					t.ArrivalAt = point.Date.Add(estimate.Duration)
				}
				t.Miles = float64(estimate.Meters) / 1609.344
				t.Assumptions, t.LastKnownPosition = boardroutes.EstimateAssumptions, job.lastKnown
			}
		}
		if err = r.storeBoardTravel(ctx, job.driver.ID, job.driver.TruckID, job.view.Revision, a.Context, t); err != nil {
			return err
		}
	}
	return nil
}

func (r *DriverBoardRepository) storeBoardTravel(ctx context.Context, id, truck, revision, key string, travel BoardTravel) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// All human board mutations take this same driver lock.
	var currentTruck string
	if err = tx.QueryRow(ctx, `SELECT coalesce((SELECT truck_id::text FROM truck_driver_assignments WHERE driver_id=d.id AND unassigned_at IS NULL),'') FROM drivers d WHERE d.id=$1 FOR UPDATE`, id).Scan(&currentTruck); err != nil {
		return err
	}
	if currentTruck != truck {
		return nil
	}
	views, err := readBoardLoads(ctx, tx, []string{id}, r.loadStartDate(""))
	if err != nil {
		return err
	}
	v := views[id]
	if v.Revision != revision || v.Automation == nil || v.Automation.Context != key || v.Automation.RequiresConfirmation {
		return nil
	}
	body, _ := json.Marshal(travel)
	if _, err = tx.Exec(ctx, `UPDATE driver_board_automation SET travel=$2,updated_at=now() WHERE driver_id=$1 AND travel IS DISTINCT FROM $2::jsonb`, id, body); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
