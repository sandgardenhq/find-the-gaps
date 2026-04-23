package analyzer

import (
	"context"
	"encoding/json"
	"fmt"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

// bifrostRequester is the subset of the Bifrost SDK used by BifrostClient.
// It allows injection of a test double without modifying production code paths.
type bifrostRequester interface {
	ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError)
}

// bifrostMaxCompletionTokens caps every completion's output length. Bifrost's Anthropic
// provider silently defaults to 4096 for any model it doesn't recognize (e.g.
// claude-opus-4-7 is missing from its static map as of v1.5.2), which truncates
// large mapper responses and surfaces as "unexpected end of JSON input". 32k is
// within the supported output window of every current Opus/Sonnet/Haiku model.
const bifrostMaxCompletionTokens = 32_000

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
		nc := schemas.DefaultNetworkConfig
		nc.DefaultRequestTimeoutInSeconds = 300 // 5 minutes — feature mapping batches can be large
		return &schemas.ProviderConfig{
			NetworkConfig:            nc,
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

// CompleteWithTools sends a multi-turn conversation with tool definitions and
// returns the LLM's next message (which may contain tool call requests or a
// final text response).
func (c *BifrostClient) CompleteWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (ChatMessage, error) {
	// Convert our ChatMessage slice to Bifrost schema messages.
	bifrostMsgs := make([]schemas.ChatMessage, 0, len(messages))
	for _, m := range messages {
		bm := schemas.ChatMessage{}
		switch m.Role {
		case "user":
			bm.Role = schemas.ChatMessageRoleUser
			bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
		case "assistant":
			bm.Role = schemas.ChatMessageRoleAssistant
			if len(m.ToolCalls) > 0 {
				calls := make([]schemas.ChatAssistantMessageToolCall, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					id := tc.ID
					name := tc.Name
					calls[i] = schemas.ChatAssistantMessageToolCall{
						ID: &id,
						Function: schemas.ChatAssistantMessageToolCallFunction{
							Name:      &name,
							Arguments: tc.Arguments,
						},
					}
				}
				bm.ChatAssistantMessage = &schemas.ChatAssistantMessage{ToolCalls: calls}
			} else {
				bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
			}
		case "tool":
			bm.Role = schemas.ChatMessageRoleTool
			id := m.ToolCallID
			bm.ChatToolMessage = &schemas.ChatToolMessage{ToolCallID: &id}
			bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
		}
		bifrostMsgs = append(bifrostMsgs, bm)
	}

	// Convert our Tool slice to Bifrost ChatTool slice.
	bifrostTools := make([]schemas.ChatTool, len(tools))
	for i, t := range tools {
		paramsJSON, _ := json.Marshal(t.Parameters)
		var params schemas.ToolFunctionParameters
		_ = json.Unmarshal(paramsJSON, &params)
		desc := t.Description
		bifrostTools[i] = schemas.ChatTool{
			Type: schemas.ChatToolTypeFunction,
			Function: &schemas.ChatToolFunction{
				Name:        t.Name,
				Description: &desc,
				Parameters:  &params,
			},
		}
	}

	bifrostCtx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	resp, bifrostErr := c.client.ChatCompletionRequest(bifrostCtx, &schemas.BifrostChatRequest{
		Provider: c.provider,
		Model:    c.model,
		Input:    bifrostMsgs,
		Params: &schemas.ChatParameters{
			Tools:               bifrostTools,
			MaxCompletionTokens: schemas.Ptr(bifrostMaxCompletionTokens),
		},
	})
	if bifrostErr != nil {
		if bifrostErr.Error != nil {
			return ChatMessage{}, fmt.Errorf("bifrost tool completion: %s", bifrostErr.Error.Message)
		}
		return ChatMessage{}, fmt.Errorf("bifrost tool completion: unknown error")
	}
	if len(resp.Choices) == 0 {
		return ChatMessage{}, fmt.Errorf("bifrost tool completion: no choices returned")
	}

	choice := resp.Choices[0]
	result := ChatMessage{Role: "assistant"}

	// Check for tool calls via the embedded ChatAssistantMessage on the response message.
	if choice.Message != nil && choice.Message.ChatAssistantMessage != nil &&
		len(choice.Message.ToolCalls) > 0 {
		tcs := choice.Message.ToolCalls
		calls := make([]ToolCall, len(tcs))
		for i, tc := range tcs {
			id := ""
			if tc.ID != nil {
				id = *tc.ID
			}
			name := ""
			if tc.Function.Name != nil {
				name = *tc.Function.Name
			}
			calls[i] = ToolCall{ID: id, Name: name, Arguments: tc.Function.Arguments}
		}
		result.ToolCalls = calls
	} else if choice.Message != nil && choice.Message.Content != nil && choice.Message.Content.ContentStr != nil {
		result.Content = *choice.Message.Content.ContentStr
	}

	return result, nil
}

// CompleteJSON sends prompt and requests a response conforming to schema,
// returning the raw JSON bytes. Dispatches per provider:
//   - Anthropic: forced tool use with a single "respond" tool whose input_schema
//     equals schema.Doc.
//   - OpenAI: response_format={"type":"json_schema", ...} with strict=true.
//
// Implemented in CompleteJSON_anthropic.go and CompleteJSON_openai.go; this
// method is a dispatcher stub until those land (Phase 2).
func (c *BifrostClient) CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error) {
	return nil, fmt.Errorf("BifrostClient.CompleteJSON: not implemented for provider %q", c.provider)
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
		Params: &schemas.ChatParameters{
			MaxCompletionTokens: schemas.Ptr(bifrostMaxCompletionTokens),
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
