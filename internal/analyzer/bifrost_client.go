package analyzer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/log"
	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

// formatUsage renders Bifrost token-usage info as a single human-readable line
// for verbose debug logging. Returns "" for a nil usage so call sites can pass
// the result to log.Debugf unconditionally without emitting empty lines on
// providers that do not return usage. Cache-token fields default to 0 when
// PromptTokensDetails is absent (the common case for non-Anthropic providers).
func formatUsage(u *schemas.BifrostLLMUsage) string {
	if u == nil {
		return ""
	}
	var cw, cr int
	if u.PromptTokensDetails != nil {
		cw = u.PromptTokensDetails.CachedWriteTokens
		cr = u.PromptTokensDetails.CachedReadTokens
	}
	return fmt.Sprintf("usage: prompt=%d completion=%d cache_write=%d cache_read=%d",
		u.PromptTokens, u.CompletionTokens, cw, cr)
}

// bifrostRequester is the subset of the Bifrost SDK used by BifrostClient.
// It allows injection of a test double without modifying production code paths.
type bifrostRequester interface {
	ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError)
}

// bifrostDefaultMaxCompletionTokens is the fallback output cap when caps don't
// specify one. Bifrost's Anthropic provider silently defaults to 4096 for any
// model it doesn't recognize (e.g. claude-opus-4-7 is missing from its static
// map as of v1.5.2), which truncates large mapper responses and surfaces as
// "unexpected end of JSON input". 32k is within the supported output window of
// every current Opus/Sonnet/Haiku model. Models with a smaller hard cap (Groq
// llama-4-scout at 8192) override this via ModelCapabilities.MaxCompletionTokens.
const bifrostDefaultMaxCompletionTokens = 32_000

// maxCompletionTokens returns the effective output cap for this client's model:
// the per-model value from capabilities when set, otherwise the default.
func (c *BifrostClient) maxCompletionTokens() int {
	if c.caps.MaxCompletionTokens > 0 {
		return c.caps.MaxCompletionTokens
	}
	return bifrostDefaultMaxCompletionTokens
}

// BifrostClient implements LLMClient using the Bifrost Go SDK.
type BifrostClient struct {
	client   bifrostRequester
	provider schemas.ModelProvider
	model    string
	caps     ModelCapabilities
}

// Capabilities returns the resolved model capabilities recorded at
// construction time. Callers branch on Capabilities().Vision and
// Capabilities().ToolUse instead of inspecting the provider name.
func (c *BifrostClient) Capabilities() ModelCapabilities { return c.caps }

type bifrostAccount struct {
	provider schemas.ModelProvider
	apiKey   string
	baseURL  string
}

func (a *bifrostAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return []schemas.ModelProvider{a.provider}, nil
}

// localServerPlaceholderKey satisfies Bifrost's per-request key filter
// (selectKeyFromProviderForModel + utils.go CanProviderKeyValueBeEmpty), which
// drops empty-value keys for OpenAI. Local OpenAI-compatible servers (LM Studio,
// keyless proxies) ignore the Authorization header in practice, so a non-empty
// placeholder bearer is harmless.
const localServerPlaceholderKey = "local-server-no-auth"

func (a *bifrostAccount) GetKeysForProvider(ctx context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
	if provider == a.provider {
		keyValue := a.apiKey
		if a.provider == schemas.OpenAI && keyValue == "" && a.baseURL != "" {
			keyValue = localServerPlaceholderKey
		}
		key := schemas.Key{
			Value:  *schemas.NewEnvVar(keyValue),
			Models: schemas.WhiteList{"*"},
			Weight: 1.0,
		}
		if a.provider == schemas.Ollama {
			// Bifrost's Ollama provider reads the server URL from Key.OllamaKeyConfig.URL,
			// not NetworkConfig.BaseURL. See /maximhq/bifrost/core providers/ollama/ollama.go.
			key.OllamaKeyConfig = &schemas.OllamaKeyConfig{URL: *schemas.NewEnvVar(a.baseURL)}
		}
		return []schemas.Key{key}, nil
	}
	return nil, fmt.Errorf("unsupported provider: %s", provider)
}

