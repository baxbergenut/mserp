// Package boardroutes implements budgeted commercial truck ETA estimates.
package boardroutes

import (
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

// RouteProfile versions the cached estimates and geometry for the selected vehicle.
const RouteProfile = "truck-791-v1"

const EstimateAssumptions = "TomTom traffic-aware truck route (VRC 791), excluding HOS and breaks"

type Options struct {
	Enabled                    bool
	SpeedUnit                  string
	RequestsPerMinute          int
	TomTomAPIKey               string
	TomTomMonthlyFree          int
	PaidProvider               string
	TomTomBillingTimezone      string
	ReverseGeocodeMonthlyLimit int
	AddressGeocodeMonthlyLimit int
}

func LoadOptions() (Options, error) {
	o := Options{Enabled: os.Getenv("BOARD_ROUTES_ENABLED") == "true", RequestsPerMinute: 30, TomTomMonthlyFree: 20000, PaidProvider: "off", TomTomBillingTimezone: "UTC", ReverseGeocodeMonthlyLimit: 1000, AddressGeocodeMonthlyLimit: 1000}
	if !o.Enabled {
		return o, nil
	}
	o.TomTomAPIKey = strings.TrimSpace(os.Getenv("TOMTOM_ROUTING_API_KEY"))
	if v := os.Getenv("BOARD_ROUTES_TOMTOM_BILLING_TIMEZONE"); v != "" {
		o.TomTomBillingTimezone = v
	}
	if _, err := time.LoadLocation(o.TomTomBillingTimezone); err != nil {
		return o, errors.New("invalid TomTom billing timezone")
	}
	for name, dst := range map[string]*int{
		"BOARD_ROUTES_TOMTOM_MONTHLY_FREE":    &o.TomTomMonthlyFree,
		"BOARD_REVERSE_GEOCODE_MONTHLY_LIMIT": &o.ReverseGeocodeMonthlyLimit,
		"BOARD_ADDRESS_GEOCODE_MONTHLY_LIMIT": &o.AddressGeocodeMonthlyLimit,
	} {
		if v := os.Getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > *dst {
				return o, fmt.Errorf("invalid %s", name)
			}
			*dst = n
		}
	}
	if v := os.Getenv("BOARD_ROUTES_PAID_PROVIDER"); v != "" {
		o.PaidProvider = v
	}
	if o.PaidProvider != "off" && o.PaidProvider != "tomtom" {
		return o, errors.New("invalid paid routing provider")
	}
	o.SpeedUnit = os.Getenv("FIVE_ELD_SPEED_UNIT")
	if o.TomTomAPIKey == "" || os.Getenv("BOARD_ROUTES_ACCESS_VERIFIED") != "true" || os.Getenv("BOARD_ROUTES_OPERATIONS_VERIFIED") != "true" || os.Getenv("FIVE_ELD_SPEED_VERIFIED") != "true" || (o.SpeedUnit != "mph" && o.SpeedUnit != "kph") {
		return o, errors.New("board routes requires a TomTom key, verified access/operations and verified mph/kph speed semantics")
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
	TomTomURL        string
	TomTomReverseURL string
	TomTomGeocodeURL string
	Budget           Budget
	mu               sync.Mutex
	next             time.Time
	blockedUntil     time.Time
}

func NewClient(o Options) *Client {
	return &Client{Options: o, HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, TomTomURL: "https://api.tomtom.com/routing/1/calculateRoute", TomTomReverseURL: "https://api.tomtom.com/search/2/reverseGeocode", TomTomGeocodeURL: "https://api.tomtom.com/search/2/geocode"}
}

type Estimate struct {
	Duration          time.Duration
	Meters            int
	Assumptions       string
	Geometry          *RouteGeometry
	Following         []RouteLeg
	FollowingRejected bool
}

type Waypoint struct{ Latitude, Longitude float64 }

type RouteLeg struct {
	Meters   int
	Geometry *RouteGeometry
}

type routeSummary struct {
	Seconds *int64 `json:"travelTimeInSeconds"`
	Meters  *int   `json:"lengthInMeters"`
}

type RouteGeometry struct {
	Polyline  string `json:"polyline"`
	Precision int    `json:"precision"`
}

var ErrRouteUnavailable = errors.New("route unavailable")
var ErrTomTomNoRoute = fmt.Errorf("%w: TomTom could not find a truck route for VRC 791", ErrRouteUnavailable)

func (c *Client) Route(ctx context.Context, lat, lng, toLat, toLng float64, following ...Waypoint) (Estimate, error) {
	estimate, err := c.routeAttempt(ctx, lat, lng, toLat, toLng, following)
	if len(following) == 0 || !errors.Is(err, ErrTomTomNoRoute) {
		return estimate, err
	}
	// A later stop may be unreachable. Retry just the active stop with the
	// same truck profile and a separate reservation.
	estimate, err = c.routeAttempt(ctx, lat, lng, toLat, toLng, nil)
	estimate.FollowingRejected = true
	return estimate, err
}

func (c *Client) routeAttempt(ctx context.Context, lat, lng, toLat, toLng float64, following []Waypoint) (Estimate, error) {
	if c.Budget == nil {
		return Estimate{}, errors.New("routing usage accounting unavailable")
	}
	if err := c.waitForProvider(ctx); err != nil {
		return Estimate{}, err
	}
	// Commit the reservation before sending; failed/unknown outcomes are not refunded.
	if err := c.Budget.Reserve(ctx, c.Options); err != nil {
		return Estimate{}, err
	}
	// VRC 791 covers five axles and a trailer over 48 ft and up to 53 ft.
	// The WB-67 combination is 73 ft 6 in long and 8 ft 6 in wide.
	// Use 13 ft 6 in clearance height and 80,000 lb gross weight for the ETA profile.
	q := url.Values{
		"key": {c.Options.TomTomAPIKey}, "traffic": {"true"}, "departAt": {"now"},
		"travelMode": {"truck"}, "routeType": {"fastest"}, "routeRepresentation": {"encodedPolyline"},
		"maxAlternatives": {"0"}, "sectionType": {"travelMode"}, "vehicleCommercial": {"true"},
		"vehicleLength": {"22.4028"}, "vehicleWidth": {"2.5908"}, "vehicleHeight": {"4.1148"},
		"vehicleWeight": {"36287"}, "vehicleNumberOfAxles": {"5"},
	}
	locations := fmt.Sprintf("%.7f,%.7f:%.7f,%.7f", lat, lng, toLat, toLng)
	for _, point := range following {
		locations += fmt.Sprintf(":%.7f,%.7f", point.Latitude, point.Longitude)
	}
	endpoint := c.TomTomURL + "/" + locations + "/json?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Estimate{}, errors.New("invalid routing request")
	}
	// Avoid net/http's automatic GET replay on a failed reused connection.
	req.Close = true
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Estimate{}, errors.New("routing connection unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// Only the documented no-route error permits an active-stop retry.
		// Provider messages and URLs can include private data.
		if res.StatusCode == http.StatusBadRequest {
			var failure struct {
				DetailedError struct {
					Code string `json:"code"`
				} `json:"detailedError"`
			}
			if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&failure) == nil && failure.DetailedError.Code == "NO_ROUTE_FOUND" {
				return Estimate{}, ErrTomTomNoRoute
			}
		}
		c.pauseAfterHTTPError(res.StatusCode)
		if res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
			return Estimate{}, fmt.Errorf("%w: HTTP %d", ErrRouteUnavailable, res.StatusCode)
		}
		return Estimate{}, fmt.Errorf("routing returned HTTP %d", res.StatusCode)
	}
	var response struct {
		Routes []struct {
			Sections []struct {
				Type       string `json:"sectionType"`
				TravelMode string `json:"travelMode"`
			} `json:"sections"`
			Summary routeSummary `json:"summary"`
			Legs    []struct {
				Summary   routeSummary `json:"summary"`
				Polyline  string       `json:"encodedPolyline"`
				Precision int          `json:"encodedPolylinePrecision"`
			} `json:"legs"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil || len(response.Routes) == 0 {
		return Estimate{}, ErrRouteUnavailable
	}
	route := response.Routes[0]
	for _, section := range route.Sections {
		if section.Type == "TRAVEL_MODE" && section.TravelMode != "truck" {
			return Estimate{}, ErrTomTomNoRoute
		}
	}
	summary := route.Summary
	if len(following) > 0 {
		if len(route.Legs) != len(following)+1 {
			return Estimate{}, ErrRouteUnavailable
		}
		// The board's ETA and arrival evidence belong to the active stop.
		summary = route.Legs[0].Summary
	}
	if summary.Seconds == nil || *summary.Seconds < 0 || *summary.Seconds > 31536000 || summary.Meters == nil || *summary.Meters < 0 {
		return Estimate{}, ErrRouteUnavailable
	}
	estimate := Estimate{Duration: time.Duration(*summary.Seconds) * time.Second, Meters: *summary.Meters, Assumptions: EstimateAssumptions}
	for i, leg := range route.Legs {
		var geometry *RouteGeometry
		if leg.Polyline != "" && (leg.Precision == 5 || leg.Precision == 7) {
			geometry = &RouteGeometry{Polyline: leg.Polyline, Precision: leg.Precision}
		}
		if i == 0 && len(route.Legs) == len(following)+1 {
			estimate.Geometry = geometry
		} else if len(following) > 0 {
			result := RouteLeg{}
			if leg.Summary.Meters != nil && *leg.Summary.Meters >= 0 {
				result = RouteLeg{Meters: *leg.Summary.Meters, Geometry: geometry}
			}
			estimate.Following = append(estimate.Following, result)
		}
	}
	return estimate, nil
}

func (c *Client) pauseAfterHTTPError(status int) {
	delay := time.Minute
	if status == http.StatusTooManyRequests {
		delay = 5 * time.Minute
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until := time.Now().Add(delay)
	if until.After(c.blockedUntil) {
		c.blockedUntil = until
	}
}
