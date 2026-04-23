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
	_, err := NewBifrostClientWithProvider("grok", "fake-key", "some-model")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestNewBifrostClientWithProvider_Anthropic_ReturnsClient(t *testing.T) {
	client, err := NewBifrostClientWithProvider("anthropic", "fake-key", "claude-3-5-sonnet-20241022")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewBifrostClientWithProvider_OpenAI_ReturnsClient(t *testing.T) {
	client, err := NewBifrostClientWithProvider("openai", "fake-key", "gpt-4o-mini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestBifrostClient_ImplementsLLMClient(t *testing.T) {
	client, err := NewBifrostClientWithProvider("anthropic", "fake-key", "claude-3-5-sonnet-20241022")
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
