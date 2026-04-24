package analyzer

import (
	"context"
	"encoding/json"
)

// LLMClient sends a prompt and returns the completion text.
// The real implementation wraps the Bifrost SDK; unit tests use a fake.
type LLMClient interface {
	Complete(ctx context.Context, prompt string) (string, error)

	// CompleteJSON sends prompt and requests a response that conforms to the
	// given JSON schema. Returns the raw JSON bytes. Implementations dispatch
	// to provider-native structured-output features (Anthropic forced tool use,
	// OpenAI response_format=json_schema, Ollama format, LM Studio response_format).
	CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error)
}

// ToolLLMClient extends LLMClient with a multi-turn tool-use conversation.
// The caller sends messages and tool definitions; the LLM may request tool
// calls; the caller executes them and continues the conversation until the
// LLM returns a final non-tool response.
type ToolLLMClient interface {
	LLMClient
	CompleteWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (ChatMessage, error)
}