func (a *bifrostAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	if provider == a.provider {
		nc := schemas.DefaultNetworkConfig
		nc.DefaultRequestTimeoutInSeconds = 300 // 5 minutes — feature mapping batches can be large
		if a.baseURL != "" && a.provider != schemas.Ollama {
			// OpenAI / Anthropic honor NetworkConfig.BaseURL for custom endpoints
			// (lmstudio). Ollama routes via OllamaKeyConfig instead.
			nc.BaseURL = a.baseURL
		}
		return &schemas.ProviderConfig{
			NetworkConfig:            nc,
			ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
		}, nil
	}
	return nil, fmt.Errorf("unsupported provider: %s", provider)
}

// NewBifrostClientWithProvider creates a BifrostClient for the named provider.
// providerName must be "anthropic", "openai", "ollama", or "groq". baseURL
// overrides the provider's default endpoint — required for "ollama" and
// "groq", optional for the others (empty string means use the provider's
// default hosted endpoint). "groq" is an OpenAI-compatible alias that routes
// through schemas.OpenAI with a custom BaseURL.
//
// caps records the resolved model capability flags so analyzer code can branch
// on Capabilities() without re-importing cli (which would create a cycle).
func NewBifrostClientWithProvider(providerName, apiKey, model, baseURL string, caps ModelCapabilities) (*BifrostClient, error) {
	var provider schemas.ModelProvider
	switch providerName {
	case "anthropic":
		provider = schemas.Anthropic
	case "openai":
		provider = schemas.OpenAI
	case "ollama":
		provider = schemas.Ollama
		if baseURL == "" {
			return nil, fmt.Errorf("ollama provider requires a baseURL")
		}
	case "groq":
		provider = schemas.OpenAI
		if baseURL == "" {
			return nil, fmt.Errorf("groq provider requires a baseURL")
		}
	case "gateway":
		provider = schemas.OpenAI
		if baseURL == "" {
			return nil, fmt.Errorf("gateway provider requires a baseURL")
		}
	default:
		return nil, fmt.Errorf("unsupported Bifrost provider: %q", providerName)
	}

	account := &bifrostAccount{provider: provider, apiKey: apiKey, baseURL: baseURL}
	client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: account})
	if err != nil {
		return nil, fmt.Errorf("bifrost init: %w", err)
	}
	return &BifrostClient{client: client, provider: provider, model: model, caps: caps}, nil
}

// anthropicCachedContent renders text as a one-element ContentBlocks slice
// carrying ephemeral cache_control. This is the only Bifrost-supported path
// for marking a user/tool message as a cache breakpoint on the Anthropic
// provider — ContentStr does not carry cache_control through to the wire.
// Only call when c.provider == schemas.Anthropic.
func anthropicCachedContent(text string) *schemas.ChatMessageContent {
	return &schemas.ChatMessageContent{
		ContentBlocks: []schemas.ChatContentBlock{{
			Type:         schemas.ChatContentBlockTypeText,
			Text:         schemas.Ptr(text),
			CacheControl: &schemas.CacheControl{Type: schemas.CacheControlTypeEphemeral},
		}},
	}
}

// renderContentBlocks translates a slice of analyzer ContentBlock into the
// Bifrost SDK's []schemas.ChatContentBlock wire format. Both Anthropic and
// OpenAI lanes use the same struct shape: text blocks set Type=Text + Text*,
// image blocks set Type=Image + ImageURLStruct{URL: ...}. Bifrost normalizes
// the on-wire JSON per provider, so this helper is provider-agnostic.
//
// The provider arg is reserved for future provider-specific divergence
// (e.g. base64 vs URL discrimination) but is currently unused — keeping it
// in the signature documents that ContentBlock translation is, in principle,
// a provider-aware operation.
func renderContentBlocks(_ schemas.ModelProvider, blocks []ContentBlock) []schemas.ChatContentBlock {
	out := make([]schemas.ChatContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case ContentBlockText:
			out = append(out, schemas.ChatContentBlock{
				Type: schemas.ChatContentBlockTypeText,
				Text: schemas.Ptr(b.Text),
			})
		case ContentBlockImageURL:
			out = append(out, schemas.ChatContentBlock{
				Type:           schemas.ChatContentBlockTypeImage,
				ImageURLStruct: &schemas.ChatInputImage{URL: b.ImageURL},
			})
		}
	}
	return out
}

