package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExtractExpenseUsesStructuredMultimodalRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "secret" {
			t.Errorf("missing API key header")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		format, ok := request["response_format"].(map[string]any)
		if !ok || format["mime_type"] != "application/json" || format["schema"] == nil || request["store"] != false {
			t.Errorf("response format = %#v, store = %#v", format, request["store"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"ok\":true,\"items\":[{\"conf\":0.97,\"co\":\"MS Express\",\"cat\":\"Safety\",\"date\":\"2026-09-23\",\"unit\":\"101\",\"driver\":null,\"amt\":\"15.00\",\"pay\":\"EFS\",\"kind\":\"Scale\",\"ref\":null,\"desc\":\"Scale ticket\",\"cover\":\"Company\",\"paid\":null,\"ev\":[\"$15 scale\"]}]}"}]}]}`))
	}))
	defer server.Close()

	client := NewClient("secret", "gemini-test")
	client.baseURL = server.URL
	extraction, err := client.ExtractExpense(context.Background(), ExpenseInput{
		Text: "scale", MessageDate: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		MIMEType: "image/jpeg", FileName: "ticket.jpg", FileData: []byte("image"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !extraction.IsExpense || len(extraction.Expenses) != 1 || extraction.Expenses[0].Amount == nil || *extraction.Expenses[0].Amount != "15.00" {
		t.Fatalf("extraction = %#v", extraction)
	}
}

func TestExtractExpenseFallsBackOnCapacityFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if requests == 1 {
			if request["model"] != "primary-model" {
				t.Fatalf("first model = %#v", request["model"])
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"high demand"}}`))
			return
		}
		if request["model"] != "gemini-3-flash-preview" {
			t.Fatalf("fallback model = %#v", request["model"])
		}
		_, _ = w.Write([]byte(`{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"ok\":false,\"items\":[]}"}]}]}`))
	}))
	defer server.Close()

	client := NewClient("secret", "primary-model")
	client.baseURL = server.URL
	if _, err := client.ExtractExpense(context.Background(), ExpenseInput{MessageDate: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestExpenseSchemaStaysWithinGeminiComplexityLimit(t *testing.T) {
	encoded, err := json.Marshal(expenseSchema())
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 1800 {
		t.Fatalf("expense schema is %d bytes; keep the Gemini wire schema compact", len(encoded))
	}
	value := string(encoded)
	if strings.Contains(value, `"description"`) || strings.Contains(value, `"company"`) {
		t.Fatalf("expense schema contains verbose wire fields: %s", value)
	}
	if !strings.Contains(value, `"items"`) || !strings.Contains(value, `"amt"`) {
		t.Fatalf("expense schema is missing compact batch fields: %s", value)
	}
}
