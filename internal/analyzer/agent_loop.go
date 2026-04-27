package analyzer

import (
	"context"
	"errors"
	"fmt"
)

// AgentResult is the outcome of a multi-turn tool-use conversation.
type AgentResult struct {
	FinalMessage ChatMessage
	Rounds       int
}

// ErrMaxRounds is returned by the agent loop when the maximum number of rounds
// is reached without the LLM producing a final text response. Callers that
// accumulate state during the loop (e.g. via tool handlers) should check for
// it via errors.Is and recover whatever partial state they have.
var ErrMaxRounds = errors.New("agent loop exceeded max rounds")

// AgentOption configures the agent loop. Currently only WithMaxRounds; future
// options (per-round callbacks, etc.) should preserve the variadic shape.
type AgentOption func(*agentConfig)

type agentConfig struct {
	maxRounds int
}

// WithMaxRounds sets the maximum number of LLM round-trips the agent loop will
// perform before giving up. n must be >= 1.
func WithMaxRounds(n int) AgentOption {
	return func(cfg *agentConfig) { cfg.maxRounds = n }
}

// turnFunc returns the LLM's next message for one round. It is the single-turn
// boundary that runAgentLoop drives in a loop. ToolLLMClient implementations
// wrap a single LLM call as a turnFunc and pass it to runAgentLoop.
type turnFunc func(ctx context.Context, messages []ChatMessage, tools []Tool) (ChatMessage, error)

const defaultAgentMaxRounds = 30

// runAgentLoop drives the multi-turn tool-use conversation behind
// ToolLLMClient.CompleteWithTools. It repeatedly calls next, dispatches any
// tool calls in the response to handlers attached to each Tool.Execute, and
// feeds the results back as tool-role messages. It terminates when the LLM
// produces a response with no tool calls (returning nil) or when max rounds is
// reached (returning ErrMaxRounds with the rounds completed in the
// AgentResult).
//
// Tool handlers that return a non-nil error abort the loop. Errors that should
// be visible TO the LLM (e.g. "file not found") must be returned as the result
// string instead. Tool calls referencing a name with no registered handler are
// fed back as `unknown tool: "X"` and the loop continues.
//
// The loop primitive is unexported; production callers go through
// ToolLLMClient.CompleteWithTools. A test-only export (see
// agent_loop_export_test.go) makes it reachable from package analyzer_test
// stubs that need to share dispatch semantics without reimplementing them.
func runAgentLoop(ctx context.Context, next turnFunc, messages []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
	cfg := agentConfig{maxRounds: defaultAgentMaxRounds}
	for _, opt := range opts {
		opt(&cfg)
	}

	handlers := make(map[string]ToolHandler, len(tools))
	for _, tool := range tools {
		handlers[tool.Name] = tool.Execute
	}

	var lastAssistant ChatMessage
	for round := 1; round <= cfg.maxRounds; round++ {
		resp, err := next(ctx, messages, tools)
		if err != nil {
			return AgentResult{}, err
		}
		messages = append(messages, resp)
		rotateCacheBreakpoint(messages)
		lastAssistant = resp

		if len(resp.ToolCalls) == 0 {
			return AgentResult{FinalMessage: resp, Rounds: round}, nil
		}

		for _, tc := range resp.ToolCalls {
			var result string
			handler, ok := handlers[tc.Name]
			if !ok || handler == nil {
				result = fmt.Sprintf("unknown tool: %q", tc.Name)
			} else {
				r, herr := handler(ctx, tc.Arguments)
				if herr != nil {
					return AgentResult{}, herr
				}
				result = r
			}
			messages = append(messages, ChatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
		rotateCacheBreakpoint(messages)
	}
	return AgentResult{FinalMessage: lastAssistant, Rounds: cfg.maxRounds}, ErrMaxRounds
}

// rotateCacheBreakpoint clears the rotating breakpoint on every message
// EXCEPT the seeded one (index 0 if it was originally flagged), then sets
// the rotating breakpoint on messages[len(messages)-1]. The seeded flag
// at index 0 is preserved as a durable breakpoint.
func rotateCacheBreakpoint(messages []ChatMessage) {
	seededFlag := len(messages) > 0 && messages[0].CacheBreakpoint
	for i := range messages {
		messages[i].CacheBreakpoint = false
	}
	if seededFlag {
		messages[0].CacheBreakpoint = true
	}
	if len(messages) > 0 {
		messages[len(messages)-1].CacheBreakpoint = true
	}
}
