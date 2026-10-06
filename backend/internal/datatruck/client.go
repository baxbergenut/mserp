package datatruck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxRequestAttempts = 5
	maxRetryDelay      = 60 * time.Second
)

type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	gate       *requestGate
}

func NewClient(apiKey, companyName string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     strings.TrimSpace(apiKey),
		gate:       &requestGate{interval: 4 * time.Second},
		baseURL:    fmt.Sprintf("https://%s.datatruck.io/api/v1/openapi", strings.ToLower(strings.TrimSpace(companyName))),
	}
}

type LoadListResponse struct {
	Count    int     `json:"count"`
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
	Results  []Load  `json:"results"`
}

type Load struct {
	ID                      int                   `json:"id"`
	LoadID                  *string               `json:"load_id"`
	ShipmentID              *string               `json:"shipment_id"`
	Status                  string                `json:"status"`
	LoadPay                 *FlexibleString       `json:"load_pay"`
	TotalOtherPay           *FlexibleString       `json:"total_other_pay"`
	TotalPay                *FlexibleString       `json:"total_pay"`
	TotalMiles              *FlexibleString       `json:"total_miles"`
	PerMileRevenue          *FlexibleString       `json:"per_mile_revenue"`
	DispatcherFullName      *string               `json:"dispatcher__full_name"`
	CustomerCompanyName     *string               `json:"customer__company_name"`
	PickupAppointmentTime   *time.Time            `json:"pickup_appointment_time"`
	DeliveryAppointmentTime *time.Time            `json:"delivery_appointment_time"`
	PickupTime              *time.Time            `json:"pickup_time"`
	DeliveryTime            *time.Time            `json:"delivery_time"`
	CreatedDatetime         *time.Time            `json:"created_datetime"`
	Trip                    *Trip                 `json:"trip"`
	AssignedDriverNTruck    *AssignedDriverNTruck `json:"assigned_driver_n_truck"`
	RawPayload              json.RawMessage       `json:"-"`
	Stops                   []LoadStop            `json:"stops,omitempty"`
}

// Keep only the stop details used by payroll; source ordering determines the
// first pickup and final delivery for multi-stop loads.
type LoadStop struct {
	StopType string `json:"stop_type"`
	Ordering int    `json:"ordering"`
	Location struct {
		City    string `json:"city"`
		State   string `json:"state"`
		ZipCode string `json:"zip_code"`
		Address string `json:"address1"`
	} `json:"location"`
}

type Trip struct {
	LoadedMiles        *FlexibleString `json:"mile,omitempty"`
	DeadheadMiles      *FlexibleString `json:"empty_mile,omitempty"`
	DriverFullName     *string         `json:"driver__full_name"`
	TeamDriverFullName *string         `json:"team_driver__full_name"`
	TruckUnitNumber    *string         `json:"truck__unit_number"`
}

type AssignedDriverNTruck struct {
	DriverFullName  *string `json:"driver_full_name"`
	TruckUnitNumber *string `json:"truck_unit_number"`
}

type FlexibleString string

var syncDateFilterColumns = map[string]struct{}{
	"pickup_time":               {},
	"pickup_appointment_time":   {},
	"delivery_time":             {},
	"delivery_appointment_time": {},
	"created_datetime":          {},
}

func (f *FlexibleString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*f = ""
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*f = FlexibleString(value)
		return nil
	}
	*f = FlexibleString(trimmed)
	return nil
}

func (f FlexibleString) String() string {
	return string(f)
}

func (c *Client) FetchLoadsSince(ctx context.Context, since time.Time) ([]Load, error) {
	return c.FetchLoadsByDateSince(ctx, "created_datetime", since)
}

func (c *Client) FetchLoadByID(ctx context.Context, id int) (Load, error) {
	loads, err := c.fetchLoads(ctx, []map[string]string{
		{"column": "id", "value": strconv.Itoa(id), "contains": "greater_than"},
		{"column": "id", "value": strconv.Itoa(id), "contains": "less_than"},
	}, "id", func(loads []Load) error {
		for _, l := range loads {
			if l.ID != id {
				return errors.New("load detail filter returned a different record")
			}
		}
		return nil
	})
	if err != nil {
		return Load{}, err
	}
	if len(loads) != 1 {
		return Load{}, errors.New("load detail record not found")
	}
	return loads[0], nil
}

func (c *Client) FetchLoadsAfterID(ctx context.Context, afterID int) ([]Load, error) {
	return c.fetchLoads(ctx, []map[string]string{{
		"column": "id",
		// DataTruck's numeric greater_than operator is inclusive; the date
		// operator "after" is silently ignored for IDs by the upstream API.
		"value":    strconv.Itoa(afterID + 1),
		"contains": "greater_than",
	}}, "id", func(loads []Load) error {
		for _, load := range loads {
			if load.ID <= afterID {
				return fmt.Errorf("datatruck numeric filter returned record %d at or below watermark %d", load.ID, afterID)
			}
		}
		return nil
	})
}

