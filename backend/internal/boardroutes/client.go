// Package boardroutes implements budgeted car-route ETA estimates.
package boardroutes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const TravelMode = "DRIVE"

const EstimateAssumptions = "car route estimate; truck restrictions, HOS and breaks are not included"

type Options struct {
	Enabled                    bool
	APIKey                     string
	SpeedUnit                  string
	RequestsPerMinute          int
	TomTomAPIKey               string
	GoogleMonthlyFree          int
	TomTomMonthlyFree          int
	PaidProvider               string
	TomTomBillingTimezone      string
	ReverseGeocodeMonthlyLimit int
}

func LoadOptions() (Options, error) {
	o := Options{Enabled: os.Getenv("BOARD_ROUTES_ENABLED") == "true", RequestsPerMinute: 30, GoogleMonthlyFree: 10000, TomTomMonthlyFree: 20000, PaidProvider: "off", TomTomBillingTimezone: "UTC", ReverseGeocodeMonthlyLimit: 1000}
	if !o.Enabled {
		return o, nil
	}
	o.APIKey = strings.TrimSpace(os.Getenv("BOARD_ROUTES_API_KEY"))
	if o.APIKey == "" {
		o.APIKey = strings.TrimSpace(os.Getenv("GOOGLE_ROUTING_API_TOKEN"))
	}
	o.TomTomAPIKey = strings.TrimSpace(os.Getenv("TOMTOM_ROUTING_API_KEY"))
	if v := os.Getenv("BOARD_ROUTES_TOMTOM_BILLING_TIMEZONE"); v != "" {
		o.TomTomBillingTimezone = v
	}
	if _, err := time.LoadLocation(o.TomTomBillingTimezone); err != nil {
		return o, errors.New("invalid TomTom billing timezone")
	}
	for name, dst := range map[string]*int{"BOARD_ROUTES_GOOGLE_MONTHLY_FREE": &o.GoogleMonthlyFree, "BOARD_ROUTES_TOMTOM_MONTHLY_FREE": &o.TomTomMonthlyFree} {
		if v := os.Getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > *dst {
				return o, fmt.Errorf("invalid %s", name)
			}
			*dst = n
		}
	}
	if v := os.Getenv("BOARD_REVERSE_GEOCODE_MONTHLY_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 1000 {
			return o, errors.New("invalid reverse geocoding monthly limit")
		}
		o.ReverseGeocodeMonthlyLimit = n
	}
	if v := os.Getenv("BOARD_ROUTES_PAID_PROVIDER"); v != "" {
		o.PaidProvider = v
	}
	if o.PaidProvider != "off" && o.PaidProvider != "tomtom" && o.PaidProvider != "google" {
		return o, errors.New("invalid paid routing provider")
	}
	if o.PaidProvider == "tomtom" && o.TomTomAPIKey == "" || o.PaidProvider == "google" && o.APIKey == "" {
		return o, errors.New("paid routing provider requires its API key")
	}
	o.SpeedUnit = os.Getenv("FIVE_ELD_SPEED_UNIT")
	if (o.APIKey == "" && o.TomTomAPIKey == "") || os.Getenv("BOARD_ROUTES_ACCESS_VERIFIED") != "true" || os.Getenv("BOARD_ROUTES_OPERATIONS_VERIFIED") != "true" || os.Getenv("FIVE_ELD_SPEED_VERIFIED") != "true" || (o.SpeedUnit != "mph" && o.SpeedUnit != "kph") {
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
	Options          Options
	HTTP             *http.Client
	URL              string
	TomTomURL        string
	TomTomReverseURL string
	Budget           Budget
	mu               sync.Mutex
	next             time.Time
	blockedUntil     time.Time
}

func NewClient(o Options) *Client {
	return &Client{Options: o, HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, URL: "https://routes.googleapis.com/directions/v2:computeRoutes", TomTomURL: "https://api.tomtom.com/routing/1/calculateRoute", TomTomReverseURL: "https://api.tomtom.com/search/2/reverseGeocode"}
}

type Estimate struct {
	Duration    time.Duration
	Meters      int
	Assumptions string
}

var ErrRouteUnavailable = errors.New("route unavailable")
var ErrTomTomNoRoute = fmt.Errorf("%w: TomTom could not find a drivable route (NO_ROUTE_FOUND)", ErrRouteUnavailable)
var ErrAddressUnmatched = errors.New("Enter a complete destination address for automatic ETA")

func (c *Client) Route(ctx context.Context, lat, lng, toLat, toLng float64) (Estimate, error) {
	return c.route(ctx, lat, lng, toLat, toLng, "")
}

func (c *Client) RouteAddress(ctx context.Context, lat, lng float64, address string) (Estimate, error) {
	return c.route(ctx, lat, lng, 0, 0, address)
}

