package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Driver struct {
	ID            string            `json:"id"`
	FirstName     string            `json:"first_name"`
	LastName      string            `json:"last_name"`
	Phone         string            `json:"phone"`
	Email         string            `json:"email"`
	IntegrationID string            `json:"integration_id"`
	UpdatedAt     string            `json:"updated_at"`
	DataFields    []DriverDataField `json:"data_fields"`
}
type DriverDataField struct {
	Name  string `json:"field_name"`
	Value string `json:"field_value"`
}
type DriverInput struct {
	FirstName  string            `json:"first_name"`
	LastName   string            `json:"last_name"`
	Phone      string            `json:"phone"`
	Email      string            `json:"email"`
	DataFields []DriverDataField `json:"data_fields"`
}
type PolicyPrompt struct {
	Name           string `json:"name"`
	ValidationMode string `json:"validation_mode"`
}
type PolicyLimit struct {
	Type      string `json:"type"`
	Mode      string `json:"mode"`
	Amount    string `json:"amount"`
	Unit      string `json:"unit"`
	Frequency string `json:"frequency"`
	Usage     string `json:"usage,omitempty"`
	Remaining string `json:"remaining,omitempty"`
}
type DriverPolicy struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	ELDEnabled bool           `json:"eld_enabled"`
	Prompts    []PolicyPrompt `json:"authorization_prompts"`
	Limits     []PolicyLimit  `json:"limits"`
}
type PolicyAssignment struct {
	ID         string        `json:"id"`
	PolicyID   string        `json:"policy_id"`
	PolicyName string        `json:"policy_name"`
	Enabled    bool          `json:"enabled"`
	Limits     []PolicyLimit `json:"limits"`
}

// Provider errors deliberately exclude response bodies: those can contain PINs or contact data.
type APIError struct {
	Status    int
	Uncertain bool
}

func (e *APIError) Error() string {
	if e.Uncertain {
		return "Relay did not confirm the result. Refresh and verify the change before retrying."
	}
	if e.Status == 0 {
		return "Relay could not be reached. Refresh to try again."
	}
	return fmt.Sprintf("Relay rejected the request (HTTP %d). Check the driver details and Relay configuration.", e.Status)
}
func IsNotFound(err error) bool { var e *APIError; return errors.As(err, &e) && e.Status == 404 }
func (c *Client) driverRequest(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	// Driver/policy endpoints use the integrations namespace; transactions do not.
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/integrations"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Never forward the key to another host or replay a mutation through a redirect.
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return &APIError{Uncertain: method != http.MethodGet}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Uncertain: method != http.MethodGet && (resp.StatusCode >= 500 || resp.StatusCode == 408)}
	}
	if output != nil {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(output); err != nil {
			return &APIError{Status: resp.StatusCode, Uncertain: method != http.MethodGet}
		}
	}
	return nil
}
func (c *Client) Drivers(ctx context.Context) ([]Driver, error) {
	result := []Driver{}
	seen := map[string]bool{}
	for offset := 0; offset < 50000; offset += 50 {
		var page []Driver
		if err := c.driverRequest(ctx, "GET", fmt.Sprintf("/drivers/?limit=50&offset=%d", offset), nil, &page); err != nil {
			return nil, err
		}
		for _, d := range page {
			if d.ID == "" || seen[d.ID] {
				return nil, errors.New("Relay returned an incomplete or repeated driver page")
			}
			seen[d.ID] = true
			result = append(result, d)
		}
		if len(page) < 50 {
			return result, nil
		}
	}
	return nil, errors.New("Relay driver pagination exceeded the safety limit")
}
func (c *Client) Driver(ctx context.Context, id string) (Driver, error) {
	var d Driver
	err := c.driverRequest(ctx, "GET", "/drivers/"+url.PathEscape(id), nil, &d)
	return d, err
}
func (c *Client) Policies(ctx context.Context) ([]DriverPolicy, error) {
	p := []DriverPolicy{}
	err := c.driverRequest(ctx, "GET", "/fuel/policies/", nil, &p)
	return p, err
}
func (c *Client) Assignment(ctx context.Context, id string) (*PolicyAssignment, error) {
	var a PolicyAssignment
	err := c.driverRequest(ctx, "GET", "/fuel/policies/policy-assignments/"+url.PathEscape(id), nil, &a)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if a.ID == "" || a.PolicyID == "" {
		return nil, errors.New("Relay returned an invalid policy assignment")
	}
	return &a, nil
}
func (c *Client) CreateDriver(ctx context.Context, in DriverInput) (Driver, error) {
	var d Driver
	err := c.driverRequest(ctx, "POST", "/drivers/", in, &d)
	if err == nil && d.ID == "" {
		err = &APIError{Uncertain: true}
	}
	return d, err
}
func (c *Client) UpdateDriver(ctx context.Context, id string, in DriverInput) error {
	return c.driverRequest(ctx, "PUT", "/drivers/"+url.PathEscape(id), in, nil)
}
func (c *Client) RemoveDriver(ctx context.Context, id string) error {
	return c.driverRequest(ctx, "DELETE", "/drivers/"+url.PathEscape(id), nil, nil)
}
func (c *Client) AssignPolicy(ctx context.Context, driverID, policyID string) error {
	return c.driverRequest(ctx, "POST", "/fuel/policies/policy-assignments/", map[string]string{"driver_id": driverID, "policy_id": policyID}, nil)
}

// Live staging verification confirms that all assignment item routes use driver ID.
func (c *Client) EnablePolicy(ctx context.Context, driverID string, enabled bool) error {
	return c.driverRequest(ctx, "PUT", "/fuel/policies/policy-assignments/"+url.PathEscape(driverID), map[string]bool{"enabled": enabled}, nil)
}
func (c *Client) UnassignPolicy(ctx context.Context, driverID string) error {
	return c.driverRequest(ctx, "DELETE", "/fuel/policies/policy-assignments/"+url.PathEscape(driverID), nil, nil)
}
