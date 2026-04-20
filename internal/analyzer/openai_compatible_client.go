package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAICompatibleClient calls any server that implements the OpenAI chat
// completions API — Ollama, LM Studio, or a custom endpoint.
type OpenAICompatibleClient struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

// NewOpenAICompatibleClient creates a client for the given base URL and model.
// apiKey is optional; pass an empty string for local servers that don't require auth.
func NewOpenAICompatibleClient(baseURL, model, apiKey string) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		baseURL: baseURL,
		model:   model,
		apiKey:  apiKey,
		http:    &http.Client{},
	}
}

type oaiRequest struct {
	Model    string       `json:"model"`
	Messages []oaiMessage `json:"messages"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete sends prompt as a user message and returns the first completion.
func (c *OpenAICompatibleClient) Complete(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(oaiRequest{
		Model:    c.model,
		Messages: []oaiMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai-compatible request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("openai-compatible: status %d: %s", resp.StatusCode, b)
	}

	var out oaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("openai-compatible: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai-compatible: no choices in response")
	}
	return out.Choices[0].Message.Content, nil
}
