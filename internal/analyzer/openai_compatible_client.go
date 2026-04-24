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

// CompleteWithTools sends a multi-turn message list to the chat completions endpoint
// and returns the assistant's reply. OpenAI-compatible servers (Ollama, LM Studio)
// accept the same /v1/chat/completions endpoint for multi-turn conversations.
// Tool definitions are currently ignored — this implementation provides the interface
// contract so ollama/lmstudio providers satisfy ToolLLMClient.
func (c *OpenAICompatibleClient) CompleteWithTools(ctx context.Context, messages []ChatMessage, _ []Tool) (ChatMessage, error) {
	oaiMsgs := make([]oaiMessage, len(messages))
	for i, m := range messages {
		oaiMsgs[i] = oaiMessage{Role: m.Role, Content: m.Content}
	}

	body, err := json.Marshal(oaiRequest{
		Model:    c.model,
		Messages: oaiMsgs,
	})
	if err != nil {
		return ChatMessage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("openai-compatible request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return ChatMessage{}, fmt.Errorf("openai-compatible: status %d: %s", resp.StatusCode, b)
	}

	var out oaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ChatMessage{}, fmt.Errorf("openai-compatible: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return ChatMessage{}, fmt.Errorf("openai-compatible: no choices in response")
	}
	return ChatMessage{Role: "assistant", Content: out.Choices[0].Message.Content}, nil
}

// CompleteJSON sends prompt and requests a response conforming to schema. The
// exact wire format depends on the server's flavor (Ollama top-level `format`,
// LM Studio / generic OpenAI-compatible `response_format`). Implemented in
// Phase 2; stub returns not-implemented until then.
func (c *OpenAICompatibleClient) CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error) {
	return nil, fmt.Errorf("OpenAICompatibleClient.CompleteJSON: not implemented yet")
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