// CompleteWithTools runs a multi-turn tool-use conversation. It dispatches
// tool calls through Tool.Execute handlers and feeds the results back to the
// LLM until the model returns a plain-text response or the round limit is
// reached. See runAgentLoop for the loop semantics; this method is a thin
// adapter that wires Bifrost as the per-turn turnFunc.
func (c *BifrostClient) CompleteWithTools(ctx context.Context, messages []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
	return runAgentLoop(ctx, c.completeOneTurn, messages, tools, opts...)
}

// renderBifrostMessages converts the analyzer's []ChatMessage into Bifrost's
// []schemas.ChatMessage, applying provider-specific caching (Anthropic only)
// and multimodal content-block translation. Shared by completeOneTurn and the
// CompleteJSON / CompleteJSONMultimodal paths so all entry points speak the
// same wire format.
func (c *BifrostClient) renderBifrostMessages(messages []ChatMessage) []schemas.ChatMessage {
	bifrostMsgs := make([]schemas.ChatMessage, 0, len(messages))
	for _, m := range messages {
		bm := schemas.ChatMessage{}
		// cacheable is true only when the caller asked for a cache breakpoint
		// AND the provider is Anthropic. Other providers don't speak
		// cache_control; sending it would be a wire-format error.
		cacheable := m.CacheBreakpoint && c.provider == schemas.Anthropic
		switch m.Role {
		case "user":
			bm.Role = schemas.ChatMessageRoleUser
			switch {
			case len(m.ContentBlocks) > 0:
				// Multimodal user message — ContentBlocks wins. We deliberately
				// do NOT also set ContentStr; some providers will silently drop
				// the image and pass the flat text instead if both are present.
				bm.Content = &schemas.ChatMessageContent{
					ContentBlocks: renderContentBlocks(c.provider, m.ContentBlocks),
				}
			case cacheable:
				bm.Content = anthropicCachedContent(m.Content)
			default:
				bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
			}
		case "assistant":
			bm.Role = schemas.ChatMessageRoleAssistant
			if len(m.ToolCalls) > 0 {
				// Assistant tool-call messages carry no text content, so
				// CacheBreakpoint has nothing to attach to. Ignore the flag
				// here — flagging this role is structurally a caller bug.
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
			} else if cacheable {
				bm.Content = anthropicCachedContent(m.Content)
			} else {
				bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
			}
		case "tool":
			bm.Role = schemas.ChatMessageRoleTool
			id := m.ToolCallID
			bm.ChatToolMessage = &schemas.ChatToolMessage{ToolCallID: &id}
			if cacheable {
				bm.Content = anthropicCachedContent(m.Content)
			} else {
				bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
			}
		}
		bifrostMsgs = append(bifrostMsgs, bm)
	}
	return bifrostMsgs
}

