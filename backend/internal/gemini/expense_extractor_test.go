package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		_, _ = w.Write([]byte(`{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"isExpense\":true,\"containsMultipleExpenses\":false,\"confidence\":0.97,\"company\":\"MS Express\",\"category\":\"Safety\",\"expenseDate\":\"2026-09-23\",\"unitNumber\":\"101\",\"driverName\":null,\"amount\":\"15.00\",\"paymentType\":\"EFS\",\"expenseType\":\"Scale\",\"referenceNumber\":null,\"description\":\"Scale ticket\",\"coveredBy\":\"Company\",\"paidBy\":null,\"evidence\":[\"$15 scale\"]}"}]}]}`))
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
	if !extraction.IsExpense || extraction.Amount == nil || *extraction.Amount != "15.00" {
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
		_, _ = w.Write([]byte(`{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"isExpense\":false,\"containsMultipleExpenses\":false,\"confidence\":0,\"company\":null,\"category\":null,\"expenseDate\":null,\"unitNumber\":null,\"driverName\":null,\"amount\":null,\"paymentType\":null,\"expenseType\":null,\"referenceNumber\":null,\"description\":null,\"coveredBy\":null,\"paidBy\":null,\"evidence\":[]}"}]}]}`))
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
