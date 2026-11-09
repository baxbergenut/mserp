package boardroutes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// ReverseGeocodeBudget is separate from routing: search requests have their own
// durable hard cap. Paid routing never authorizes unbounded search requests.
type ReverseGeocodeBudget interface {
	ReserveReverseGeocode(context.Context, Options) error
}

func (c *Client) waitForProvider(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	now := time.Now()
	at := c.next
	if at.Before(now) {
		at = now
	}
	if at.Before(c.blockedUntil) {
		at = c.blockedUntil
	}
	rpm := c.Options.RequestsPerMinute
	if rpm < 1 {
		rpm = 30
	}
	c.next = at.Add(time.Minute / time.Duration(rpm))
	c.mu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ReverseGeocode resolves one cached fleet coordinate to its municipality and state. The
// background worker controls cache freshness; board reads never call this method.
func (c *Client) ReverseGeocode(ctx context.Context, lat, lng float64) (string, error) {
	if !c.Options.Enabled || strings.TrimSpace(c.Options.TomTomAPIKey) == "" || c.Options.ReverseGeocodeMonthlyLimit <= 0 {
		return "", errors.New("reverse geocoding is not enabled")
	}
	if math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) || lat < -90 || lat > 90 || lng < -180 || lng > 180 || lat == 0 && lng == 0 {
		return "", errors.New("invalid reverse geocoding position")
	}
	budget, ok := c.Budget.(ReverseGeocodeBudget)
	if !ok {
		return "", errors.New("reverse geocoding usage accounting unavailable")
	}
	if err := c.waitForProvider(ctx); err != nil {
		return "", err
	}
	if err := budget.ReserveReverseGeocode(ctx, c.Options); err != nil {
		return "", err
	}
	q := url.Values{"key": {c.Options.TomTomAPIKey}, "language": {"en-US"}}
	endpoint := fmt.Sprintf("%s/%.7f,%.7f.json?%s", c.TomTomReverseURL, lat, lng, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", errors.New("invalid reverse geocoding request")
	}
	// Do not replay a potentially billed GET after a reused connection fails.
	req.Close = true
	// Retain the configured transport/timeout but disallow redirects even when a
	// test or caller supplied its own HTTP client. Never send the key elsewhere.
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("reverse geocoding connection unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		delay := time.Minute
		if res.StatusCode == http.StatusTooManyRequests {
			delay = 5 * time.Minute
		}
		c.mu.Lock()
		until := time.Now().Add(delay)
		if until.After(c.blockedUntil) {
			c.blockedUntil = until
		}
		c.mu.Unlock()
		return "", fmt.Errorf("reverse geocoding returned HTTP %d", res.StatusCode)
	}
	var response struct {
		Addresses []struct {
			Address struct {
				Municipality           string `json:"municipality"`
				CountrySubdivisionCode string `json:"countrySubdivisionCode"`
				CountrySubdivision     string `json:"countrySubdivision"`
			} `json:"address"`
		} `json:"addresses"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&response); err != nil || len(response.Addresses) == 0 {
		return "", errors.New("reverse geocoding address unavailable")
	}
	address := response.Addresses[0].Address
	city := strings.TrimSpace(address.Municipality)
	state := strings.TrimSpace(address.CountrySubdivisionCode)
	if state == "" {
		state = strings.TrimSpace(address.CountrySubdivision)
	}
	// TomTom can return an ISO subdivision code (US-OH) or a short code
	// (OH). Never derive locality from freeformAddress or a county field.
	state = strings.TrimPrefix(state, "US-")
	if city == "" || state == "" || len(city) > 200 || len(state) > 100 ||
		strings.ContainsFunc(address.Municipality, unicode.IsControl) ||
		strings.ContainsFunc(address.CountrySubdivisionCode, unicode.IsControl) ||
		strings.ContainsFunc(address.CountrySubdivision, unicode.IsControl) {
		return "", errors.New("reverse geocoding address unavailable")
	}
	return city + ", " + state, nil
}