// completeOneTurn issues a single Bifrost chat completion and translates the
// result back to a ChatMessage. It is the turnFunc passed to runAgentLoop.
func (c *BifrostClient) completeOneTurn(ctx context.Context, messages []ChatMessage, tools []Tool) (ChatMessage, error) {
	bifrostMsgs := c.renderBifrostMessages(messages)

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
			MaxCompletionTokens: schemas.Ptr(c.maxCompletionTokens()),
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
	if msg := formatUsage(resp.Usage); msg != "" {
		log.Debugf("%s", msg)
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
//     equals schema.Doc (tool_choice forces the model to emit parse-guaranteed
//     JSON as the tool's arguments).
//   - OpenAI: native response_format={"type":"json_schema","json_schema":{...}}
//     with strict=true — the model's content is a JSON string conforming to
//     schema.Doc.
//
// The flat-prompt entry point preserves existing semantics: callers that pass
// a plain prompt get the same Anthropic cache breakpoint as before. Both
// CompleteJSON and CompleteJSONMultimodal share completeJSONMessages once the
// inputs are normalized to a []ChatMessage.
func (c *BifrostClient) CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error) {
	// CacheBreakpoint=true preserves the existing behavior: on Anthropic, the
	// user prompt becomes a cached content block via renderBifrostMessages.
	// Other providers ignore the flag entirely.
	msgs := []ChatMessage{{Role: "user", Content: prompt, CacheBreakpoint: true}}
	return c.completeJSONMessages(ctx, msgs, schema)
}

// CompleteJSONMultimodal is the multimodal sibling of CompleteJSON: callers
// supply pre-built messages (typically with ContentBlocks carrying image URLs)
// instead of a flat prompt string. Schema-forcing and response parsing are
// identical to CompleteJSON. Used by the screenshot vision relevance pass.
//
// Note: image-bearing user messages do NOT request Anthropic prompt caching.
// Caching multimodal blocks is a separate decision; today the relevance pass
// makes one batched call per page, so caching offers no measurable savings.
func (c *BifrostClient) CompleteJSONMultimodal(ctx context.Context, messages []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	return c.completeJSONMessages(ctx, messages, schema)
}

// completeJSONMessages is the shared schema-forcing + parsing path used by
// both CompleteJSON (flat prompt) and CompleteJSONMultimodal (pre-built
// messages). Provider dispatch:
//   - Anthropic: forced "respond" tool whose Parameters equals schema.Doc.
//   - OpenAI / Ollama: response_format=json_schema with strict=true. Bifrost's
//     Ollama provider delegates chat completions to the OpenAI handler so the
//     same code path serves both.
func (c *BifrostClient) completeJSONMessages(ctx context.Context, messages []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	switch c.provider {
	case schemas.Anthropic:
		return c.completeJSONAnthropicMessages(ctx, messages, schema)
	case schemas.OpenAI, schemas.Ollama:
		return c.completeJSONOpenAIMessages(ctx, messages, schema)
	default:
		return nil, fmt.Errorf("BifrostClient.CompleteJSON: not implemented for provider %q", c.provider)
	}
}

// completeJSONOpenAIMessages uses OpenAI's native structured outputs via
// response_format={"type":"json_schema","json_schema":{"name":..., "strict":true,
// "schema": schema.Doc}}. The assistant message content is a JSON string
// conforming to the schema.
func (c *BifrostClient) completeJSONOpenAIMessages(ctx context.Context, messages []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	rf := map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   schema.Name,
			"strict": true,
			"schema": schema.Doc,
		},
	}
	var rfIface any = rf

	bifrostCtx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	resp, bifrostErr := c.client.ChatCompletionRequest(bifrostCtx, &schemas.BifrostChatRequest{
		Provider: c.provider,
		Model:    c.model,
		Input:    c.renderBifrostMessages(messages),
		Params: &schemas.ChatParameters{
			ResponseFormat:      &rfIface,
			MaxCompletionTokens: schemas.Ptr(c.maxCompletionTokens()),
		},
	})
	if bifrostErr != nil {
		if bifrostErr.Error != nil {
			return nil, fmt.Errorf("bifrost CompleteJSON: %s", bifrostErr.Error.Message)
		}
		return nil, fmt.Errorf("bifrost CompleteJSON: unknown error")
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("bifrost CompleteJSON: no choices returned")
	}
	if logMsg := formatUsage(resp.Usage); logMsg != "" {
		log.Debugf("%s", logMsg)
	}
	msg := resp.Choices[0].Message
	if msg == nil || msg.Content == nil || msg.Content.ContentStr == nil {
		return nil, fmt.Errorf("bifrost CompleteJSON: nil content; schema=%q", schema.Name)
	}
	raw := json.RawMessage(*msg.Content.ContentStr)
	if err := schema.ValidateResponse(raw); err != nil {
		return nil, fmt.Errorf("bifrost CompleteJSON: %w", err)
	}
	return raw, nil
}

