package analyzer

import "context"

// LLMClient sends a prompt and returns the completion text.
// The real implementation wraps the Bifrost SDK; unit tests use a fake.
type LLMClient interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// ToolLLMClient extends LLMClient with a multi-turn tool-use conversation.
// The caller sends messages and tool definitions; the LLM may request tool
// calls; the caller executes them and continues the conversation until the
// LLM returns a final non-tool response.
type ToolLLMClient interface {
	LLMClient
	CompleteWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (ChatMessage, error)
}
