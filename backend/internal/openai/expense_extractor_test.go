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
	"time"
)

func TestExtractExpenseMultimodal(t *testing.T) {
	for _, mime := range []string{"", "image/png", "application/pdf", "text/plain"} {
		t.Run(mime, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("wrong endpoint/auth")
				}
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				format := request["text"].(map[string]any)["format"].(map[string]any)
				if format["type"] != "json_schema" || format["strict"] != true || request["store"] != false || request["model"] != "gpt-6-luna" {
					t.Error("wrong structured request")
				}
				parts := request["input"].([]any)[0].(map[string]any)["content"].([]any)
				if mime != "" {
					part := parts[1].(map[string]any)
					want := map[string]string{"image/png": "input_image", "application/pdf": "input_file", "text/plain": "input_text"}[mime]
					if part["type"] != want {
						t.Errorf("part=%v", part)
					}
					if mime == "application/pdf" && part["file_data"] != "data:application/pdf;base64,cmVjZWlwdA==" {
						t.Error("missing PDF data")
					}
				}
				body := map[string]any{"ok": true, "items": []any{map[string]any{"amt": "125.00", "costBreakdown": []any{map[string]any{"name": "Oil change", "amount": "125.00"}}}}}
				raw, _ := json.Marshal(body)
				json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"content": []any{map[string]any{"type": "output_text", "text": string(raw)}}}}})
			}))
			defer server.Close()
			client := NewClient("secret", "gpt-6-luna")
			client.baseURL = server.URL
			input := ExpenseInput{Text: "receipt", MIMEType: mime, FileName: "receipt.pdf", MessageDate: time.Now()}
			if mime != "" {
				input.FileData = []byte("receipt")
			}
			result, err := client.ExtractExpense(context.Background(), input)
			if err != nil || !result.IsExpense || len(result.Expenses) != 1 || len(result.Expenses[0].CostBreakdown) != 1 {
				t.Fatal(result, err)
			}
		})
	}
}

func TestExtractionRejectsFailureWithoutProviderData(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{401, "secret source data"}, {200, `{"status":"incomplete","output":[]}`}, {200, `{"status":"completed","output":[{"content":[{"type":"refusal"}]}]}`}, {200, `{"status":"completed","output":[]}`}, {200, "bad json"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
		client := NewClient("secret", "gpt-6-luna")
		client.baseURL = server.URL
		_, err := client.ExtractExpense(context.Background(), ExpenseInput{Text: "receipt"})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "secret source data") {
			t.Fatal(err)
		}
	}
	if _, err := NewClient("", "gpt-6-luna").ExtractExpense(context.Background(), ExpenseInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}

func TestExpensePromptAndSchema(t *testing.T) {
	prompt := expensePrompt(ExpenseInput{Text: "Oil change $125", MessageDate: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Categories: map[string][]string{"Maintenance": {"Oil change", "Tire issue"}}})
	for _, want := range []string{"untrusted source data", "2026-10-14", "costBreakdown", "EXACTLY", "Tire issue", "empty costBreakdown"} {
		if !strings.Contains(prompt, want) {
			t.Fatal("missing prompt instruction", want)
		}
	}
	var check func(map[string]any)
	check = func(schema map[string]any) {
		if schema["type"] == "object" {
			if schema["additionalProperties"] != false {
				t.Fatal("non-strict object")
			}
			props := schema["properties"].(map[string]any)
			if len(schema["required"].([]string)) != len(props) {
				t.Fatal("optional property")
			}
			for _, p := range props {
				check(p.(map[string]any))
			}
		}
		if schema["type"] == "array" {
			check(schema["items"].(map[string]any))
		}
	}
	check(expenseSchema())
}

// Explicit opt-in; uses synthetic receipts and never creates ledger records.
func TestExpenseExtractionLive(t *testing.T) {
	if os.Getenv("MSERP_OPENAI_LIVE_TEST") != "1" {
		t.Skip("opt-in live OpenAI extraction")
	}
	client := NewClient(os.Getenv("OPENAI_API_KEY"), "gpt-6-luna")
	for _, kind := range []string{"text", "image", "pdf"} {
		t.Run(kind, func(t *testing.T) {
			input := ExpenseInput{MessageDate: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Categories: map[string][]string{"Maintenance": {"Oil change", "Tire issue"}}, PaymentMethods: []string{"Cash"}}
			if kind == "text" {
				input.Text = "Maintenance invoice dated 2026-10-14 for truck 101: Oil change $100.10; Tire repair $20.20; Total $120.30. Paid cash."
			} else {
				path := os.Getenv("MSERP_OPENAI_TEST_RECEIPT_" + strings.ToUpper(kind))
				if path == "" {
					t.Fatal("missing synthetic receipt")
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				input.FileData = data
				if kind == "image" {
					input.MIMEType = "image/png"
					input.FileName = "receipt.png"
				} else {
					input.MIMEType = "application/pdf"
					input.FileName = "receipt.pdf"
				}
			}
			result, err := client.ExtractExpense(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsExpense || len(result.Expenses) != 1 {
				t.Fatal("expected one expense")
			}
			item := result.Expenses[0]
			if item.Amount == nil || *item.Amount != "120.30" || len(item.CostBreakdown) != 2 {
				t.Fatalf("unexpected synthetic result: %+v", item)
			}
			names := map[string]string{}
			for _, row := range item.CostBreakdown {
				names[row.Name] = row.Amount
			}
			if names["Oil change"] != "100.10" || names["Tire issue"] != "20.20" {
				t.Fatal("default naming/breakdown mismatch", names)
			}
		})
	}
}