// completeJSONAnthropicMessages forces the model to emit structured output by
// registering a single tool named "respond" whose parameters schema equals
// schema.Doc and setting tool_choice to require that tool. The tool's call
// arguments are the parse-guaranteed JSON response.
func (c *BifrostClient) completeJSONAnthropicMessages(ctx context.Context, messages []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	const respondToolName = "respond"

	var params schemas.ToolFunctionParameters
	if err := json.Unmarshal(schema.Doc, &params); err != nil {
		return nil, fmt.Errorf("CompleteJSON: schema %q: invalid JSON Schema doc: %w", schema.Name, err)
	}
	desc := fmt.Sprintf("Return the final answer by calling this tool with arguments matching the %s schema.", schema.Name)
	tool := schemas.ChatTool{
		Type: schemas.ChatToolTypeFunction,
		Function: &schemas.ChatToolFunction{
			Name:        respondToolName,
			Description: &desc,
			Parameters:  &params,
		},
	}
	toolChoice := &schemas.ChatToolChoice{
		ChatToolChoiceStruct: &schemas.ChatToolChoiceStruct{
			Type:     schemas.ChatToolChoiceTypeFunction,
			Function: &schemas.ChatToolChoiceFunction{Name: respondToolName},
		},
	}

	bifrostCtx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	resp, bifrostErr := c.client.ChatCompletionRequest(bifrostCtx, &schemas.BifrostChatRequest{
		Provider: c.provider,
		Model:    c.model,
		Input:    c.renderBifrostMessages(messages),
		Params: &schemas.ChatParameters{
			Tools:               []schemas.ChatTool{tool},
			ToolChoice:          toolChoice,
			MaxCompletionTokens: schemas.Ptr(c.maxCompletionTokens()),
		},
	})
	if bifrostErr != nil {
		if bifrostErr.Error != nil {
			return nil, fmt.Errorf("bifrost CompleteJSON: %s", bifrostErr.Error.Message)
		}
		return nil, fmt.Errorf("bifrost CompleteJSON: unknown error")
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("bifrost CompleteJSON: no choices returned")
	}
	if logMsg := formatUsage(resp.Usage); logMsg != "" {
		log.Debugf("%s", logMsg)
	}

	msg := resp.Choices[0].Message
	if msg == nil || msg.ChatAssistantMessage == nil || len(msg.ToolCalls) == 0 {
		return nil, fmt.Errorf("bifrost CompleteJSON: model did not make a tool call; schema=%q", schema.Name)
	}
	tc := msg.ToolCalls[0]
	if tc.Function.Name == nil || *tc.Function.Name != respondToolName {
		got := ""
		if tc.Function.Name != nil {
			got = *tc.Function.Name
		}
		return nil, fmt.Errorf("bifrost CompleteJSON: expected tool %q, got %q", respondToolName, got)
	}
	raw := json.RawMessage(tc.Function.Arguments)
	if err := schema.ValidateResponse(raw); err != nil {
		return nil, fmt.Errorf("bifrost CompleteJSON: %w", err)
	}
	return raw, nil
}

// Complete sends a user prompt and returns the first completion text.
// On the Anthropic provider, the user prompt is promoted to a content block
// carrying ephemeral cache_control so any retry or deterministic re-send
// within the 5m TTL reads from cache. Production caller is the drift page
// classifier (drift.go:459); see design doc's Cost Analysis Per Call Site.
func (c *BifrostClient) Complete(ctx context.Context, prompt string) (string, error) {
	var content *schemas.ChatMessageContent
	if c.provider == schemas.Anthropic {
		content = anthropicCachedContent(prompt)
	} else {
		content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(prompt)}
	}
	bifrostCtx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
	resp, bifrostErr := c.client.ChatCompletionRequest(bifrostCtx, &schemas.BifrostChatRequest{
		Provider: c.provider,
		Model:    c.model,
		Input: []schemas.ChatMessage{
			{
				Role:    schemas.ChatMessageRoleUser,
				Content: content,
			},
		},
		Params: &schemas.ChatParameters{
			MaxCompletionTokens: schemas.Ptr(c.maxCompletionTokens()),
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
	if msg := formatUsage(resp.Usage); msg != "" {
		log.Debugf("%s", msg)
	}
	respContent := resp.Choices[0].Message.Content
	if respContent == nil || respContent.ContentStr == nil {
		return "", fmt.Errorf("bifrost completion: nil content")
	}
	return *respContent.ContentStr, nil
}
