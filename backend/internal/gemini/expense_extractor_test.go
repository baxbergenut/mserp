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
		_, _ = w.Write([]byte(`{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"isExpense\":true,\"confidence\":0.97,\"company\":\"MS Express\",\"category\":\"Safety\",\"expenseDate\":\"2026-09-23\",\"unitNumber\":\"101\",\"driverName\":null,\"amount\":\"15.00\",\"paymentType\":\"EFS\",\"expenseType\":\"Scale\",\"referenceNumber\":null,\"description\":\"Scale ticket\",\"coveredBy\":\"Company\",\"paidBy\":null,\"evidence\":[\"$15 scale\"]}"}]}]}`))
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
