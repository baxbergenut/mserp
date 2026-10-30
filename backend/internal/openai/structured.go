package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Both expense and cab-card extraction use the same configured client and model.
func (c *Client) extractStructured(ctx context.Context, parts []map[string]any, name string, schema map[string]any, maxTokens int) ([]byte, error) {
	if c.apiKey == "" {
		return nil, ErrNotConfigured
	}
	payload := map[string]any{
		"model": c.model, "store": false, "reasoning": map[string]any{"effort": "low"}, "max_output_tokens": maxTokens,
		"input": []map[string]any{{"role": "user", "content": parts}},
		"text":  map[string]any{"format": map[string]any{"type": "json_schema", "name": name, "strict": true, "schema": schema}},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/responses", bytes.NewReader(encoded))
	if err != nil {
		return nil, errors.New("create OpenAI request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, errors.New("OpenAI extraction request failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, errors.New("read OpenAI response failed")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Provider bodies can contain submitted documents or credentials.
		return nil, fmt.Errorf("OpenAI extraction failed (HTTP %d); check model access, API key and quota", response.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.New("decode OpenAI response failed")
	}
	if result.Status != "completed" {
		return nil, errors.New("OpenAI extraction was incomplete; try a smaller document")
	}
	var raw strings.Builder
	for _, output := range result.Output {
		for _, content := range output.Content {
			if content.Type == "refusal" {
				return nil, errors.New("OpenAI could not extract this document")
			}
			if content.Type == "output_text" {
				raw.WriteString(content.Text)
			}
		}
	}
	if raw.Len() == 0 {
		return nil, errors.New("OpenAI returned no extraction")
	}
	return []byte(raw.String()), nil
}
