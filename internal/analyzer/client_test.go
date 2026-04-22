package analyzer_test

import (
	"context"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// stubToolClient is a minimal implementation used to verify the ToolLLMClient
// interface is satisfiable.
type stubToolClient struct{}

func (s *stubToolClient) Complete(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *stubToolClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool) (analyzer.ChatMessage, error) {
	return analyzer.ChatMessage{}, nil
}

// TestToolLLMClient_InterfaceSatisfied ensures the ToolLLMClient interface
// exists and that a concrete type can satisfy it.
func TestToolLLMClient_InterfaceSatisfied(t *testing.T) {
	var _ analyzer.ToolLLMClient = (*stubToolClient)(nil)
}

// TestToolLLMClient_EmbedssLLMClient ensures ToolLLMClient is assignable to
// LLMClient (i.e. it embeds / extends the base interface).
func TestToolLLMClient_EmbedsLLMClient(t *testing.T) {
	var client analyzer.ToolLLMClient = &stubToolClient{}
	var _ analyzer.LLMClient = client
}
