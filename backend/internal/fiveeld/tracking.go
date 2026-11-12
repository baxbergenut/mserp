package fiveeld

import (
	"context"
	"errors"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	TrackingHistoryWindow = 24 * time.Hour
	historySampleSpacing  = 5 * time.Minute
	historyTrailGap       = 10 * time.Minute
)

type cachedTrackingHistory struct {
	at     time.Time
	points []TrackingPoint
}

type TrackingPoint struct {
	Coordinates struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"coordinates"`
	Speed    *float64  `json:"speed"`
	Date     time.Time `json:"date"`
	Odometer *float64  `json:"odometer"`
}

// Identity lookup shares the existing roster cache used by odometer consumers.
func (c *Client) RecentTracking(ctx context.Context, usdot, vin string, now time.Time) ([]TrackingPoint, error) {
	return c.tracking(ctx, usdot, vin, now, 10*time.Minute)
}

// TrackingHistory serves the Map only. It does not change the ETA worker's tracking window.
func (c *Client) TrackingHistory(ctx context.Context, usdot, vin string, now time.Time) ([]TrackingPoint, error) {
	key := usdot + ":" + strings.ToUpper(strings.TrimSpace(vin))
	c.historyMu.Lock()
	cached, ok := c.history[key]
	c.historyMu.Unlock()
	if ok && !cached.at.After(now) && now.Sub(cached.at) < 2*time.Minute {
		cutoff := now.Add(-TrackingHistoryWindow)
		first := sort.Search(len(cached.points), func(i int) bool { return !cached.points[i].Date.Before(cutoff) })
		return cached.points[first:], nil
	}

	points, err := c.tracking(ctx, usdot, vin, now, TrackingHistoryWindow)
	if errors.Is(err, ErrOdometerUnavailable) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	points = sampleTrackingHistory(points)
	c.historyMu.Lock()
	defer c.historyMu.Unlock()
	if c.history == nil || len(c.history) >= 1024 {
		c.history = make(map[string]cachedTrackingHistory)
	}
	if current := c.history[key]; !current.at.After(now) {
		c.history[key] = cachedTrackingHistory{at: now, points: points}
	}
	return points, nil
}

func sampleTrackingHistory(points []TrackingPoint) []TrackingPoint {
	if len(points) < 2 {
		return points
	}
	samples := []TrackingPoint{points[0]}
	for i := 1; i < len(points); i++ {
		point, previous := points[i], points[i-1]
		lastAt := samples[len(samples)-1].Date
		// Keep the preceding fix so thinning never introduces or widens a GPS gap.
		if point.Date.Sub(lastAt) > historyTrailGap && previous.Date.After(lastAt) {
			samples = append(samples, previous)
			lastAt = previous.Date
		}
		if point.Date.After(lastAt) && (point.Date.Sub(lastAt) >= historySampleSpacing || i == len(points)-1) {
			samples = append(samples, point)
		}
	}
	return samples
}

func (c *Client) tracking(ctx context.Context, usdot, vin string, now time.Time, window time.Duration) ([]TrackingPoint, error) {
	format := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
	rosterQuery := url.Values{"from": {format(now.Add(-24 * time.Hour))}, "to": {format(now)}}
	units, err := c.odometerUnits(ctx, usdot, rosterQuery, now)
	if err != nil {
		return nil, err
	}
	id := ""
	for _, u := range units {
		if strings.EqualFold(strings.TrimSpace(u.VIN), strings.TrimSpace(vin)) {
			if id != "" && id != u.ID {
				return nil, ErrOdometerUnavailable
			}
			id = u.ID
		}
	}
	if id == "" {
		return nil, ErrOdometerUnavailable
	}
	var points []TrackingPoint
	query := url.Values{"from": {format(now.Add(-window))}, "to": {format(now)}}
	if err = c.get(ctx, "/api/externalservice/trackings/"+url.PathEscape(usdot)+"/"+url.PathEscape(id)+"/", query, &points); err != nil {
		return nil, err
	}
	valid := points[:0]
	for _, p := range points {
		if p.Date.After(now) || now.Sub(p.Date) > window || p.Date.IsZero() || (p.Coordinates.Lat == 0 && p.Coordinates.Lng == 0) || math.Abs(p.Coordinates.Lat) > 90 || math.Abs(p.Coordinates.Lng) > 180 || math.IsNaN(p.Coordinates.Lat) || math.IsNaN(p.Coordinates.Lng) {
			continue
		}
		valid = append(valid, p)
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Date.Before(valid[j].Date) })
	return valid, nil
}
