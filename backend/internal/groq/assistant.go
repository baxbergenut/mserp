package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type AssistantMessage struct {
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	Name       string              `json:"name,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	ToolCalls  []AssistantToolCall `json:"tool_calls,omitempty"`
}

type AssistantToolCall struct {
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function AssistantFunctionCall `json:"function"`
}

type AssistantFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type AssistantTool struct {
	Type     string                  `json:"type"`
	Function AssistantToolDefinition `json:"function"`
}

type AssistantToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type AssistantCompletion struct {
	Message      AssistantMessage
	FinishReason string
}

func (client *Client) CompleteWithTools(ctx context.Context, messages []AssistantMessage, tools []AssistantTool) (AssistantCompletion, error) {
	if client.apiKey == "" {
		return AssistantCompletion{}, ErrNotConfigured
	}
	payload := map[string]any{
		"model": client.model, "messages": messages, "tools": tools,
		"tool_choice": "auto", "parallel_tool_calls": false,
		"temperature": 0.1, "max_completion_tokens": 1600,
		"reasoning_effort": "medium", "reasoning_format": "hidden", "store": false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AssistantCompletion{}, fmt.Errorf("encode assistant request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(client.baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AssistantCompletion{}, fmt.Errorf("create assistant request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return AssistantCompletion{}, fmt.Errorf("call Groq assistant: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return AssistantCompletion{}, fmt.Errorf("read Groq assistant response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var value struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(responseBody, &value)
		message := strings.TrimSpace(value.Error.Message)
		if message == "" {
			message = response.Status
		}
		return AssistantCompletion{}, fmt.Errorf("Groq assistant rejected request: %s", message)
	}
	var completion struct {
		Choices []struct {
			Message      AssistantMessage `json:"message"`
			FinishReason string           `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &completion); err != nil {
		return AssistantCompletion{}, fmt.Errorf("decode Groq assistant response: %w", err)
	}
	if len(completion.Choices) == 0 {
		return AssistantCompletion{}, fmt.Errorf("Groq assistant returned no choices")
	}
	return AssistantCompletion{Message: completion.Choices[0].Message, FinishReason: completion.Choices[0].FinishReason}, nil
}
