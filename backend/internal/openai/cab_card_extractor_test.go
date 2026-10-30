package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestExtractCabCardLunaRequestAndNormalization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("wrong endpoint or auth")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		format := request["text"].(map[string]any)["format"].(map[string]any)
		if request["model"] != "gpt-6-luna" || request["store"] != false || format["strict"] != true || format["name"] != "cab_card_extraction" {
			t.Fatal("wrong Luna structured extraction contract")
		}
		schema := format["schema"].(map[string]any)
		if schema["additionalProperties"] != false || len(schema["required"].([]any)) != 8 {
			t.Fatal("cab-card fields must be allowlisted and required")
		}
		parts := request["input"].([]any)[0].(map[string]any)["content"].([]any)
		if len(parts) != 3 || parts[1].(map[string]any)["image_url"] != "data:image/png;base64,aW1hZ2U=" || parts[2].(map[string]any)["type"] != "input_image" {
			t.Fatal("missing cab-card page images")
		}
		raw := `{"unitNumber":" 1042 ","vin":"1fujgldr0dlbs1234","year":2021,"make":" Freightliner ","model":" Cascadia ","licensePlate":" ab 123 ","licenseState":"il","registrationExpires":"2027-01-31"}`
		json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"content": []any{map[string]any{"type": "output_text", "text": raw}}}}})
	}))
	defer server.Close()
	client := NewClient("secret", "gpt-6-luna")
	client.baseURL = server.URL
	fields, err := client.ExtractCabCard(context.Background(), []Image{{ContentType: "image/png", Data: []byte("image")}, {ContentType: "image/jpeg", Data: []byte("page 2")}})
	if err != nil || fields.UnitNumber != "1042" || fields.VIN != "1FUJGLDR0DLBS1234" || fields.LicensePlate != "AB123" || fields.LicenseState != "IL" || fields.Make != "Freightliner" || fields.Model != "Cascadia" {
		t.Fatal(fields, err)
	}
	year := 1800
	fields = normalizeCabCardFields(CabCardFields{VIN: "partial", Year: &year, LicenseState: "Illinois", RegistrationExpires: "January 2027"})
	if fields.VIN != "" || fields.Year != nil || fields.LicenseState != "" || fields.RegistrationExpires != "" {
		t.Fatal("unreadable values were retained", fields)
	}
}

func TestCabCardFailuresKeepProviderDataPrivate(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{401, "private provider source"},
		{200, `{"status":"incomplete","output":[]}`},
		{200, `{"status":"completed","output":[{"content":[{"type":"refusal"}]}]}`},
		{200, `{"status":"completed","output":[]}`},
		{200, `{"status":"completed","output":[{"content":[{"type":"output_text","text":"not JSON"}]}]}`},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
		client := NewClient("secret", "gpt-6-luna")
		client.baseURL = server.URL
		_, err := client.ExtractCabCard(context.Background(), []Image{{ContentType: "image/png", Data: []byte("image")}})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private provider source") {
			t.Fatal(err)
		}
	}
	if _, err := NewClient("", "gpt-6-luna").ExtractCabCard(context.Background(), nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	client := NewClient("secret", "gpt-6-luna")
	for _, images := range [][]Image{nil, make([]Image, 4), {{ContentType: "text/plain", Data: []byte("text")}}, {{ContentType: "image/png"}}} {
		if _, err := client.ExtractCabCard(context.Background(), images); err == nil {
			t.Fatal("invalid image input accepted")
		}
	}
}

// Explicit opt-in with a synthetic cab-card image; ordinary tests never call OpenAI.
func TestCabCardLunaLive(t *testing.T) {
	if os.Getenv("MSERP_OPENAI_CAB_CARD_LIVE_TEST") != "1" {
		t.Skip("opt-in synthetic cab-card extraction")
	}
	data, err := os.ReadFile(os.Getenv("MSERP_OPENAI_CAB_CARD_LIVE_IMAGE"))
	if err != nil {
		t.Fatal(err)
	}
	model := os.Getenv("OPENAI_EXPENSE_MODEL")
	if model == "" {
		model = "gpt-6-luna"
	}
	fields, err := NewClient(os.Getenv("OPENAI_API_KEY"), model).ExtractCabCard(context.Background(), []Image{{ContentType: "image/png", Data: data}})
	if err != nil || fields.UnitNumber != "1042" || fields.VIN != "1FUJGLDR0DLBS1234" || fields.Year == nil || *fields.Year != 2021 || fields.LicensePlate != "AB123" || fields.LicenseState != "IL" || fields.RegistrationExpires != "2027-01-31" {
		t.Fatal("synthetic cab-card extraction mismatch", fields, err)
	}
}
