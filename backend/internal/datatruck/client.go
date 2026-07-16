package datatruck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
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
	LoadPay                 *string               `json:"load_pay"`
	TotalOtherPay           *string               `json:"total_other_pay"`
	TotalPay                *string               `json:"total_pay"`
	TotalMiles              *string               `json:"total_miles"`
	PerMileRevenue          *string               `json:"per_mile_revenue"`
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
	query.Set("page_size", "10")
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
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("datatruck request failed: %s", response.Status)
	}

	var payload LoadListResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
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
