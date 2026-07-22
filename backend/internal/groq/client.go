package groq

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.groq.com/openai/v1"

var ErrNotConfigured = errors.New("GROQ_API_KEY is not configured")

type Image struct {
	ContentType string
	Data        []byte
}

type CabCardFields struct {
	UnitNumber          string `json:"unitNumber"`
	VIN                 string `json:"vin"`
	Year                *int   `json:"year"`
	Make                string `json:"make"`
	Model               string `json:"model"`
	LicensePlate        string `json:"licensePlate"`
	LicenseState        string `json:"licenseState"`
	RegistrationExpires string `json:"registrationExpires"`
}

type CDLFields struct {
	FullName       string `json:"fullName"`
	LicenseNumber  string `json:"licenseNumber"`
	LicenseState   string `json:"licenseState"`
	LicenseExpires string `json:"licenseExpires"`
	Address        string `json:"address"`
	City           string `json:"city"`
	State          string `json:"state"`
	PostalCode     string `json:"postalCode"`
}

type DocumentExtractor interface {
	ExtractCabCard(context.Context, []Image) (CabCardFields, error)
	ExtractCDL(context.Context, []Image) (CDLFields, error)
}

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (client *Client) ExtractCabCard(ctx context.Context, images []Image) (CabCardFields, error) {
	var fields CabCardFields
	if err := client.extractDocument(ctx, images, cabCardPrompt, "cab card", &fields); err != nil {
		return CabCardFields{}, err
	}
	return normalizeCabCardFields(fields), nil
}

func (client *Client) ExtractCDL(ctx context.Context, images []Image) (CDLFields, error) {
	var fields CDLFields
	if err := client.extractDocument(ctx, images, cdlPrompt, "CDL", &fields); err != nil {
		return CDLFields{}, err
	}
	return normalizeCDLFields(fields), nil
}

func (client *Client) extractDocument(
	ctx context.Context,
	images []Image,
	prompt string,
	label string,
	destination any,
) error {
	if client.apiKey == "" {
		return ErrNotConfigured
	}
	if len(images) == 0 {
		return fmt.Errorf("at least one %s image is required", label)
	}
	if len(images) > 3 {
		return fmt.Errorf("at most three %s images are supported", label)
	}

	content := make([]map[string]any, 0, len(images)+1)
	content = append(content, map[string]any{
		"type": "text",
		"text": prompt,
	})
	for _, image := range images {
		content = append(content, map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": "data:" + image.ContentType + ";base64," + base64.StdEncoding.EncodeToString(image.Data),
			},
		})
	}

	payload := map[string]any{
		"model": client.model,
		"messages": []map[string]any{{
			"role":    "user",
			"content": content,
		}},
		"response_format":       map[string]string{"type": "json_object"},
		"reasoning_effort":      "none",
		"temperature":           0.1,
		"max_completion_tokens": 800,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode GROQ request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(client.baseURL, "/")+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create GROQ request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call GROQ: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read GROQ response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(responseBody, &apiError)
		message := strings.TrimSpace(apiError.Error.Message)
		if message == "" {
			message = response.Status
		}
		return fmt.Errorf("GROQ rejected the %s: %s", label, message)
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.Unmarshal(responseBody, &completion); err != nil {
		return fmt.Errorf("decode GROQ response: %w", err)
	}
	if len(completion.Choices) == 0 || strings.TrimSpace(completion.Choices[0].Message.Content) == "" {
		return fmt.Errorf("GROQ returned no %s fields", label)
	}
	if err = json.Unmarshal([]byte(completion.Choices[0].Message.Content), destination); err != nil {
		return fmt.Errorf("decode extracted %s fields: %w", label, err)
	}
	return nil
}

const cabCardPrompt = `Read this IRP cab card and return a JSON object with exactly these keys:
unitNumber, vin, year, make, model, licensePlate, licenseState, registrationExpires.
Use the power unit/equipment/unit number for unitNumber. VIN must be the full 17-character vehicle identification number. licenseState must be the two-letter plate jurisdiction. registrationExpires must use YYYY-MM-DD. year must be a JSON integer. Use null for an unknown year and an empty string for every other unknown field. Never guess or infer an unreadable value. Do not include explanations or markdown.`

const cdlPrompt = `Read this commercial driver's license (CDL), including front and back images when provided, and return a JSON object with exactly these keys:
fullName, licenseNumber, licenseState, licenseExpires, address, city, state, postalCode.
fullName must be the license holder's full legal name in normal First Middle Last order. licenseNumber is the driver's license or CDL number. licenseState is the two-letter issuing jurisdiction. licenseExpires must use YYYY-MM-DD. Split the residential/mailing address into street address, city, two-letter state, and postalCode. Use an empty string for every unreadable or unknown field. Never guess or infer a value. Do not include explanations or markdown.`

func normalizeCabCardFields(fields CabCardFields) CabCardFields {
	fields.UnitNumber = strings.TrimSpace(fields.UnitNumber)
	fields.VIN = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(fields.VIN), " ", ""))
	if len(fields.VIN) != 17 {
		fields.VIN = ""
	}
	if fields.Year != nil && (*fields.Year < 1900 || *fields.Year > 2200) {
		fields.Year = nil
	}
	fields.Make = strings.TrimSpace(fields.Make)
	fields.Model = strings.TrimSpace(fields.Model)
	fields.LicensePlate = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(fields.LicensePlate), " ", ""))
	fields.LicenseState = strings.ToUpper(strings.TrimSpace(fields.LicenseState))
	if len(fields.LicenseState) != 2 {
		fields.LicenseState = ""
	}
	fields.RegistrationExpires = strings.TrimSpace(fields.RegistrationExpires)
	if fields.RegistrationExpires != "" {
		if _, err := time.Parse("2006-01-02", fields.RegistrationExpires); err != nil {
			fields.RegistrationExpires = ""
		}
	}
	return fields
}

func normalizeCDLFields(fields CDLFields) CDLFields {
	fields.FullName = strings.Join(strings.Fields(fields.FullName), " ")
	fields.LicenseNumber = strings.ToUpper(strings.TrimSpace(fields.LicenseNumber))
	fields.LicenseState = strings.ToUpper(strings.TrimSpace(fields.LicenseState))
	if len(fields.LicenseState) != 2 {
		fields.LicenseState = ""
	}
	fields.LicenseExpires = strings.TrimSpace(fields.LicenseExpires)
	if fields.LicenseExpires != "" {
		if _, err := time.Parse("2006-01-02", fields.LicenseExpires); err != nil {
			fields.LicenseExpires = ""
		}
	}
	fields.Address = strings.Join(strings.Fields(fields.Address), " ")
	fields.City = strings.Join(strings.Fields(fields.City), " ")
	fields.State = strings.ToUpper(strings.TrimSpace(fields.State))
	if len(fields.State) != 2 {
		fields.State = ""
	}
	fields.PostalCode = strings.TrimSpace(fields.PostalCode)
	return fields
}
