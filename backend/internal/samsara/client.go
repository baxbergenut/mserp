package samsara

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type GPS struct {
	Time                time.Time
	Latitude, Longitude float64
	Heading, SpeedMPH   *float64
}
type Odometer struct {
	Time  time.Time
	Miles int
}
type Vehicle struct {
	ID, VIN, Name string
	GPS           *GPS
	Odometer      *Odometer
}
type Snapshot struct {
	Vehicles         []Vehicle
	Ambiguous        []string
	Fetched, Invalid int
}
type Client struct {
	base, token, version string
	http                 *http.Client
}

func NewClient(base, token, version string) *Client {
	return &Client{strings.TrimRight(base, "/"), token, version, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

type rosterVehicle struct {
	ID   string `json:"id"`
	VIN  string `json:"vin"`
	Name string `json:"name"`
}
type rawGPS struct {
	Time      string   `json:"time"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Heading   *float64 `json:"headingDegrees"`
	Speed     *float64 `json:"speedMilesPerHour"`
}
type rawOdometer struct {
	Time  string   `json:"time"`
	Value *float64 `json:"value"`
}
type rawStats struct {
	ID        string       `json:"id"`
	GPS       *rawGPS      `json:"gps"`
	OBD       *rawOdometer `json:"obdOdometerMeters"`
	Estimated *rawOdometer `json:"gpsOdometerMeters"`
}

var vinPattern = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`)

// Read-only, complete paginated snapshots. Errors never include provider bodies
// or credentials, and incomplete pages never become an authoritative roster.
func (c *Client) pages(ctx context.Context, path string, query url.Values, out func(json.RawMessage) error) error {
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		var body struct {
			Data       json.RawMessage `json:"data"`
			Pagination struct {
				After string `json:"endCursor"`
				More  bool   `json:"hasNextPage"`
			} `json:"pagination"`
		}
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			req, e := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+query.Encode(), nil)
			if e != nil {
				return fmt.Errorf("Samsara request configuration invalid")
			}
			req.Header.Set("Authorization", "Bearer "+c.token)
			req.Header.Set("Accept", "application/json")
			if c.version != "" {
				req.Header.Set("Samsara-Version", c.version)
			}
			response, e := c.http.Do(req)
			if e != nil {
				return fmt.Errorf("Samsara request failed")
			}
			if response.StatusCode == http.StatusOK {
				err = json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&body)
				response.Body.Close()
				if err != nil {
					return fmt.Errorf("Samsara response invalid")
				}
				break
			}
			status := response.StatusCode
			response.Body.Close()
			err = fmt.Errorf("Samsara returned HTTP %d", status)
			if (status != 429 && status < 500) || attempt == 2 {
				return err
			}
			delay := time.Duration(attempt+1) * time.Second
			if parsed, e := time.ParseDuration(response.Header.Get("Retry-After") + "s"); e == nil && parsed > delay {
				delay = parsed
			}
			if delay > time.Minute {
				delay = time.Minute
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		if err != nil {
			return err
		}
		if len(body.Data) == 0 || string(body.Data) == "null" {
			return fmt.Errorf("Samsara response missing data")
		}
		if err = out(body.Data); err != nil {
			return fmt.Errorf("Samsara response invalid")
		}
		if !body.Pagination.More {
			return nil
		}
		if body.Pagination.After == "" || seen[body.Pagination.After] {
			return fmt.Errorf("Samsara pagination did not advance")
		}
		seen[body.Pagination.After] = true
		query.Set("after", body.Pagination.After)
	}
	return fmt.Errorf("Samsara pagination limit exceeded")
}

func (c *Client) Snapshot(ctx context.Context, now time.Time) (Snapshot, error) {
	started := time.Now()
	result := Snapshot{Vehicles: []Vehicle{}, Ambiguous: []string{}}
	roster := []rosterVehicle{}
	stats := []rawStats{}
	if err := c.pages(ctx, "/fleet/vehicles", url.Values{"limit": {"512"}}, func(raw json.RawMessage) error {
		var page []rosterVehicle
		if e := json.Unmarshal(raw, &page); e != nil {
			return e
		}
		roster = append(roster, page...)
		return nil
	}); err != nil {
		return result, err
	}
	if err := c.pages(ctx, "/fleet/vehicles/stats", url.Values{"types": {"gps,obdOdometerMeters,gpsOdometerMeters"}}, func(raw json.RawMessage) error {
		var page []rawStats
		if e := json.Unmarshal(raw, &page); e != nil {
			return e
		}
		stats = append(stats, page...)
		return nil
	}); err != nil {
		return result, err
	}
	result.Fetched = len(roster)
	// A provider may observe a new point while the paginated reads are running.
	// Validate against completion time, retaining the caller's reference clock.
	now = now.Add(time.Since(started))
	byVIN := map[string][]rosterVehicle{}
	idCount := map[string]int{}
	byID := map[string][]rawStats{}
	for _, v := range roster {
		v.VIN = strings.ToUpper(strings.TrimSpace(v.VIN))
		if v.ID == "" || !vinPattern.MatchString(v.VIN) {
			result.Invalid++
			continue
		}
		byVIN[v.VIN] = append(byVIN[v.VIN], v)
		idCount[v.ID]++
	}
	for _, s := range stats {
		byID[s.ID] = append(byID[s.ID], s)
	}
	for vin, values := range byVIN {
		if len(values) != 1 || idCount[values[0].ID] != 1 || len(byID[values[0].ID]) > 1 {
			result.Ambiguous = append(result.Ambiguous, vin)
			continue
		}
		v := values[0]
		value := Vehicle{ID: v.ID, VIN: vin, Name: v.Name}
		if entries := byID[v.ID]; len(entries) == 1 {
			s := entries[0]
			if g := s.GPS; g != nil && g.Latitude != nil && g.Longitude != nil {
				at, err := time.Parse(time.RFC3339Nano, g.Time)
				if err == nil && !at.IsZero() && !at.After(now) && finite(*g.Latitude) && finite(*g.Longitude) && math.Abs(*g.Latitude) <= 90 && math.Abs(*g.Longitude) <= 180 && (*g.Latitude != 0 || *g.Longitude != 0) {
					gps := &GPS{Time: at, Latitude: *g.Latitude, Longitude: *g.Longitude}
					if g.Heading != nil && finite(*g.Heading) && *g.Heading >= 0 && *g.Heading < 360 {
						gps.Heading = g.Heading
					}
					if g.Speed != nil && finite(*g.Speed) && *g.Speed >= 0 && *g.Speed <= 200 {
						gps.SpeedMPH = g.Speed
					}
					value.GPS = gps
				}
			}
			for _, o := range []*rawOdometer{s.OBD, s.Estimated} {
				if o == nil || o.Value == nil || !finite(*o.Value) || *o.Value < 0 {
					continue
				}
				at, e := time.Parse(time.RFC3339Nano, o.Time)
				miles := math.Round(*o.Value / 1609.344)
				if e == nil && !at.IsZero() && !at.After(now) && miles <= 2000000000 {
					value.Odometer = &Odometer{Time: at, Miles: int(miles)}
					break
				}
			}
		}
		result.Vehicles = append(result.Vehicles, value)
	}
	sort.Slice(result.Vehicles, func(i, j int) bool { return result.Vehicles[i].VIN < result.Vehicles[j].VIN })
	sort.Strings(result.Ambiguous)
	return result, nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
