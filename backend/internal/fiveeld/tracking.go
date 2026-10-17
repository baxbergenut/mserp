package fiveeld

import (
	"context"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

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
	query := url.Values{"from": {format(now.Add(-10 * time.Minute))}, "to": {format(now)}}
	if err = c.get(ctx, "/api/externalservice/trackings/"+url.PathEscape(usdot)+"/"+url.PathEscape(id)+"/", query, &points); err != nil {
		return nil, err
	}
	valid := points[:0]
	for _, p := range points {
		if p.Date.After(now) || now.Sub(p.Date) > 10*time.Minute || p.Date.IsZero() || (p.Coordinates.Lat == 0 && p.Coordinates.Lng == 0) || math.Abs(p.Coordinates.Lat) > 90 || math.Abs(p.Coordinates.Lng) > 180 || math.IsNaN(p.Coordinates.Lat) || math.IsNaN(p.Coordinates.Lng) {
			continue
		}
		valid = append(valid, p)
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Date.Before(valid[j].Date) })
	return valid, nil
}
