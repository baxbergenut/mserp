// Package boardroutes implements opt-in, traffic-aware car-route ETA estimates.
package boardroutes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const TravelMode = "DRIVE"

const EstimateAssumptions = "Traffic-aware car route estimate; truck restrictions, HOS and breaks are not included"

type Options struct {
	Enabled           bool
	APIKey            string
	SpeedUnit         string
	RequestsPerMinute int
}

func LoadOptions() (Options, error) {
	o := Options{Enabled: os.Getenv("BOARD_ROUTES_ENABLED") == "true", RequestsPerMinute: 30}
	if !o.Enabled {
		return o, nil
	}
	o.APIKey = strings.TrimSpace(os.Getenv("BOARD_ROUTES_API_KEY"))
	if o.APIKey == "" {
		o.APIKey = strings.TrimSpace(os.Getenv("GOOGLE_ROUTING_API_TOKEN"))
	}
	o.SpeedUnit = os.Getenv("FIVE_ELD_SPEED_UNIT")
	if o.APIKey == "" || os.Getenv("BOARD_ROUTES_ACCESS_VERIFIED") != "true" || os.Getenv("BOARD_ROUTES_OPERATIONS_VERIFIED") != "true" || os.Getenv("FIVE_ELD_SPEED_VERIFIED") != "true" || (o.SpeedUnit != "mph" && o.SpeedUnit != "kph") {
		return o, errors.New("board routes requires key, verified access/operations and verified mph/kph speed semantics")
	}
	if v := os.Getenv("BOARD_ROUTES_REQUESTS_PER_MINUTE"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 120 {
			return o, errors.New("invalid board routes request limit")
		}
		o.RequestsPerMinute = n
	}
	return o, nil
}

type Client struct {
	Options      Options
	HTTP         *http.Client
	URL          string
	mu           sync.Mutex
	next         time.Time
	blockedUntil time.Time
}

func NewClient(o Options) *Client {
	return &Client{Options: o, HTTP: &http.Client{Timeout: 15 * time.Second}, URL: "https://routes.googleapis.com/directions/v2:computeRoutes"}
}

type Estimate struct {
	Duration time.Duration
	Meters   int
}

var ErrRouteUnavailable = errors.New("route unavailable")

func (c *Client) Route(ctx context.Context, lat, lng, toLat, toLng float64) (Estimate, error) {
	c.mu.Lock()
	now := time.Now()
	at := c.next
	if at.Before(now) {
		at = now
	}
	if at.Before(c.blockedUntil) {
		at = c.blockedUntil
	}
	c.next = at.Add(time.Minute / time.Duration(c.Options.RequestsPerMinute))
	c.mu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Estimate{}, ctx.Err()
	case <-timer.C:
	}
	point := func(a, b float64) any {
		return map[string]any{"location": map[string]any{"latLng": map[string]float64{"latitude": a, "longitude": b}}}
	}
	body, _ := json.Marshal(map[string]any{"origin": point(lat, lng), "destination": point(toLat, toLng), "travelMode": TravelMode, "routingPreference": "TRAFFIC_AWARE_OPTIMAL"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return Estimate{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", c.Options.APIKey)
	req.Header.Set("X-Goog-FieldMask", "routes.duration,routes.distanceMeters")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Estimate{}, errors.New("routing connection unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		c.mu.Lock()
		delay := time.Minute
		if res.StatusCode == 429 {
			delay = 5 * time.Minute
		}
		c.blockedUntil = time.Now().Add(delay)
		c.mu.Unlock()
		if res.StatusCode < 500 && res.StatusCode != 429 {
			return Estimate{}, fmt.Errorf("%w: HTTP %d", ErrRouteUnavailable, res.StatusCode)
		}
		return Estimate{}, fmt.Errorf("routing returned HTTP %d", res.StatusCode)
	}
	var response struct {
		Routes []struct {
			Duration string `json:"duration"`
			Meters   *int   `json:"distanceMeters"`
		} `json:"routes"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil {
		return Estimate{}, ErrRouteUnavailable
	}
	if len(response.Routes) == 0 {
		return Estimate{}, ErrRouteUnavailable
	}
	r := response.Routes[0]
	duration, err := time.ParseDuration(r.Duration)
	if err != nil || duration < 0 || r.Meters == nil || *r.Meters < 0 {
		return Estimate{}, ErrRouteUnavailable
	}
	return Estimate{Duration: duration, Meters: *r.Meters}, nil
}
