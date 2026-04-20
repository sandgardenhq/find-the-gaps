package analyzer

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/tiktoken-go/tokenizer"
)

// TokenCounter counts the tokens in a text string for a specific LLM provider.
// Used by MapFeaturesToCode to validate batch prompts before sending.
type TokenCounter interface {
	CountTokens(ctx context.Context, text string) (int, error)
}

var defaultEnc = mustGetEncoder()

func mustGetEncoder() tokenizer.Codec {
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		panic("tiktoken: failed to load cl100k_base: " + err.Error())
	}
	return enc
}

// countTokens is a fast package-private estimator using embedded cl100k_base.
// Used by batchSymLines for initial sizing only — no network calls.
func countTokens(s string) int {
	if s == "" {
		return 0
	}
	ids, _, err := defaultEnc.Encode(s)
	if err != nil {
		return 1 // conservative: treat as non-empty to avoid zero-cost miscounts
	}
	return len(ids)
}

// tiktokenCounter implements TokenCounter using the local cl100k_base vocabulary.
type tiktokenCounter struct{}

// NewTiktokenCounter returns a TokenCounter backed by the embedded cl100k_base vocabulary.
// Use this for OpenAI and Ollama providers.
func NewTiktokenCounter() TokenCounter { return &tiktokenCounter{} }

func (c *tiktokenCounter) CountTokens(_ context.Context, text string) (int, error) {
	return countTokens(text), nil
}

// anthropicCounter implements TokenCounter using the Anthropic token counting API.
// Unit tests are omitted because this type requires a live Anthropic endpoint.
// It is covered by integration tests (go test -tags integration).
type anthropicCounter struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicCounter returns a TokenCounter that calls POST /v1/messages/count_tokens.
// Gives exact token counts for Claude models.
// Pass baseURL as "" to use the default Anthropic API endpoint.
func NewAnthropicCounter(apiKey, model, baseURL string) TokenCounter {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := anthropic.NewClient(opts...)
	return &anthropicCounter{client: &client, model: model}
}

func (c *anthropicCounter) CountTokens(ctx context.Context, text string) (int, error) {
	resp, err := c.client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
		Model: anthropic.Model(c.model),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
		},
	})
	if err != nil {
		return 0, fmt.Errorf("anthropic count tokens: %w", err)
	}
	return int(resp.InputTokens), nil
}
