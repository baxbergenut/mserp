package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompleteWithToolsDecodesStringArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"resolve_date_range","arguments":"{\"period\":\"last_week\"}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer server.Close()
	client := NewClient("secret", "openai/gpt-oss-120b")
	client.baseURL = server.URL
	completion, err := client.CompleteWithTools(context.Background(), []AssistantMessage{{Role: "user", Content: "last week"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(completion.Message.ToolCalls) != 1 || completion.Message.ToolCalls[0].Function.Arguments != `{"period":"last_week"}` {
		t.Fatalf("unexpected tool call: %#v", completion.Message.ToolCalls)
	}
}