// FetchLoadsByIDs uses the documented array-valued is_in filter. Reject an
// ignored filter before pagination can turn a small refresh into a full import.
func (c *Client) FetchLoadsByIDs(ctx context.Context, ids []int) ([]Load, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	wanted := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("load IDs must be positive")
		}
		wanted[id] = true
	}
	return c.fetchLoads(ctx, []map[string]any{{"column": "id", "contains": "is_in", "value": ids}}, "id", func(loads []Load) error {
		for _, load := range loads {
			if !wanted[load.ID] {
				return fmt.Errorf("datatruck ID filter returned unexpected record %d", load.ID)
			}
		}
		return nil
	})
}

func (c *Client) FetchLoadsByDateSince(
	ctx context.Context,
	column string,
	since time.Time,
) ([]Load, error) {
	return c.fetchLoadsByDateSince(ctx, column, since, nil)
}

// FetchLoadsByDateSinceThroughID reconciles only previously imported IDs.
// Newer IDs are already fetched by FetchLoadsAfterID in the same sync.
func (c *Client) FetchLoadsByDateSinceThroughID(ctx context.Context, column string, since time.Time, throughID int) ([]Load, error) {
	return c.fetchLoadsByDateSince(ctx, column, since, &throughID)
}

func (c *Client) fetchLoadsByDateSince(ctx context.Context, column string, since time.Time, throughID *int) ([]Load, error) {
	if _, ok := syncDateFilterColumns[column]; !ok {
		return nil, fmt.Errorf("unsupported datatruck load date filter %q", column)
	}
	filters := []map[string]string{{
		"column":   column,
		"value":    since.UTC().Format(time.RFC3339Nano),
		"contains": "after",
	}}
	if throughID != nil {
		filters = append(filters, map[string]string{
			"column": "id", "value": strconv.Itoa(*throughID), "contains": "less_than",
		})
	}
	return c.fetchLoads(ctx, filters, "-"+column, func(loads []Load) error {
		for _, load := range loads {
			if throughID != nil && load.ID > *throughID {
				return fmt.Errorf("datatruck reconciliation exceeded watermark %d", *throughID)
			}
		}
		return nil
	})
}

func (c *Client) fetchLoads(
	ctx context.Context,
	filters any,
	ordering string,
	validatePage func([]Load) error,
) ([]Load, error) {
	filter, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("page_size", "25")
	query.Set("ordering", ordering)
	query.Set("filter", string(filter))

	requestURL := c.baseURL + "/orders/?" + query.Encode()
	loads := make([]Load, 0)

	for requestURL != "" {
		response, err := c.doRequest(ctx, requestURL)
		if err != nil {
			return nil, err
		}

		if validatePage != nil {
			if err := validatePage(response.Results); err != nil {
				return nil, err
			}
		}
		loads = append(loads, response.Results...)

		if response.Next == nil || strings.TrimSpace(*response.Next) == "" {
			break
		}
		requestURL = resolveNextURL(c.baseURL, *response.Next)
	}

	return loads, nil
}

func (c *Client) doRequest(ctx context.Context, requestURL string) (*LoadListResponse, error) {
	for attempt := 0; attempt < maxRequestAttempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Token "+c.apiKey)
		request.Header.Set("Content-Type", "application/json")

		release := func() {}
		if c.gate != nil {
			release, err = c.gate.acquire(ctx)
			if err != nil {
				return nil, err
			}
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			release()
			return nil, err
		}

		if response.StatusCode == http.StatusOK {
			var payload LoadListResponse
			decodeErr := json.NewDecoder(response.Body).Decode(&payload)
			closeErr := response.Body.Close()
			release()
			if decodeErr != nil {
				return nil, decodeErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			return &payload, nil
		}

		status := response.Status
		retryAfter := response.Header.Get("Retry-After")
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		_ = response.Body.Close()
		delay := retryDelay(retryAfter, attempt)
		if c.gate != nil && response.StatusCode == http.StatusTooManyRequests {
			// Without Retry-After, allow the upstream minute window to reset.
			if strings.TrimSpace(retryAfter) == "" {
				delay = max(delay, time.Minute)
			}
			c.gate.cooldown(delay)
		}
		release()

		if !isRetryableStatus(response.StatusCode) || attempt == maxRequestAttempts-1 {
			return nil, fmt.Errorf("datatruck request failed: %s", status)
		}
		if err := waitForRetry(ctx, delay); err != nil {
			return nil, err
		}
	}

	return nil, errors.New("datatruck request retry limit reached")
}

func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests ||
		status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	trimmed := strings.TrimSpace(retryAfter)
	if seconds, err := strconv.Atoi(trimmed); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(trimmed); err == nil {
		return max(time.Until(retryAt), 0)
	}
	return min(time.Second*time.Duration(1<<attempt), maxRetryDelay)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func resolveNextURL(baseURL, nextURL string) string {
	trimmed := strings.TrimSpace(nextURL)
	if trimmed == "" {
		return ""
	}
	base, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		return trimmed
	}
	next, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	return base.ResolveReference(next).String()
}
