package analyzer

import (
	"context"
	"encoding/json"
)

// ModelCapabilities mirrors cli.ModelCapabilities so analyzer code can branch
// on capabilities without importing cli (which would create a dependency
// cycle). Field set is identical.
type ModelCapabilities struct {
	Provider string
	Model    string
	ToolUse  bool
	Vision   bool
}

// LLMClient sends a prompt and returns the completion text.
// The real implementation wraps the Bifrost SDK; unit tests use a fake.
type LLMClient interface {
	Complete(ctx context.Context, prompt string) (string, error)

	// CompleteJSON sends prompt and requests a response that conforms to the
	// given JSON schema. Returns the raw JSON bytes. Implementations dispatch
	// to provider-native structured-output features (Anthropic forced tool use,
	// OpenAI response_format=json_schema, Ollama format, LM Studio response_format).
	CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error)

	// Capabilities returns the model's resolved capability flags. Callers branch
	// on Capabilities().Vision and Capabilities().ToolUse to enable optional
	// pipeline features without naming providers directly.
	Capabilities() ModelCapabilities
}

// ToolLLMClient extends LLMClient with a multi-turn tool-use conversation.
// CompleteWithTools runs an agent loop: it sends messages and tool definitions
// to the LLM, dispatches any tool calls the LLM requests through Tool.Execute,
// feeds the results back, and repeats until the LLM returns a plain-text
// response (no tool calls) or the round limit is reached. Callers configure
// the loop via AgentOption values (e.g. WithMaxRounds).
type ToolLLMClient interface {
	LLMClient
	CompleteWithTools(ctx context.Context, messages []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error)
}
