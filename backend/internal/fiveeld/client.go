package fiveeld

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 4 << 20

type Position struct {
	TruckNumber string
	VIN         string
	Latitude    float64
	Longitude   float64
	ReportedAt  time.Time
}

type Client struct {
	baseURL       string
	apiKey        string
	providerToken string
	httpClient    *http.Client
}

func NewClient(baseURL, apiKey, providerToken string) *Client {
	return NewClientWithHTTPClient(baseURL, apiKey, providerToken, &http.Client{Timeout: 30 * time.Second})
}

func NewClientWithHTTPClient(baseURL, apiKey, providerToken string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:       strings.TrimRight(baseURL, "/"),
		apiKey:        apiKey,
		providerToken: providerToken,
		httpClient:    httpClient,
	}
}

func (c *Client) CurrentPositions(ctx context.Context, usdot string) ([]Position, error) {
	var response struct {
		Units []struct {
			TruckNumber string `json:"truck_number"`
			VIN         string `json:"vin"`
			Coordinates struct {
				Latitude  float64 `json:"lat"`
				Longitude float64 `json:"lng"`
			} `json:"coordinates"`
			Timestamp string `json:"timestamp"`
		} `json:"units"`
	}
	if err := c.get(ctx, "/api/v2/units-by-usdot/"+url.PathEscape(usdot), nil, &response); err != nil {
		return nil, err
	}
	positions := make([]Position, 0, len(response.Units))
	for _, unit := range response.Units {
		reportedAt, err := time.Parse(time.RFC3339Nano, unit.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("Five ELD unit %q has an invalid timestamp", unit.TruckNumber)
		}
		positions = append(positions, Position{
			TruckNumber: strings.TrimSpace(unit.TruckNumber),
			VIN:         strings.TrimSpace(unit.VIN),
			Latitude:    unit.Coordinates.Latitude,
			Longitude:   unit.Coordinates.Longitude,
			ReportedAt:  reportedAt.UTC(),
		})
	}
	return positions, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, target any) error {
	requestURL := c.baseURL + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-api-key", c.apiKey)
	request.Header.Set("provider-token", c.providerToken)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Five ELD: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Five ELD returned %s", response.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode Five ELD response: %w", err)
	}
	return nil
}
