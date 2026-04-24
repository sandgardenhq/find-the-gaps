package analyzer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// White-box tests for bifrost_client.go.
// These access unexported types to exercise branches that the integration tests cover via real API calls.

// fakeBifrostRequester is a test double for bifrostRequester.
type fakeBifrostRequester struct {
	resp        *schemas.BifrostChatResponse
	bifroErr    *schemas.BifrostError
	lastRequest *schemas.BifrostChatRequest
}

func (f *fakeBifrostRequester) ChatCompletionRequest(_ *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	f.lastRequest = req
	return f.resp, f.bifroErr
}

func newBifrostClientWithFake(fake bifrostRequester, provider schemas.ModelProvider, model string) *BifrostClient {
	return &BifrostClient{client: fake, provider: provider, model: model}
}

func TestBifrostAccount_GetConfigForProvider_TimeoutIs5Minutes(t *testing.T) {
	acc := &bifrostAccount{provider: schemas.Anthropic, apiKey: "test-key"}
	cfg, err := acc.GetConfigForProvider(schemas.Anthropic)
	if err != nil {
		t.Fatal(err)
	}
	const want = 300 // 5 minutes
	if cfg.NetworkConfig.DefaultRequestTimeoutInSeconds != want {
		t.Errorf("timeout = %d seconds, want %d", cfg.NetworkConfig.DefaultRequestTimeoutInSeconds, want)
	}
}

func TestBifrostAccount_GetConfiguredProviders_ReturnsProvider(t *testing.T) {
	acc := &bifrostAccount{provider: schemas.Anthropic, apiKey: "test-key"}
	providers, err := acc.GetConfiguredProviders()
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0] != schemas.Anthropic {
		t.Fatalf("expected [Anthropic], got %v", providers)
	}
}

func TestBifrostAccount_GetKeysForProvider_MatchingProvider_ReturnsKey(t *testing.T) {
	acc := &bifrostAccount{provider: schemas.Anthropic, apiKey: "test-key"}
	keys, err := acc.GetKeysForProvider(context.Background(), schemas.Anthropic)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
}

func TestBifrostAccount_GetKeysForProvider_WrongProvider_ReturnsError(t *testing.T) {
	acc := &bifrostAccount{provider: schemas.Anthropic, apiKey: "test-key"}
	_, err := acc.GetKeysForProvider(context.Background(), schemas.OpenAI)
	if err == nil {
		t.Fatal("expected error for wrong provider")
	}
}

