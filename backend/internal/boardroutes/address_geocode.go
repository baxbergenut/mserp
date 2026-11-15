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

var ErrAddressUnmatched = errors.New("Destination not found; enter a city and state or a street address for automatic ETA")
var ErrAddressGeocodeBudgetExhausted = errors.New("monthly destination geocoding request limit reached")

const AddressGeocodeProfile = "address-or-city-v2"

type Destination struct {
	Waypoint
	City bool `json:"city,omitempty"`
}

type geocodeResult struct {
	Type       string `json:"type"`
	EntityType string `json:"entityType"`
	Match      struct {
		Score float64 `json:"score"`
	} `json:"matchConfidence"`
	Address struct {
		Municipality string `json:"municipality"`
		Subdivision  string `json:"municipalitySubdivision"`
		State        string `json:"countrySubdivision"`
		StateName    string `json:"countrySubdivisionName"`
	} `json:"address"`
	Position struct {
		Latitude  *float64 `json:"lat"`
		Longitude *float64 `json:"lon"`
	} `json:"position"`
}

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
func (c *Client) GeocodeAddress(ctx context.Context, address string) (Destination, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return Destination{}, ErrAddressUnmatched
	}
	results, err := c.geocodeDestination(ctx, address, false)
	if err != nil {
		return Destination{}, err
	}
	// Prefer an exact street address; otherwise use a uniquely matched locality.
	for _, city := range []bool{false, true} {
		point, found, err := matchDestination(results, address, city)
		if err != nil || found {
			return point, err
		}
	}
	// Street-name matches can crowd out cities (e.g. Logan Township, NJ).
	// A separately metered locality-only search keeps those streets out.
	results, err = c.geocodeDestination(ctx, address, true)
	if err != nil {
		return Destination{}, err
	}
	point, found, err := matchDestination(results, address, true)
	if err != nil {
		return Destination{}, err
	}
	if !found {
		return Destination{}, ErrAddressUnmatched
	}
	return point, nil
}

func (c *Client) geocodeDestination(ctx context.Context, address string, cityOnly bool) ([]geocodeResult, error) {
	budget, ok := c.Budget.(addressGeocodeBudget)
	if !ok {
		return nil, errors.New("destination geocoding usage accounting unavailable")
	}
	if err := c.waitForProvider(ctx); err != nil {
		return nil, err
	}
	if err := budget.ReserveAddressGeocode(ctx, c.Options); err != nil {
		return nil, err
	}
	q := url.Values{"key": {c.Options.TomTomAPIKey}, "limit": {"2"}}
	if cityOnly {
		q.Set("entityTypeSet", "Municipality,MunicipalitySubdivision")
	}
	endpoint := c.TomTomGeocodeURL + "/" + url.PathEscape(address) + ".json?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid destination geocoding request")
	}
	req.Close = true
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("destination geocoding connection unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		c.pauseAfterHTTPError(res.StatusCode)
		return nil, fmt.Errorf("destination geocoding returned HTTP %d", res.StatusCode)
	}
	var response struct {
		Results []geocodeResult `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&response); err != nil || response.Results == nil {
		return nil, errors.New("destination geocoding response unavailable")
	}
	return response.Results, nil
}

func matchDestination(results []geocodeResult, address string, city bool) (Destination, bool, error) {
	var match *Destination
	for _, result := range results {
		if city {
			if result.Type != "Geography" || (result.EntityType != "Municipality" && result.EntityType != "MunicipalitySubdivision") {
				continue
			}
			if result.Match.Score < 1 && !matchesCityAndState(address, result) {
				continue
			}
		} else if result.Type != "Point Address" || result.Match.Score < 1 {
			continue
		}
		lat, lng := result.Position.Latitude, result.Position.Longitude
		if lat == nil || lng == nil || !validCoordinate(*lat, *lng) {
			return Destination{}, false, errors.New("destination geocoding response unavailable")
		}
		point := Destination{Waypoint: Waypoint{Latitude: *lat, Longitude: *lng}, City: city}
		if match != nil && *match != point {
			return Destination{}, false, ErrAddressUnmatched
		}
		match = &point
	}
	if match == nil {
		return Destination{}, false, nil
	}
	return *match, true, nil
}

// TomTom names US townships without the "Township" suffix. Require both the
// locality and state to match before accepting that lower-confidence alias.
func matchesCityAndState(query string, result geocodeResult) bool {
	parts := strings.Split(query, ",")
	if len(parts) != 2 {
		return false
	}
	normalize := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	city := strings.TrimSuffix(normalize(parts[0]), " township")
	state := normalize(parts[1])
	if city == "" || state == "" || (state != normalize(result.Address.State) && state != normalize(result.Address.StateName)) {
		return false
	}
	return city == strings.TrimSuffix(normalize(result.Address.Municipality), " township") || city == strings.TrimSuffix(normalize(result.Address.Subdivision), " township")
}
