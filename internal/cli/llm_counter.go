package cli

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/charmbracelet/log"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// logLLMCallCounts emits a per-tier LLM call summary at debug level. Because
// root.go gates DebugLevel on --verbose, the line only appears when the user
// passed -v / --verbose. Safe to call after run completion; reads counters
// once via atomic Load.
func logLLMCallCounts(t *llmTiering) {
	c := t.CallCounts()
	log.Debugf("LLM call counts: small=%d typical=%d large=%d (total=%d)",
		c.Small, c.Typical, c.Large, c.Total())
}

// countingClient wraps an analyzer.LLMClient and increments counter on every
// Complete or CompleteJSON call. The counter is shared with the owning tier so
// reads after the run see all writes.
type countingClient struct {
	inner   analyzer.LLMClient
	counter *atomic.Int64
}

func (c *countingClient) Complete(ctx context.Context, prompt string) (string, error) {
	c.counter.Add(1)
	return c.inner.Complete(ctx, prompt)
}

func (c *countingClient) CompleteJSON(ctx context.Context, prompt string, schema analyzer.JSONSchema) (json.RawMessage, error) {
	c.counter.Add(1)
	return c.inner.CompleteJSON(ctx, prompt, schema)
}

// countingToolClient extends countingClient with CompleteWithTools so a
// ToolLLMClient stays a ToolLLMClient after wrapping. drift.go uses a runtime
// type assertion (tiering.Typical().(ToolLLMClient)) — preserving the
// interface keeps that contract intact.
type countingToolClient struct {
	*countingClient
	tool analyzer.ToolLLMClient
}

func (c *countingToolClient) CompleteWithTools(ctx context.Context, messages []analyzer.ChatMessage, tools []analyzer.Tool, opts ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	c.counter.Add(1)
	return c.tool.CompleteWithTools(ctx, messages, tools, opts...)
}

// wrapWithCounter returns inner wrapped so every LLM call increments counter.
// If inner is a ToolLLMClient, the returned value is also a ToolLLMClient.
func wrapWithCounter(inner analyzer.LLMClient, counter *atomic.Int64) analyzer.LLMClient {
	base := &countingClient{inner: inner, counter: counter}
	if tc, ok := inner.(analyzer.ToolLLMClient); ok {
		return &countingToolClient{countingClient: base, tool: tc}
	}
	return base
}
