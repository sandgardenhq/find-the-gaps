package analyzer

import "context"

// LLMClient sends a prompt and returns the completion text.
// The real implementation wraps the Bifrost SDK; unit tests use a fake.
type LLMClient interface {
	Complete(ctx context.Context, prompt string) (string, error)
}
