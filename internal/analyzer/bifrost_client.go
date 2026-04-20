package analyzer

import (
	"context"
	"fmt"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

// bifrostRequester is the subset of the Bifrost SDK used by BifrostClient.
// It allows injection of a test double without modifying production code paths.
type bifrostRequester interface {
	ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError)
}

// BifrostClient implements LLMClient using the Bifrost Go SDK.
type BifrostClient struct {
	client   bifrostRequester
	provider schemas.ModelProvider
	model    string
}

type bifrostAccount struct {
	provider schemas.ModelProvider
	apiKey   string
}

func (a *bifrostAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return []schemas.ModelProvider{a.provider}, nil
}

func (a *bifrostAccount) GetKeysForProvider(ctx context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
	if provider == a.provider {
		return []schemas.Key{{
			Value:  *schemas.NewEnvVar(a.apiKey),
			Models: schemas.WhiteList{"*"},
			Weight: 1.0,
		}}, nil
	}
	return nil, fmt.Errorf("unsupported provider: %s", provider)
}

func (a *bifrostAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	if provider == a.provider {
		return &schemas.ProviderConfig{
			NetworkConfig:            schemas.DefaultNetworkConfig,
			ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
		}, nil
	}
	return nil, fmt.Errorf("unsupported provider: %s", provider)
}

// NewBifrostClientWithProvider creates a BifrostClient for the named provider.
// providerName must be "anthropic" or "openai".
func NewBifrostClientWithProvider(providerName, apiKey, model string) (*BifrostClient, error) {
	var provider schemas.ModelProvider
	switch providerName {
	case "anthropic":
		provider = schemas.Anthropic
	case "openai":
		provider = schemas.OpenAI
	default:
		return nil, fmt.Errorf("unsupported Bifrost provider: %q", providerName)
	}

	account := &bifrostAccount{provider: provider, apiKey: apiKey}
	client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: account})
	if err != nil {
		return nil, fmt.Errorf("bifrost init: %w", err)
	}
	return &BifrostClient{client: client, provider: provider, model: model}, nil
}

// Complete sends a user prompt and returns the first completion text.
func (c *BifrostClient) Complete(ctx context.Context, prompt string) (string, error) {
	bifrostCtx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	resp, bifrostErr := c.client.ChatCompletionRequest(bifrostCtx, &schemas.BifrostChatRequest{
		Provider: c.provider,
		Model:    c.model,
		Input: []schemas.ChatMessage{
			{
				Role:    schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr(prompt)},
			},
		},
	})
	if bifrostErr != nil {
		if bifrostErr.Error != nil {
			return "", fmt.Errorf("bifrost completion: %s", bifrostErr.Error.Message)
		}
		return "", fmt.Errorf("bifrost completion: unknown error")
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("bifrost completion: no choices returned")
	}
	content := resp.Choices[0].Message.Content
	if content == nil || content.ContentStr == nil {
		return "", fmt.Errorf("bifrost completion: nil content")
	}
	return *content.ContentStr, nil
}
