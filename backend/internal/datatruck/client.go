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
}

func NewClient(apiKey, companyName string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     strings.TrimSpace(apiKey),
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
}

type Trip struct {
	DriverFullName     *string `json:"driver__full_name"`
	TeamDriverFullName *string `json:"team_driver__full_name"`
	TruckUnitNumber    *string `json:"truck__unit_number"`
}

type AssignedDriverNTruck struct {
	DriverFullName  *string `json:"driver_full_name"`
	TruckUnitNumber *string `json:"truck_unit_number"`
}

type FlexibleString string

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
	filter, err := json.Marshal([]map[string]string{{
		"column":   "created_datetime",
		"value":    since.UTC().Format(time.RFC3339Nano),
		"contains": "after",
	}})
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("page_size", "100")
	query.Set("ordering", "-created_datetime")
	query.Set("filter", string(filter))

	requestURL := c.baseURL + "/orders/?" + query.Encode()
	loads := make([]Load, 0)

	for requestURL != "" {
		response, err := c.doRequest(ctx, requestURL)
		if err != nil {
			return nil, err
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

		response, err := c.httpClient.Do(request)
		if err != nil {
			return nil, err
		}

		if response.StatusCode == http.StatusOK {
			var payload LoadListResponse
			decodeErr := json.NewDecoder(response.Body).Decode(&payload)
			closeErr := response.Body.Close()
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

		if !isRetryableStatus(response.StatusCode) || attempt == maxRequestAttempts-1 {
			return nil, fmt.Errorf("datatruck request failed: %s", status)
		}
		if err := waitForRetry(ctx, retryDelay(retryAfter, attempt)); err != nil {
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
		return min(time.Duration(seconds)*time.Second, maxRetryDelay)
	}
	if retryAt, err := http.ParseTime(trimmed); err == nil {
		return min(max(time.Until(retryAt), 0), maxRetryDelay)
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
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "/") {
		return strings.TrimRight(baseURL, "/") + trimmed
	}
	return strings.TrimRight(baseURL, "/") + "/" + trimmed
}
