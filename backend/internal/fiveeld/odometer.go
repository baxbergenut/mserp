package fiveeld

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"
)

type OdometerReading struct {
	Mileage    int       `json:"mileage"`
	ReportedAt time.Time `json:"reportedAt"`
}

type odometerUnit struct {
	ID  string `json:"id"`
	VIN string `json:"vin"`
}
type cachedOdometer struct {
	reading OdometerReading
	at      time.Time
	missing bool
}

// Share the identity roster across visible rows, but keep exact unique VIN matching.
func (c *Client) odometerUnits(ctx context.Context, usdot string, query url.Values, now time.Time) ([]odometerUnit, error) {
	c.rosterMu.Lock()
	defer c.rosterMu.Unlock()
	if c.rosterUSDOT == usdot && !c.rosterAt.After(now) && now.Sub(c.rosterAt) < 5*time.Minute {
		return c.roster, nil
	}
	var units []odometerUnit
	if err := c.get(ctx, "/api/externalservice/active-units/"+url.PathEscape(usdot)+"/", query, &units); err != nil {
		return nil, err
	}
	c.roster, c.rosterAt, c.rosterUSDOT = units, now, usdot
	return units, nil
}

var ErrOdometerUnavailable = errors.New("Five ELD has no recent odometer reading for this truck")

// The tracking API exposes odometer; the v2 location API does not.
// Resolve provider identity by exact VIN, never by a similar unit number.
func (c *Client) CurrentOdometer(ctx context.Context, usdot, vin string, now time.Time) (OdometerReading, error) {
	vin = strings.ToUpper(strings.TrimSpace(vin))
	key := usdot + ":" + vin
	c.odometerMu.Lock()
	cached, ok := c.odometers[key]
	c.odometerMu.Unlock()
	if ok && !cached.at.After(now) && now.Sub(cached.at) < time.Minute {
		if cached.missing {
			return OdometerReading{}, ErrOdometerUnavailable
		}
		if !cached.reading.ReportedAt.After(now) && now.Sub(cached.reading.ReportedAt) <= time.Hour {
			return cached.reading, nil
		}
	}
	cache := func(reading OdometerReading, missing bool) {
		c.odometerMu.Lock()
		defer c.odometerMu.Unlock()
		if c.odometers == nil || len(c.odometers) > 4096 {
			c.odometers = map[string]cachedOdometer{}
		}
		c.odometers[key] = cachedOdometer{reading, now, missing}
	}
	format := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
	query := url.Values{"from": {format(now.Add(-24 * time.Hour))}, "to": {format(now)}}
	units, err := c.odometerUnits(ctx, usdot, query, now)
	if err != nil {
		return OdometerReading{}, err
	}
	id := ""
	for _, u := range units {
		if strings.EqualFold(strings.TrimSpace(u.VIN), vin) {
			if id != "" && id != u.ID {
				return OdometerReading{}, ErrOdometerUnavailable
			}
			id = u.ID
		}
	}
	if id == "" {
		cache(OdometerReading{}, true)
		return OdometerReading{}, ErrOdometerUnavailable
	}
	var points []struct {
		Odometer *float64 `json:"odometer"`
		Date     string   `json:"date"`
	}
	if err := c.get(ctx, "/api/externalservice/trackings/"+url.PathEscape(usdot)+"/"+url.PathEscape(id)+"/", query, &points); err != nil {
		return OdometerReading{}, err
	}
	var latest OdometerReading
	for _, p := range points {
		at, err := time.Parse(time.RFC3339Nano, p.Date)
		if err != nil || p.Odometer == nil || math.IsNaN(*p.Odometer) || math.IsInf(*p.Odometer, 0) || *p.Odometer < 0 || *p.Odometer > 2000000000 || at.After(now) || now.Sub(at) > time.Hour {
			continue
		}
		if at.After(latest.ReportedAt) {
			latest = OdometerReading{Mileage: int(math.Floor(*p.Odometer)), ReportedAt: at.UTC()}
		}
	}
	if latest.ReportedAt.IsZero() {
		cache(latest, true)
		return latest, ErrOdometerUnavailable
	}
	cache(latest, false)
	return latest, nil
}