func (c *Client) route(ctx context.Context, lat, lng, toLat, toLng float64, address string) (Estimate, error) {
	options := c.Options
	if address != "" {
		// Google resolves address waypoints in the same metered route request.
		if options.APIKey == "" {
			return Estimate{}, errors.New("Custom destination routing requires Google Routes")
		}
		options.TomTomAPIKey = ""
	}
	estimate, err := c.routeAttempt(ctx, lat, lng, toLat, toLng, address, options)
	if !errors.Is(err, ErrTomTomNoRoute) || address != "" {
		return estimate, err
	}
	if options.APIKey == "" {
		return Estimate{}, fmt.Errorf("%w; Google fallback is not configured", err)
	}
	// One separately paced/accounted coordinate fallback. Excluding TomTom leaves
	// only Google's remaining allowance or an explicitly selected paid Google plan.
	// In particular, paid TomTom does not authorize paid Google.
	options.TomTomAPIKey = ""
	fallback, fallbackErr := c.routeAttempt(ctx, lat, lng, toLat, toLng, "", options)
	if fallbackErr != nil {
		return Estimate{}, fmt.Errorf("%w; Google fallback unavailable: %w", err, fallbackErr)
	}
	return fallback, nil
}

func (c *Client) routeAttempt(ctx context.Context, lat, lng, toLat, toLng float64, address string, options Options) (Estimate, error) {
	if c.Budget == nil {
		return Estimate{}, errors.New("routing usage accounting unavailable")
	}
	if err := ctx.Err(); err != nil {
		return Estimate{}, err
	}
	if err := c.waitForProvider(ctx); err != nil {
		return Estimate{}, err
	}
	// Commit the reservation before sending; failed/unknown outcomes are not refunded.
	provider, err := c.Budget.Reserve(ctx, options)
	if err != nil {
		return Estimate{}, err
	}
	var req *http.Request
	if provider == "google" {
		point := func(a, b float64) any {
			return map[string]any{"location": map[string]any{"latLng": map[string]float64{"latitude": a, "longitude": b}}}
		}
		destination := point(toLat, toLng)
		fields := "routes.duration,routes.distanceMeters"
		if address != "" {
			destination = map[string]string{"address": address}
			fields += ",geocodingResults.destination"
		}
		body, _ := json.Marshal(map[string]any{"origin": point(lat, lng), "destination": destination, "travelMode": TravelMode, "routingPreference": "TRAFFIC_UNAWARE"})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Goog-Api-Key", c.Options.APIKey)
			req.Header.Set("X-Goog-FieldMask", fields)
		}
	} else if provider == "tomtom" && address == "" {
		q := url.Values{"key": {c.Options.TomTomAPIKey}, "traffic": {"true"}, "departAt": {"now"}, "travelMode": {"car"}, "routeType": {"fastest"}, "routeRepresentation": {"summaryOnly"}, "maxAlternatives": {"0"}}
		endpoint := fmt.Sprintf("%s/%.7f,%.7f:%.7f,%.7f/json?%s", c.TomTomURL, lat, lng, toLat, toLng, q.Encode())
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		// Avoid net/http's automatic GET replay on a failed reused connection.
		if req != nil {
			req.Close = true
		}
	} else {
		return Estimate{}, errors.New("invalid routing provider")
	}
	if err != nil {
		return Estimate{}, errors.New("invalid routing request")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Estimate{}, errors.New("routing connection unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// Only this exact documented code permits fallback. Never expose provider
		// messages, URLs or unknown codes, which can contain credentials/user data.
		if provider == "tomtom" && res.StatusCode == http.StatusBadRequest {
			var failure struct {
				DetailedError struct {
					Code string `json:"code"`
				} `json:"detailedError"`
			}
			if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&failure) == nil && failure.DetailedError.Code == "NO_ROUTE_FOUND" {
				return Estimate{}, ErrTomTomNoRoute
			}
		}
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
	if provider == "tomtom" {
		var response struct {
			Routes []struct {
				Summary struct {
					Seconds *int64 `json:"travelTimeInSeconds"`
					Meters  *int   `json:"lengthInMeters"`
				} `json:"summary"`
			} `json:"routes"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil || len(response.Routes) == 0 {
			return Estimate{}, ErrRouteUnavailable
		}
		summary := response.Routes[0].Summary
		if summary.Seconds == nil || *summary.Seconds < 0 || *summary.Seconds > 31536000 || summary.Meters == nil || *summary.Meters < 0 {
			return Estimate{}, ErrRouteUnavailable
		}
		return Estimate{Duration: time.Duration(*summary.Seconds) * time.Second, Meters: *summary.Meters, Assumptions: "TomTom traffic-aware " + EstimateAssumptions}, nil
	}
	var response struct {
		GeocodingResults struct {
			Destination struct {
				PlaceID      string `json:"placeId"`
				PartialMatch bool   `json:"partialMatch"`
				Status       struct {
					Code int `json:"code"`
				} `json:"geocoderStatus"`
			} `json:"destination"`
		} `json:"geocodingResults"`
		Routes []struct {
			Duration string `json:"duration"`
			Meters   *int   `json:"distanceMeters"`
		} `json:"routes"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil {
		return Estimate{}, ErrRouteUnavailable
	}
	if address != "" {
		match := response.GeocodingResults.Destination
		if match.PartialMatch || match.Status.Code != 0 || match.PlaceID == "" {
			return Estimate{}, ErrAddressUnmatched
		}
	}
	if len(response.Routes) == 0 {
		return Estimate{}, ErrRouteUnavailable
	}
	r := response.Routes[0]
	duration, err := time.ParseDuration(r.Duration)
	if err != nil || duration < 0 || r.Meters == nil || *r.Meters < 0 {
		return Estimate{}, ErrRouteUnavailable
	}
	return Estimate{Duration: duration, Meters: *r.Meters, Assumptions: "Google traffic-unaware " + EstimateAssumptions}, nil
}
