package boardroutes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrAddressUnmatched = errors.New("Enter a complete destination address for automatic ETA")
var ErrAddressGeocodeBudgetExhausted = errors.New("monthly destination geocoding request limit reached")

type addressGeocodeBudget interface {
	ReserveAddressGeocode(context.Context, Options) error
}

func (b *PostgresBudget) ReserveAddressGeocode(ctx context.Context, o Options) error {
	err := b.reserveTomTom(ctx, o, time.Time{}, "tomtom_geocode", o.AddressGeocodeMonthlyLimit, false, ErrAddressGeocodeBudgetExhausted)
	if err != nil && !errors.Is(err, ErrAddressGeocodeBudgetExhausted) {
		return errors.New("destination geocoding usage accounting unavailable")
	}
	return err
}

// GeocodeAddress resolves a dispatcher-entered destination before truck routing.
func (c *Client) GeocodeAddress(ctx context.Context, address string) (Waypoint, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return Waypoint{}, ErrAddressUnmatched
	}
	budget, ok := c.Budget.(addressGeocodeBudget)
	if !ok {
		return Waypoint{}, errors.New("destination geocoding usage accounting unavailable")
	}
	if err := c.waitForProvider(ctx); err != nil {
		return Waypoint{}, err
	}
	if err := budget.ReserveAddressGeocode(ctx, c.Options); err != nil {
		return Waypoint{}, err
	}
	q := url.Values{"key": {c.Options.TomTomAPIKey}, "limit": {"2"}}
	endpoint := c.TomTomGeocodeURL + "/" + url.PathEscape(address) + ".json?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Waypoint{}, errors.New("invalid destination geocoding request")
	}
	req.Close = true
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Waypoint{}, errors.New("destination geocoding connection unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		c.pauseAfterHTTPError(res.StatusCode)
		return Waypoint{}, fmt.Errorf("destination geocoding returned HTTP %d", res.StatusCode)
	}
	var response struct {
		Results []struct {
			Type  string `json:"type"`
			Match struct {
				Score float64 `json:"score"`
			} `json:"matchConfidence"`
			Position struct {
				Latitude  *float64 `json:"lat"`
				Longitude *float64 `json:"lon"`
			} `json:"position"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&response); err != nil || response.Results == nil {
		return Waypoint{}, errors.New("destination geocoding response unavailable")
	}
	// Require a complete point address and reject equally confident alternatives.
	var match *Waypoint
	for _, result := range response.Results {
		if result.Type != "Point Address" || result.Match.Score < 1 {
			continue
		}
		lat, lng := result.Position.Latitude, result.Position.Longitude
		if lat == nil || lng == nil || !validCoordinate(*lat, *lng) {
			return Waypoint{}, errors.New("destination geocoding response unavailable")
		}
		point := Waypoint{Latitude: *lat, Longitude: *lng}
		if match != nil && *match != point {
			return Waypoint{}, ErrAddressUnmatched
		}
		match = &point
	}
	if match == nil {
		return Waypoint{}, ErrAddressUnmatched
	}
	return *match, nil
}
