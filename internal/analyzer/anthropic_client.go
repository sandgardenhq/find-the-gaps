package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const anthropicAPIURL = "https://api.anthropic.com"
const anthropicVersion = "2023-06-01"
const anthropicMaxTokens = 4096

type anthropicClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func newAnthropicClient(baseURL, apiKey, model string) *anthropicClient {
	return &anthropicClient{baseURL: baseURL, apiKey: apiKey, model: model, http: &http.Client{}}
}

// NewAnthropicClient creates an LLMClient that calls the Anthropic Messages API directly.
func NewAnthropicClient(apiKey, model string) *anthropicClient {
	return newAnthropicClient(anthropicAPIURL, apiKey, model)
}

func (c *anthropicClient) Complete(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":      c.model,
		"max_tokens": anthropicMaxTokens,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic API: status %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("anthropic API: no content in response")
	}
	return result.Content[0].Text, nil
}
