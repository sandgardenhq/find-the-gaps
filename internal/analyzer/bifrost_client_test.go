package analyzer

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

// White-box tests for bifrost_client.go.
// These access unexported types to exercise branches that the integration tests cover via real API calls.

// fakeBifrostRequester is a test double for bifrostRequester.
type fakeBifrostRequester struct {
	resp     *schemas.BifrostChatResponse
	bifroErr *schemas.BifrostError
}

func (f *fakeBifrostRequester) ChatCompletionRequest(_ *schemas.BifrostContext, _ *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
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
