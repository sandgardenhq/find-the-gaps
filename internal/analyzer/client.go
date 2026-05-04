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
	// MaxCompletionTokens is the per-model output cap. Some providers (notably
	// Groq's llama-4-scout) reject requests whose max_completion_tokens exceed
	// the model's specific limit. Zero means "use the BifrostClient default".
	MaxCompletionTokens int
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

	// CompleteJSONMultimodal is the multimodal sibling of CompleteJSON. The
	// caller supplies pre-built ChatMessages (typically with ContentBlocks
	// carrying image URLs) instead of a flat prompt string. Schema-forcing and
	// response parsing are identical to CompleteJSON; only the message body
	// differs. Vision-capable models can attend to attached images; non-vision
	// models will see only the text content blocks (provider-dependent).
	CompleteJSONMultimodal(ctx context.Context, messages []ChatMessage, schema JSONSchema) (json.RawMessage, error)

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
