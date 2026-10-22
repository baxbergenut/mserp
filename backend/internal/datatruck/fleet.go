package datatruck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Fleet writes intentionally never use the load client's automatic retries.
// DataTruck documents no idempotency key for either create endpoint.
type FleetError struct {
	Status    int
	Uncertain bool
}

func (e *FleetError) Error() string {
	if e.Uncertain {
		return "DataTruck did not confirm the result. Check DataTruck before another enrollment."
	}
	return fmt.Sprintf("DataTruck rejected enrollment (HTTP %d). Check the saved profile and API token permissions.", e.Status)
}
func (c *Client) Configured() bool    { return c != nil && c.apiKey != "" && c.baseURL != "" }
func (c *Client) Environment() string { return c.baseURL }

// FleetList uses documented filters and validates pagination origin before sending credentials.
func (c *Client) FleetList(ctx context.Context, kind string, filters []map[string]string) ([]map[string]any, error) {
	if kind != "drivers" && kind != "trucks" {
		return nil, errors.New("invalid DataTruck record type")
	}
	encoded, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	next := c.baseURL + "/" + kind + "/list/?page_size=10&filter=" + url.QueryEscape(string(encoded))
	var out []map[string]any
	seen := map[string]bool{}
	for next != "" {
		parsed, err := url.Parse(next)
		if err != nil {
			return nil, err
		}
		base, _ := url.Parse(c.baseURL)
		if parsed.Scheme != base.Scheme || parsed.Host != base.Host || !strings.HasPrefix(parsed.Path, base.Path+"/"+kind+"/list/") || seen[next] {
			return nil, errors.New("invalid DataTruck pagination")
		}
		seen[next] = true
		data, err := c.fleetRequest(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Results []map[string]any `json:"results"`
			Next    *string          `json:"next"`
		}
		if err = json.Unmarshal(data, &page); err != nil || page.Results == nil {
			return nil, errors.New("invalid DataTruck roster response")
		}
		out = append(out, page.Results...)
		if page.Next == nil || *page.Next == "" {
			break
		}
		next = resolveNextURL(c.baseURL, *page.Next)
	}
	return out, nil
}
func (c *Client) CreateFleetRecord(ctx context.Context, kind string, payload json.RawMessage) (json.RawMessage, error) {
	if kind != "drivers" && kind != "trucks" {
		return nil, errors.New("invalid DataTruck record type")
	}
	return c.fleetRequest(ctx, http.MethodPost, c.baseURL+"/"+kind+"/create/", payload)
}
func (c *Client) UpdateDriver(ctx context.Context, id int64, payload json.RawMessage) (json.RawMessage, error) {
	if id <= 0 {
		return nil, errors.New("invalid DataTruck driver ID")
	}
	return c.fleetRequest(ctx, http.MethodPut, c.baseURL+"/drivers/update/"+strconv.FormatInt(id, 10)+"/", payload)
}
func (c *Client) fleetRequest(ctx context.Context, method, target string, payload []byte) (json.RawMessage, error) {
	mutation := method != http.MethodGet
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	release := func() {}
	if c.gate != nil {
		release, err = c.gate.acquire(ctx)
		if err != nil {
			return nil, err
		}
	}
	defer release()
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, &FleetError{Uncertain: mutation}
	}
	defer res.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode == 429 && c.gate != nil {
		c.gate.cooldown(max(retryDelay(res.Header.Get("Retry-After"), 0), maxRetryDelay))
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		definiteRejection := res.StatusCode == 400 || res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 404 || res.StatusCode == 405 || res.StatusCode == 409 || res.StatusCode == 422 || res.StatusCode == 429
		return nil, &FleetError{Status: res.StatusCode, Uncertain: mutation && !definiteRejection}
	}
	if readErr != nil || !json.Valid(data) {
		return nil, &FleetError{Status: res.StatusCode, Uncertain: mutation}
	}
	if mutation {
		var result struct {
			ID     int    `json:"id"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(data, &result) != nil || result.ID <= 0 && result.Detail != "success" {
			return nil, &FleetError{Status: res.StatusCode, Uncertain: true}
		}
	}
	return data, nil
}
