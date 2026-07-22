package relay

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

const maxErrorBodyBytes = 4 << 10

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return NewClientWithHTTPClient(baseURL, apiKey, &http.Client{Timeout: 60 * time.Second})
}

func NewClientWithHTTPClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: httpClient,
	}
}

type Transaction struct {
	TransactionID    string            `json:"transaction_id"`
	CreatedAt        time.Time         `json:"created_at"`
	RelayFuelCode    *string           `json:"relay_fuel_code"`
	TotalAmountPaid  string            `json:"total_amount_paid"`
	TotalRetailPrice string            `json:"total_retail_price"`
	TotalAmountSaved string            `json:"total_amount_saved"`
	IsDirectBill     bool              `json:"is_direct_bill"`
	CurrencyCode     string            `json:"currency_code"`
	CashAdvance      *string           `json:"cash_advance"`
	Driver           TransactionDriver `json:"driver"`
	Merchant         Merchant          `json:"merchant"`
	Location         Location          `json:"location"`
	Prompts          []Prompt          `json:"prompts"`
	FuelItems        []FuelItem        `json:"fuel_items"`
	Products         []Product         `json:"products"`
	Fees             []Fee             `json:"fees"`
	FuelPolicy       *FuelPolicy       `json:"fuel_policy"`
	FuelCodeType     *string           `json:"fuel_code_type"`
}

type TransactionDriver struct {
	ID            string  `json:"id"`
	FirstName     string  `json:"first_name"`
	LastName      string  `json:"last_name"`
	Phone         string  `json:"phone"`
	Email         *string `json:"email"`
	IntegrationID *string `json:"integration_id"`
}

type Merchant struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Number string `json:"number"`
}

type Location struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	FuelMerchantLocationID string  `json:"fuel_merchant_location_id"`
	Address                string  `json:"address"`
	City                   string  `json:"city"`
	State                  string  `json:"state"`
	ZipCode                string  `json:"zip_code"`
	Latitude               float64 `json:"latitude"`
	Longitude              float64 `json:"longitude"`
	OPISID                 *string `json:"opis_id"`
	Timezone               string  `json:"timezone"`
}

type Prompt struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type FuelItem struct {
	FuelType               string `json:"fuel_type"`
	FuelTypeDescription    string `json:"fuel_type_description"`
	FuelProductCode        string `json:"fuel_product_code"`
	RetailPricePerUnit     string `json:"retail_price_per_unit"`
	DiscountedPricePerUnit string `json:"discounted_price_per_unit"`
	Volume                 string `json:"volume"`
	VolumeUOM              string `json:"volume_uom"`
	TotalRetailPrice       string `json:"total_retail_price"`
	TotalDiscountedPrice   string `json:"total_discounted_price"`
	Fees                   []Fee  `json:"fees"`
}

type Product struct {
	ProductType            string `json:"product_type"`
	ProductTypeDescription string `json:"product_type_description"`
	PurchasePriceTotal     string `json:"purchase_price_total"`
	Quantity               string `json:"quantity"`
	PricePerUnit           string `json:"price_per_unit"`
	Fees                   []Fee  `json:"fees"`
}

type Fee struct {
	Type   string `json:"type"`
	Amount string `json:"amount"`
}

type FuelPolicy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) FetchTransactions(ctx context.Context, start, end time.Time) ([]Transaction, error) {
	endpoint, err := url.Parse(c.baseURL + "/fuel/transactions/")
	if err != nil {
		return nil, fmt.Errorf("build Relay transactions URL: %w", err)
	}

	query := endpoint.Query()
	query.Set("dtstart", start.UTC().Format(time.RFC3339))
	query.Set("dtend", end.UTC().Format(time.RFC3339))
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Relay transactions request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Relay transactions: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = response.Status
		}
		return nil, fmt.Errorf("Relay transactions API returned %s: %s", response.Status, message)
	}

	var transactions []Transaction
	if err := json.NewDecoder(response.Body).Decode(&transactions); err != nil {
		return nil, fmt.Errorf("decode Relay transactions: %w", err)
	}
	return transactions, nil
}