func TestBifrostAccount_GetConfigForProvider_MatchingProvider_ReturnsConfig(t *testing.T) {
	acc := &bifrostAccount{provider: schemas.Anthropic, apiKey: "test-key"}
	cfg, err := acc.GetConfigForProvider(schemas.Anthropic)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestBifrostAccount_GetConfigForProvider_WrongProvider_ReturnsError(t *testing.T) {
	acc := &bifrostAccount{provider: schemas.Anthropic, apiKey: "test-key"}
	_, err := acc.GetConfigForProvider(schemas.OpenAI)
	if err == nil {
		t.Fatal("expected error for wrong provider")
	}
}

func TestNewBifrostClientWithProvider_UnsupportedProvider_ReturnsError(t *testing.T) {
	_, err := NewBifrostClientWithProvider("grok", "fake-key", "some-model", "")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestNewBifrostClientWithProvider_Anthropic_ReturnsClient(t *testing.T) {
	client, err := NewBifrostClientWithProvider("anthropic", "fake-key", "claude-3-5-sonnet-20241022", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewBifrostClientWithProvider_OpenAI_ReturnsClient(t *testing.T) {
	client, err := NewBifrostClientWithProvider("openai", "fake-key", "gpt-4o-mini", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewBifrostClientWithProvider_Ollama_ReturnsClient(t *testing.T) {
	client, err := NewBifrostClientWithProvider("ollama", "", "llama3.1", "http://localhost:11434")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.provider != schemas.Ollama {
		t.Fatalf("expected provider schemas.Ollama, got %q", client.provider)
	}
}

func TestNewBifrostClientWithProvider_OpenAI_WithBaseURL_ReturnsClient(t *testing.T) {
	// "lmstudio" collapses to schemas.OpenAI at the CLI layer, so the analyzer
	// must accept a non-empty base URL paired with provider "openai".
	client, err := NewBifrostClientWithProvider("openai", "", "local-model", "http://localhost:1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.provider != schemas.OpenAI {
		t.Fatalf("expected provider schemas.OpenAI, got %q", client.provider)
	}
}

func TestBifrostAccount_Ollama_GetKeysReturnsOllamaKeyConfig(t *testing.T) {
	// Bifrost's Ollama provider targets its server URL via Key.OllamaKeyConfig.URL
	// (see /maximhq/bifrost/core@v1.5.2/providers/ollama/ollama.go), NOT via
	// NetworkConfig.BaseURL. The account must plumb baseURL into the per-key config.
	const baseURL = "http://ollama.local:11434"
	acc := &bifrostAccount{provider: schemas.Ollama, apiKey: "", baseURL: baseURL}
	keys, err := acc.GetKeysForProvider(context.Background(), schemas.Ollama)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].OllamaKeyConfig == nil {
		t.Fatal("expected OllamaKeyConfig to be set for Ollama provider")
	}
	if got := keys[0].OllamaKeyConfig.URL.GetValue(); got != baseURL {
		t.Errorf("OllamaKeyConfig.URL = %q, want %q", got, baseURL)
	}
}

func TestBifrostAccount_OpenAI_WithBaseURL_SetsNetworkConfigBaseURL(t *testing.T) {
	// For OpenAI-compatible custom endpoints (LM Studio), Bifrost honors
	// NetworkConfig.BaseURL — see schemas/provider.go:54.
	const baseURL = "http://lmstudio.local:1234"
	acc := &bifrostAccount{provider: schemas.OpenAI, apiKey: "", baseURL: baseURL}
	cfg, err := acc.GetConfigForProvider(schemas.OpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkConfig.BaseURL != baseURL {
		t.Errorf("NetworkConfig.BaseURL = %q, want %q", cfg.NetworkConfig.BaseURL, baseURL)
	}
}

func TestBifrostAccount_EmptyBaseURL_LeavesNetworkConfigBaseURLEmpty(t *testing.T) {
	// Regression: hosted providers (anthropic, openai with default endpoint) must
	// not have a stray BaseURL that would redirect traffic away from the real API.
	acc := &bifrostAccount{provider: schemas.Anthropic, apiKey: "test-key", baseURL: ""}
	cfg, err := acc.GetConfigForProvider(schemas.Anthropic)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkConfig.BaseURL != "" {
		t.Errorf("NetworkConfig.BaseURL = %q, want empty", cfg.NetworkConfig.BaseURL)
	}
}

func TestBifrostAccount_OpenAI_EmptyKeyWithBaseURL_UsesPlaceholder(t *testing.T) {
	// Bifrost's per-request key filter (selectKeyFromProviderForModel + utils.go
	// CanProviderKeyValueBeEmpty) drops keys whose Value is empty unless the
	// provider is in {Vertex, Bedrock, VLLM, Azure, Ollama, SGL}. OpenAI is not
	// in that set, so a literally-empty key is filtered out and every chat
	// request fails with "no keys found that support model: <model>". For local
	// OpenAI-compatible servers (LM Studio) we substitute a non-empty placeholder
	// so the filter passes; the local server ignores the bogus bearer.
	acc := &bifrostAccount{provider: schemas.OpenAI, apiKey: "", baseURL: "http://localhost:1234"}
	keys, err := acc.GetKeysForProvider(context.Background(), schemas.OpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Value.GetValue() == "" {
		t.Error("expected non-empty placeholder key value to bypass Bifrost's empty-key filter")
	}
}

func TestBifrostAccount_OpenAI_EmptyKeyNoBaseURL_LeavesValueEmpty(t *testing.T) {
	// Hosted OpenAI without an API key is a user error — surface it via Bifrost's
	// standard "no keys found" path rather than masking it with a placeholder.
	acc := &bifrostAccount{provider: schemas.OpenAI, apiKey: "", baseURL: ""}
	keys, err := acc.GetKeysForProvider(context.Background(), schemas.OpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys[0].Value.GetValue(); got != "" {
		t.Errorf("expected empty key value for hosted OpenAI without API key, got %q", got)
	}
}

func TestBifrostAccount_OpenAI_NonEmptyKey_PreservesKeyValue(t *testing.T) {
	// The placeholder path must not clobber a user-supplied API key.
	const key = "sk-real-key"
	acc := &bifrostAccount{provider: schemas.OpenAI, apiKey: key, baseURL: "http://proxy.local"}
	keys, err := acc.GetKeysForProvider(context.Background(), schemas.OpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys[0].Value.GetValue(); got != key {
		t.Errorf("key value = %q, want %q", got, key)
	}
}

func TestBifrostClient_ImplementsLLMClient(t *testing.T) {
	client, err := NewBifrostClientWithProvider("anthropic", "fake-key", "claude-3-5-sonnet-20241022", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var _ LLMClient = client
	var _ ToolLLMClient = client
}

func makeChoice(content *schemas.ChatMessageContent) schemas.BifrostResponseChoice {
	return schemas.BifrostResponseChoice{
		ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
			Message: &schemas.ChatMessage{Content: content},
		},
	}
}

func TestBifrostClient_Complete_ReturnsContent(t *testing.T) {
	text := "pong"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &text}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-3-5-sonnet-20241022")
	got, err := client.Complete(context.Background(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	if got != "pong" {
		t.Errorf("expected pong, got %q", got)
	}
}

func TestBifrostClient_Complete_BifrostError_WithMessage(t *testing.T) {
	fake := &fakeBifrostRequester{
		bifroErr: &schemas.BifrostError{
			Error: &schemas.ErrorField{Message: "rate limited"},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-3-5-sonnet-20241022")
	_, err := client.Complete(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBifrostClient_Complete_BifrostError_NoErrorField(t *testing.T) {
	fake := &fakeBifrostRequester{
		bifroErr: &schemas.BifrostError{Error: nil},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-3-5-sonnet-20241022")
	_, err := client.Complete(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error for nil ErrorField")
	}
}

func TestBifrostClient_Complete_EmptyChoices_ReturnsError(t *testing.T) {
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{Choices: []schemas.BifrostResponseChoice{}},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-3-5-sonnet-20241022")
	_, err := client.Complete(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestBifrostClient_Complete_NilContent_ReturnsError(t *testing.T) {
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{makeChoice(nil)},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-3-5-sonnet-20241022")
	_, err := client.Complete(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error for nil Content")
	}
}

func TestBifrostClient_Complete_SetsMaxCompletionTokens(t *testing.T) {
	// Bifrost's Anthropic provider defaults max_tokens to 4096 for any model not in its
	// static fallback map (which omits newer Claude versions). Large JSON responses get
	// truncated — producing "unexpected end of JSON input" downstream. Complete must
	// explicitly set Params.MaxCompletionTokens so responses have room to finish.
	text := "ok"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &text}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	_, err := client.Complete(context.Background(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastRequest == nil {
		t.Fatal("expected request to be captured")
	}
	if fake.lastRequest.Params == nil {
		t.Fatal("expected Params to be set on request")
	}
	if fake.lastRequest.Params.MaxCompletionTokens == nil {
		t.Fatal("expected Params.MaxCompletionTokens to be set (bifrost defaults to 4096 otherwise)")
	}
	if *fake.lastRequest.Params.MaxCompletionTokens < 16_000 {
		t.Errorf("MaxCompletionTokens = %d, want >= 16000 so mapper responses don't truncate",
			*fake.lastRequest.Params.MaxCompletionTokens)
	}
}

func TestBifrostClient_CompleteWithTools_SetsMaxCompletionTokens(t *testing.T) {
	// Same reasoning as Complete: must prevent bifrost's 4096 default from truncating
	// tool-driven responses (drift agent can produce long final messages).
	text := "done"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(&schemas.ChatMessageContent{ContentStr: &text}, nil),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	_, err := client.CompleteWithTools(context.Background(),
		[]ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastRequest == nil || fake.lastRequest.Params == nil {
		t.Fatal("expected Params to be set on request")
	}
	if fake.lastRequest.Params.MaxCompletionTokens == nil {
		t.Fatal("expected Params.MaxCompletionTokens to be set")
	}
	if *fake.lastRequest.Params.MaxCompletionTokens < 16_000 {
		t.Errorf("MaxCompletionTokens = %d, want >= 16000",
			*fake.lastRequest.Params.MaxCompletionTokens)
	}
}

func TestBifrostClient_Complete_NilContentStr_ReturnsError(t *testing.T) {
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: nil}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-3-5-sonnet-20241022")
	_, err := client.Complete(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error for nil ContentStr")
	}
}

func makeToolChoice(content *schemas.ChatMessageContent, toolCalls []schemas.ChatAssistantMessageToolCall) schemas.BifrostResponseChoice {
	msg := &schemas.ChatMessage{Content: content}
	if len(toolCalls) > 0 {
		msg.ChatAssistantMessage = &schemas.ChatAssistantMessage{ToolCalls: toolCalls}
	}
	return schemas.BifrostResponseChoice{
		ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
			Message: msg,
		},
	}
}

func TestBifrostClient_CompleteWithTools_ReturnsFinalContent(t *testing.T) {
	// Simulate LLM returning a non-tool final answer directly.
	text := `[{"page":"https://x.com","issue":"Missing param."}]`
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(&schemas.ChatMessageContent{ContentStr: &text}, nil),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
	msgs := []ChatMessage{{Role: "user", Content: "check this"}}
	tools := []Tool{{Name: "read_file", Description: "reads a file", Parameters: map[string]any{"type": "object"}}}
	got, err := client.CompleteWithTools(context.Background(), msgs, tools)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", got.Role)
	}
	if !strings.Contains(got.Content, "Missing param") {
		t.Errorf("expected content to contain 'Missing param', got %q", got.Content)
	}
}

func TestBifrostClient_CompleteWithTools_ReturnsToolCalls(t *testing.T) {
	// Simulate LLM requesting a tool call.
	id := "call_1"
	name := "read_file"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(nil, []schemas.ChatAssistantMessageToolCall{
					{
						ID: &id,
						Function: schemas.ChatAssistantMessageToolCallFunction{
							Name:      &name,
							Arguments: `{"path":"main.go"}`,
						},
					},
				}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
	msgs := []ChatMessage{{Role: "user", Content: "check this"}}
	tools := []Tool{{Name: "read_file", Description: "reads a file", Parameters: map[string]any{"type": "object"}}}
	got, err := client.CompleteWithTools(context.Background(), msgs, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got %q", got.ToolCalls[0].Name)
	}
	if got.ToolCalls[0].ID != "call_1" {
		t.Errorf("expected tool call ID 'call_1', got %q", got.ToolCalls[0].ID)
	}
}

func TestBifrostClient_CompleteWithTools_BifrostError_WithMessage(t *testing.T) {
	fake := &fakeBifrostRequester{
		bifroErr: &schemas.BifrostError{
			Error: &schemas.ErrorField{Message: "rate limited"},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
	msgs := []ChatMessage{{Role: "user", Content: "check"}}
	_, err := client.CompleteWithTools(context.Background(), msgs, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected error to contain 'rate limited', got %q", err.Error())
	}
}

func TestBifrostClient_CompleteWithTools_BifrostError_NoErrorField(t *testing.T) {
	fake := &fakeBifrostRequester{
		bifroErr: &schemas.BifrostError{Error: nil},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
	msgs := []ChatMessage{{Role: "user", Content: "check"}}
	_, err := client.CompleteWithTools(context.Background(), msgs, nil)
	if err == nil {
		t.Fatal("expected error for nil ErrorField")
	}
}

func TestBifrostClient_CompleteWithTools_EmptyChoices_ReturnsError(t *testing.T) {
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{Choices: []schemas.BifrostResponseChoice{}},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
	msgs := []ChatMessage{{Role: "user", Content: "check"}}
	_, err := client.CompleteWithTools(context.Background(), msgs, nil)
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestBifrostClient_CompleteWithTools_MultiTurnMessages(t *testing.T) {
	// Exercise assistant+tool_calls and tool-role branches in message conversion.
	text := "done"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(&schemas.ChatMessageContent{ContentStr: &text}, nil),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
	msgs := []ChatMessage{
		{Role: "user", Content: "check this"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
		{Role: "tool", Content: "file contents", ToolCallID: "c1"},
	}
	tools := []Tool{{Name: "read_file", Description: "reads a file", Parameters: map[string]any{"type": "object"}}}
	got, err := client.CompleteWithTools(context.Background(), msgs, tools)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "done" {
		t.Errorf("expected content 'done', got %q", got.Content)
	}
}

func TestBifrostClient_CompleteWithTools_ToolCallNilIDAndName(t *testing.T) {
	// Simulate a tool call response where ID and Name are nil pointers.
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(nil, []schemas.ChatAssistantMessageToolCall{
					{
						ID: nil,
						Function: schemas.ChatAssistantMessageToolCallFunction{
							Name:      nil,
							Arguments: `{}`,
						},
					},
				}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
	msgs := []ChatMessage{{Role: "user", Content: "check"}}
	got, err := client.CompleteWithTools(context.Background(), msgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].ID != "" {
		t.Errorf("expected empty ID, got %q", got.ToolCalls[0].ID)
	}
	if got.ToolCalls[0].Name != "" {
		t.Errorf("expected empty name, got %q", got.ToolCalls[0].Name)
	}
}

// --- CompleteJSON tests (Anthropic forced tool use) ---

const testAnalyzeSchemaDoc = `{"type":"object","properties":{"summary":{"type":"string"},"features":{"type":"array","items":{"type":"string"}}},"required":["summary","features"]}`

func testAnalyzeSchema() JSONSchema {
	return JSONSchema{Name: "analyze_response", Doc: json.RawMessage(testAnalyzeSchemaDoc)}
}

func TestBifrostClient_CompleteJSON_Anthropic_ForcesRespondTool(t *testing.T) {
	// When Anthropic is the provider, CompleteJSON must (a) register a single tool
	// named "respond" whose parameters equal schema.Doc, and (b) set tool_choice to
	// force that tool. The model's response is then the tool-call arguments — a
	// parse-guaranteed JSON object, no free-text prose to recover from.
	respondID := "tc_1"
	respondName := "respond"
	expected := `{"summary":"ok","features":["a","b"]}`
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(nil, []schemas.ChatAssistantMessageToolCall{
					{
						ID: &respondID,
						Function: schemas.ChatAssistantMessageToolCallFunction{
							Name:      &respondName,
							Arguments: expected,
						},
					},
				}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	got, err := client.CompleteJSON(context.Background(), "summarize this", testAnalyzeSchema())
	require.NoError(t, err)
	assert.JSONEq(t, expected, string(got))

	req := fake.lastRequest
	require.NotNil(t, req, "expected request to be captured")
	require.NotNil(t, req.Params, "expected Params to be set")
	require.Len(t, req.Params.Tools, 1, "must register exactly one tool")
	require.NotNil(t, req.Params.Tools[0].Function)
	assert.Equal(t, "respond", req.Params.Tools[0].Function.Name)
	require.NotNil(t, req.Params.ToolChoice, "must force tool choice")
	require.NotNil(t, req.Params.ToolChoice.ChatToolChoiceStruct)
	assert.Equal(t, schemas.ChatToolChoiceTypeFunction, req.Params.ToolChoice.ChatToolChoiceStruct.Type)
	require.NotNil(t, req.Params.ToolChoice.ChatToolChoiceStruct.Function)
	assert.Equal(t, "respond", req.Params.ToolChoice.ChatToolChoiceStruct.Function.Name)
}

func TestBifrostClient_CompleteJSON_Anthropic_BifrostError(t *testing.T) {
	fake := &fakeBifrostRequester{
		bifroErr: &schemas.BifrostError{Error: &schemas.ErrorField{Message: "rate limited"}},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
}

func TestBifrostClient_CompleteJSON_Anthropic_EmptyChoices(t *testing.T) {
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{Choices: []schemas.BifrostResponseChoice{}},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.Error(t, err)
}

func TestBifrostClient_CompleteJSON_Anthropic_NoToolCalls(t *testing.T) {
	// Model returned free-text content instead of calling the forced tool.
	// Must surface a clear error — do NOT try to parse the content.
	text := "I am refusing to use the tool."
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(&schemas.ChatMessageContent{ContentStr: &text}, nil),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool call")
}

func TestBifrostClient_CompleteJSON_Anthropic_WrongToolName(t *testing.T) {
	// Model called a different tool than the one we forced.
	otherID := "tc_x"
	otherName := "some_other_tool"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(nil, []schemas.ChatAssistantMessageToolCall{
					{
						ID: &otherID,
						Function: schemas.ChatAssistantMessageToolCallFunction{
							Name:      &otherName,
							Arguments: `{}`,
						},
					},
				}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "respond")
}

func TestBifrostClient_CompleteJSON_Anthropic_RejectsSchemaViolations(t *testing.T) {
	// Anthropic's tool input_schema is advisory — the model can ignore it and
	// emit arguments that don't match. The client must validate against the
	// schema and return a clear error rather than handing malformed data to the
	// caller. The canary payload is missing the required "summary" field.
	respondID := "tc_1"
	respondName := "respond"
	badArgs := `{"features":["a","b"]}`
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(nil, []schemas.ChatAssistantMessageToolCall{
					{
						ID: &respondID,
						Function: schemas.ChatAssistantMessageToolCallFunction{
							Name:      &respondName,
							Arguments: badArgs,
						},
					},
				}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	schema := JSONSchema{
		Name: "analyze_response",
		Doc: json.RawMessage(`{
			"type": "object",
			"properties": {
				"summary":  {"type": "string"},
				"features": {"type": "array", "items": {"type": "string"}}
			},
			"required": ["summary", "features"]
		}`),
	}
	_, err := client.CompleteJSON(context.Background(), "x", schema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "analyze_response")
}

func TestBifrostClient_CompleteJSON_Anthropic_SetsMaxCompletionTokens(t *testing.T) {
	respondID := "tc_1"
	respondName := "respond"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeToolChoice(nil, []schemas.ChatAssistantMessageToolCall{
					{
						ID: &respondID,
						Function: schemas.ChatAssistantMessageToolCallFunction{
							Name:      &respondName,
							Arguments: `{"summary":"x","features":[]}`,
						},
					},
				}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-opus-4-7")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.NoError(t, err)
	require.NotNil(t, fake.lastRequest.Params.MaxCompletionTokens)
	assert.GreaterOrEqual(t, *fake.lastRequest.Params.MaxCompletionTokens, 16_000)
}

// --- CompleteJSON tests (OpenAI native response_format=json_schema) ---

func TestBifrostClient_CompleteJSON_OpenAI_SetsJSONSchemaResponseFormat(t *testing.T) {
	// OpenAI supports structured outputs natively via response_format:
	// {"type":"json_schema","json_schema":{"name":..., "strict":true, "schema":...}}.
	// CompleteJSON must set this on the request, NOT use Anthropic-style forced
	// tool use. The returned content is a JSON string — parse it and return.
	content := `{"summary":"ok","features":["a","b"]}`
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &content}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "gpt-4o-mini")
	got, err := client.CompleteJSON(context.Background(), "summarize this", testAnalyzeSchema())
	require.NoError(t, err)
	assert.JSONEq(t, content, string(got))

	req := fake.lastRequest
	require.NotNil(t, req, "expected request to be captured")
	require.NotNil(t, req.Params, "expected Params to be set")
	require.Empty(t, req.Params.Tools, "OpenAI path must not register tools")
	require.Nil(t, req.Params.ToolChoice, "OpenAI path must not force a tool choice")
	require.NotNil(t, req.Params.ResponseFormat, "expected response_format to be set")

	rfJSON, err := json.Marshal(*req.Params.ResponseFormat)
	require.NoError(t, err)
	var rf struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name   string          `json:"name"`
			Strict bool            `json:"strict"`
			Schema json.RawMessage `json:"schema"`
		} `json:"json_schema"`
	}
	require.NoError(t, json.Unmarshal(rfJSON, &rf))
	assert.Equal(t, "json_schema", rf.Type)
	assert.Equal(t, "analyze_response", rf.JSONSchema.Name)
	assert.True(t, rf.JSONSchema.Strict, "strict mode must be true")
	assert.JSONEq(t, testAnalyzeSchemaDoc, string(rf.JSONSchema.Schema))
}

func TestBifrostClient_CompleteJSON_Ollama_UsesOpenAIJSONSchemaPath(t *testing.T) {
	// Bifrost's Ollama provider delegates chat completions to OpenAI's handler
	// (providers/ollama/ollama.go:136 → openai.HandleOpenAIChatCompletionRequest),
	// so response_format=json_schema passes through untouched. CompleteJSON must
	// therefore route Ollama through the same structured-outputs path as OpenAI:
	// set ResponseFormat, parse msg.Content.ContentStr as JSON, validate.
	content := `{"summary":"ok","features":["a"]}`
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &content}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.Ollama, "llama3.1")
	got, err := client.CompleteJSON(context.Background(), "summarize this", testAnalyzeSchema())
	require.NoError(t, err)
	assert.JSONEq(t, content, string(got))

	req := fake.lastRequest
	require.NotNil(t, req, "expected request to be captured")
	require.NotNil(t, req.Params, "expected Params to be set")
	require.NotNil(t, req.Params.ResponseFormat, "expected response_format to be set for ollama (OpenAI-compat path)")
}

func TestBifrostClient_CompleteJSON_OpenAI_BifrostError(t *testing.T) {
	fake := &fakeBifrostRequester{
		bifroErr: &schemas.BifrostError{Error: &schemas.ErrorField{Message: "bad model"}},
	}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "gpt-4o-mini")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad model")
}

func TestBifrostClient_CompleteJSON_OpenAI_EmptyChoices(t *testing.T) {
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{Choices: []schemas.BifrostResponseChoice{}},
	}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "gpt-4o-mini")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.Error(t, err)
}

func TestBifrostClient_CompleteJSON_OpenAI_NilContent_ReturnsError(t *testing.T) {
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{makeChoice(nil)},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "gpt-4o-mini")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.Error(t, err)
}

func TestBifrostClient_CompleteJSON_OpenAI_ValidatesSchema(t *testing.T) {
	// Even with strict json_schema, defensively validate — catches mismatches
	// between our schema definition and whatever the provider honored.
	bad := `{"features":["a"]}` // missing required "summary"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &bad}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "gpt-4o-mini")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "analyze_response")
}

func TestBifrostClient_CompleteJSON_OpenAI_SetsMaxCompletionTokens(t *testing.T) {
	content := `{"summary":"x","features":[]}`
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &content}),
			},
		},
	}
	client := newBifrostClientWithFake(fake, schemas.OpenAI, "gpt-4o-mini")
	_, err := client.CompleteJSON(context.Background(), "x", testAnalyzeSchema())
	require.NoError(t, err)
	require.NotNil(t, fake.lastRequest.Params.MaxCompletionTokens)
	assert.GreaterOrEqual(t, *fake.lastRequest.Params.MaxCompletionTokens, 16_000)
}
