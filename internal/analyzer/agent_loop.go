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
	onTurn    func()
	// preTurnHook fires immediately before each next() call. Returning a
	// non-nil error terminates the loop with that error and the rounds
	// completed so far. budgetedClient uses this to translate
	// "next turn would exceed input budget" into ErrTokenBudgetExceeded.
	preTurnHook func(messages []ChatMessage, tools []Tool) error
	// maxToolResultTokens caps the cl100k_base token count of a single
	// tool result before it's appended to the message history. Results
	// over the cap are truncated with a "[truncated: ~N tokens omitted]"
	// marker. Zero (the default) disables clipping.
	maxToolResultTokens int
}

// WithMaxRounds sets the maximum number of LLM round-trips the agent loop will
// perform before giving up. n must be >= 1.
func WithMaxRounds(n int) AgentOption {
	return func(cfg *agentConfig) { cfg.maxRounds = n }
}

// WithTurnCallback registers fn to be called once per successful turn (i.e.
// once per real LLM round-trip), immediately after the turnFunc returns
// without error. Used by callers that need to count actual LLM calls rather
// than outer CompleteWithTools invocations. fn must be safe to invoke from
// the goroutine driving the loop; pass nil to opt out.
func WithTurnCallback(fn func()) AgentOption {
	return func(cfg *agentConfig) { cfg.onTurn = fn }
}

// WithPreTurnHook registers a function called immediately before each LLM
// turn. If the hook returns a non-nil error, runAgentLoop terminates and
// returns that error along with the rounds completed so far. budgetedClient
// uses this to gate per-turn input size before the request goes out.
func WithPreTurnHook(fn func(messages []ChatMessage, tools []Tool) error) AgentOption {
	return func(cfg *agentConfig) { cfg.preTurnHook = fn }
}

// WithMaxToolResultTokens caps the cl100k_base token count of any single
// tool result before it is appended to the message history. Oversized
// results are truncated and a "[truncated: ~N tokens omitted]" marker is
// appended so the LLM can choose to call again with a narrower argument.
// n <= 0 disables clipping (the default).
//
// The cap is preventative: it stops one giant file/page read from
// single-handedly busting the next turn's input budget. budgetedClient
// sets this to roughly 0.5 × the gated budget when the model has a
// non-zero MaxInputTokens.
func WithMaxToolResultTokens(n int) AgentOption {
	return func(cfg *agentConfig) { cfg.maxToolResultTokens = n }
}

// OnTurnFromOptionsForTesting applies opts to a fresh agentConfig and returns
// the registered per-turn callback (or nil). Cross-package tests use it to
// drive AgentOption-based hooks without invoking runAgentLoop end-to-end.
// Not intended for production code.
func OnTurnFromOptionsForTesting(opts ...AgentOption) func() {
	cfg := agentConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg.onTurn
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
		// Pre-turn hook: budgetedClient uses this to translate "next turn
		// would exceed input budget" into ErrTokenBudgetExceeded. Returning
		// here preserves any partial state captured by tool handlers in
		// earlier rounds — same shape as the ErrMaxRounds termination path.
		if cfg.preTurnHook != nil {
			if err := cfg.preTurnHook(messages, tools); err != nil {
				return AgentResult{FinalMessage: lastAssistant, Rounds: round - 1}, err
			}
		}
		resp, err := next(ctx, messages, tools)
		if err != nil {
			return AgentResult{}, err
		}
		if cfg.onTurn != nil {
			cfg.onTurn()
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
			if cfg.maxToolResultTokens > 0 {
				result = clipToolResult(result, cfg.maxToolResultTokens)
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

// clipToolResult returns result truncated so its tiktoken count does not
// exceed max, with a "[truncated: ~N tokens omitted from this tool result]"
// marker appended. Returns result unchanged when it already fits.
//
// The marker is plain text the LLM sees, so it can decide to call the tool
// again with a narrower argument or simply move on. cl100k_base averages
// ~4 chars per token on English text; the byte-based first cut is
// followed by a re-count + halving fallback to handle dense bytes (code,
// minified JSON, base64) where the average is much lower.
func clipToolResult(result string, max int) string {
	n := countTokens(result)
	if n <= max {
		return result
	}
	cut := max * 4
	if cut > len(result) {
		cut = len(result)
	}
	trimmed := result[:cut]
	for countTokens(trimmed) > max && len(trimmed) > 1 {
		half := len(trimmed) / 2
		if half == 0 {
			break
		}
		trimmed = trimmed[:half]
	}
	omitted := n - countTokens(trimmed)
	return trimmed + fmt.Sprintf("\n\n[truncated: ~%d tokens omitted from this tool result]", omitted)
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
