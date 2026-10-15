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

var ErrOdometerUnavailable = errors.New("Five ELD has no recent odometer reading for this truck")

// The tracking API exposes odometer; the v2 location API does not.
// Resolve provider identity by exact VIN, never by a similar unit number.
func (c *Client) CurrentOdometer(ctx context.Context, usdot, vin string, now time.Time) (OdometerReading, error) {
	var units []struct {
		ID  string `json:"id"`
		VIN string `json:"vin"`
	}
	format := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
	query := url.Values{"from": {format(now.Add(-24 * time.Hour))}, "to": {format(now)}}
	if err := c.get(ctx, "/api/externalservice/active-units/"+url.PathEscape(usdot)+"/", query, &units); err != nil {
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
		return latest, ErrOdometerUnavailable
	}
	return latest, nil
}
