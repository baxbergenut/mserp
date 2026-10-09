// Package weighmytruck implements the server-only CAT Scale driver API.
package weighmytruck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Options struct{ ClientID, ClientSecret, TokenURL, Scope, APIURL, CompanyName string }

func (o Options) Enabled() bool { return o.ClientID != "" && o.ClientSecret != "" }
func (o Options) Validate() error {
	if o.ClientID == "" && o.ClientSecret == "" {
		return nil
	}
	if !o.Enabled() || o.Scope == "" || o.CompanyName == "" {
		return errors.New("WeighMyTruck requires client ID, client secret, scope and company name")
	}
	for _, raw := range []string{o.APIURL, o.TokenURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("WeighMyTruck endpoints must be HTTPS URLs")
		}
	}
	return nil
}

type Driver struct {
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Email       string `json:"driverEmail"`
	Phone       string `json:"phone"`
	UniqueID    string `json:"uniqueId,omitempty"`
	CompanyName string `json:"companyName"`
}

// Error deliberately excludes provider response bodies, tokens and contact data.
type Error struct {
	Message   string
	Uncertain bool
}

func (e *Error) Error() string { return e.Message }

type Client struct {
	options Options
	http    *http.Client
	mu      sync.Mutex
	token   string
	expires time.Time
}

func NewClient(o Options) *Client {
	return &Client{options: o, http: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Configured() bool { return c != nil && c.options.Enabled() }
func (c *Client) bearer(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Add(time.Minute).Before(c.expires) {
		return c.token, nil
	}
	f := url.Values{"client_id": {c.options.ClientID}, "client_secret": {c.options.ClientSecret}, "scope": {c.options.Scope}, "grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.options.TokenURL, strings.NewReader(f.Encode()))
	if err != nil {
		return "", errors.New("WeighMyTruck token request could not be prepared")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return "", errors.New("WeighMyTruck authentication unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", errors.New("WeighMyTruck authentication failed; check server credentials")
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&t) != nil || t.AccessToken == "" || t.ExpiresIn <= 0 {
		return "", errors.New("Invalid WeighMyTruck token response")
	}
	c.token = t.AccessToken
	c.expires = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	return c.token, nil
}
func (c *Client) Change(ctx context.Context, add bool, d Driver) error {
	if !c.Configured() {
		return &Error{Message: "WeighMyTruck is not configured on the server"}
	}
	token, err := c.bearer(ctx)
	if err != nil {
		return &Error{Message: err.Error()}
	}
	path := "/DriverManager/RemoveDriver"
	// RemoveDriver's OpenAPI schema is a JSON string; its object example is wrong.
	var payload any = d.Email
	if add {
		path = "/DriverManager/AddDriverToFleet"
		d.CompanyName = c.options.CompanyName
		payload = d
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.options.APIURL, "/")+path, strings.NewReader(string(body)))
	if err != nil {
		return &Error{Message: "WeighMyTruck request could not be prepared"}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return &Error{Message: "WeighMyTruck response was not received. Verify membership before retrying.", Uncertain: true}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	if res.StatusCode == 401 || res.StatusCode == 403 {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return &Error{Message: "WeighMyTruck denied access. Check API and fleet payment-method permissions."}
	}
	if res.StatusCode == 429 {
		return &Error{Message: "WeighMyTruck rate limit reached. Try again later."}
	}
	// A rejected add may mean it already belongs to this fleet. Never guess from
	// undocumented message text, and never automatically replay a mutation.
	return &Error{Message: fmt.Sprintf("WeighMyTruck returned HTTP %d. Verify membership on the fleet website.", res.StatusCode), Uncertain: true}
}
