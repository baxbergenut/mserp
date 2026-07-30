package prepass

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
	"sync"
	"time"
)

const (
	maxErrorBodyBytes = 4 << 10
	pageSize          = 10_000
)

type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	mu             sync.Mutex
	accessToken    string
	tokenExpiresAt time.Time
	accountNumbers []string
}

func NewClient(baseURL, clientID, clientSecret string) *Client {
	return NewClientWithHTTPClient(
		baseURL,
		clientID,
		clientSecret,
		&http.Client{Timeout: 60 * time.Second},
	)
}

func NewClientWithHTTPClient(
	baseURL string,
	clientID string,
	clientSecret string,
	httpClient *http.Client,
) *Client {
	return &Client{
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
		httpClient:   httpClient,
	}
}

type Transaction struct {
	TollID              int64       `json:"tollId"`
	AccountNumber       json.Number `json:"accountNumber"`
	PostDateTime        string      `json:"postDateTime"`
	InvoiceDateTime     string      `json:"invoiceDateTime"`
	DeviceNumber        string      `json:"deviceNumber"`
	VehicleNumber       string      `json:"vehicleNumber"`
	PlateNumber         string      `json:"plateNumber"`
	PlateState          string      `json:"plateState"`
	PPDeviceID          string      `json:"ppDeviceId"`
	TollAgencyCode      string      `json:"tollAgencyCode"`
	TollAgencyName      string      `json:"tollAgencyName"`
	TollAgencyState     string      `json:"tollAgencyState"`
	BillingAgencyCode   string      `json:"billingAgencyCode"`
	EntryDateTime       string      `json:"entryDateTime"`
	EntryPlazaCode      string      `json:"entryPlazaCode"`
	EntryPlazaName      string      `json:"entryPlazaName"`
	ReadType            string      `json:"readType"`
	ExitDateTime        string      `json:"exitDateTime"`
	ExitPlazaCode       string      `json:"exitPlazaCode"`
	ExitPlazaName       string      `json:"exitPlazaName"`
	TollClass           string      `json:"tollClass"`
	TollCharge          json.Number `json:"tollCharge"`
	TollCategory        string      `json:"tollCategory"`
	DisputeStatus       string      `json:"disputeStatus"`
	DisputeStatusReason string      `json:"disputeStatusReason"`
	DeviceStatus        string      `json:"deviceStatus"`
	CostCenter          string      `json:"costCenter"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type account struct {
	AccountNumber string `json:"accountNumber"`
	AccountStatus string `json:"accountStatus"`
}

type accountsResponse struct {
	Accounts []account `json:"accounts"`
}

type pageInfo struct {
	PageNumber   int `json:"pageNumber"`
	TotalPages   int `json:"totalPages"`
	TotalRecords int `json:"totalRecords"`
}

type transactionsResponse struct {
	PageInfo     pageInfo      `json:"pageInfo"`
	Transactions []Transaction `json:"transactions"`
}

func ParseTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid PrePass timestamp %q", value)
}

func (c *Client) FetchTransactions(
	ctx context.Context,
	start time.Time,
	end time.Time,
) ([]Transaction, error) {
	if !start.Before(end) {
		return nil, errors.New("PrePass start date must precede end date")
	}
	if end.Sub(start) > 31*24*time.Hour {
		return nil, errors.New("PrePass date range cannot exceed 31 days")
	}

	token, accounts, err := c.credentials(ctx)
	if err != nil {
		return nil, err
	}

	var transactions []Transaction
	for pageNumber := 1; ; pageNumber++ {
		endpoint, err := url.Parse(c.baseURL + "/tolltransaction/v1/transactions")
		if err != nil {
			return nil, fmt.Errorf("build PrePass transactions URL: %w", err)
		}
		query := endpoint.Query()
		query.Set("startPostDate", start.UTC().Format(time.DateOnly))
		query.Set("endPostDate", end.UTC().Format(time.DateOnly))
		query.Set("accountNumbers", strings.Join(accounts, ","))
		query.Set("pageNumber", strconv.Itoa(pageNumber))
		query.Set("pageSize", strconv.Itoa(pageSize))
		endpoint.RawQuery = query.Encode()

		var response transactionsResponse
		status, err := c.getJSON(ctx, endpoint.String(), token, &response)
		if err != nil {
			return nil, fmt.Errorf("fetch PrePass toll transactions: %w", err)
		}
		if status == http.StatusNoContent {
			return transactions, nil
		}
		transactions = append(transactions, response.Transactions...)
		if response.PageInfo.TotalPages <= pageNumber {
			return transactions, nil
		}
	}
}

func (c *Client) credentials(ctx context.Context) (string, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if c.accessToken == "" || !now.Before(c.tokenExpiresAt) {
		token, expiresAt, err := c.fetchToken(ctx, now)
		if err != nil {
			return "", nil, err
		}
		c.accessToken = token
		c.tokenExpiresAt = expiresAt
		c.accountNumbers = nil
	}
	if len(c.accountNumbers) == 0 {
		accounts, err := c.fetchActiveAccounts(ctx, c.accessToken)
		if err != nil {
			return "", nil, err
		}
		c.accountNumbers = accounts
	}
	return c.accessToken, append([]string(nil), c.accountNumbers...), nil
}

func (c *Client) fetchToken(ctx context.Context, now time.Time) (string, time.Time, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/auth/v1/token",
		bytes.NewBufferString("{}"),
	)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create PrePass token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("client_id", c.clientID)
	req.Header.Set("client_secret", c.clientSecret)

	response, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("request PrePass token: %w", err)
	}
	defer response.Body.Close()
	if err := requireSuccess(response, "PrePass token API"); err != nil {
		return "", time.Time{}, err
	}

	var value tokenResponse
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return "", time.Time{}, fmt.Errorf("decode PrePass token: %w", err)
	}
	value.AccessToken = strings.TrimSpace(value.AccessToken)
	if value.AccessToken == "" {
		return "", time.Time{}, errors.New("PrePass token API returned an empty access token")
	}
	expiresIn := time.Duration(value.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}
	refreshAt := now.Add(expiresIn - time.Minute)
	if !refreshAt.After(now) {
		refreshAt = now.Add(expiresIn / 2)
	}
	return value.AccessToken, refreshAt, nil
}

func (c *Client) fetchActiveAccounts(ctx context.Context, token string) ([]string, error) {
	var value accountsResponse
	status, err := c.getJSON(ctx, c.baseURL+"/accounts/v1/accounts", token, &value)
	if err != nil {
		return nil, fmt.Errorf("fetch PrePass accounts: %w", err)
	}
	if status == http.StatusNoContent {
		return nil, errors.New("PrePass account API returned no accounts")
	}

	accounts := make([]string, 0, len(value.Accounts))
	for _, item := range value.Accounts {
		number := strings.TrimSpace(item.AccountNumber)
		if !strings.EqualFold(strings.TrimSpace(item.AccountStatus), "active") || number == "" {
			continue
		}
		if _, err := strconv.ParseInt(number, 10, 64); err != nil {
			return nil, fmt.Errorf("PrePass account number %q is not numeric", number)
		}
		accounts = append(accounts, number)
	}
	if len(accounts) == 0 {
		return nil, errors.New("PrePass account API returned no active accounts")
	}
	return accounts, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint, token string, target any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	if err := requireSuccess(response, "PrePass API"); err != nil {
		return response.StatusCode, err
	}

	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return response.StatusCode, err
	}
	return response.StatusCode, nil
}

func requireSuccess(response *http.Response, service string) error {
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = response.Status
	}
	return fmt.Errorf("%s returned %s: %s", service, response.Status, message)
}
