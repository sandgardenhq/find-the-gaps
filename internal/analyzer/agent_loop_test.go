package analyzer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunAgentLoop_TextResponseOnFirstTurn_EndsLoop(t *testing.T) {
	nextMessage := func(_ context.Context, _ []ChatMessage, _ []Tool) (ChatMessage, error) {
		return ChatMessage{Role: "assistant", Content: "all done"}, nil
	}
	msgs := []ChatMessage{{Role: "user", Content: "go"}}

	result, err := runAgentLoop(context.Background(), nextMessage, msgs, nil)
	require.NoError(t, err)
	assert.Equal(t, "all done", result.FinalMessage.Content)
	assert.Equal(t, 1, result.Rounds)
}

// scriptedTurns returns a turnFunc that yields the given messages in order;
// after exhaustion it reuses the last entry. It captures every received
// message slice so tests can assert on what the loop fed back.
func scriptedTurns(scripted []ChatMessage, observed *[][]ChatMessage) turnFunc {
	calls := 0
	return func(_ context.Context, msgs []ChatMessage, _ []Tool) (ChatMessage, error) {
		if observed != nil {
			snapshot := make([]ChatMessage, len(msgs))
			copy(snapshot, msgs)
			*observed = append(*observed, snapshot)
		}
		idx := calls
		if idx >= len(scripted) {
			idx = len(scripted) - 1
		}
		calls++
		return scripted[idx], nil
	}
}

func TestRunAgentLoop_ToolCall_HandlerInvoked_ResultFedBack(t *testing.T) {
	var observed [][]ChatMessage
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{"x":"hi"}`}}},
		{Role: "assistant", Content: "ok"},
	}
	var handlerArgs string
	tools := []Tool{
		{
			Name:        "echo",
			Description: "echoes",
			Execute: func(_ context.Context, args string) (string, error) {
				handlerArgs = args
				return "echoed:" + args, nil
			},
		},
	}

	result, err := runAgentLoop(context.Background(), scriptedTurns(scripted, &observed), []ChatMessage{{Role: "user", Content: "go"}}, tools)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Rounds)
	assert.Equal(t, "ok", result.FinalMessage.Content)
	assert.Equal(t, `{"x":"hi"}`, handlerArgs)

	// Round 2's input must include the assistant tool-call message and the
	// tool-role result. Two turns recorded; the second turn's messages should
	// have grown by 2 entries (assistant + tool).
	require.Len(t, observed, 2)
	assert.Len(t, observed[0], 1)
	assert.Len(t, observed[1], 3)
	assert.Equal(t, "tool", observed[1][2].Role)
	assert.Equal(t, "c1", observed[1][2].ToolCallID)
	assert.Equal(t, `echoed:{"x":"hi"}`, observed[1][2].Content)
}

func TestRunAgentLoop_UnknownTool_FedBackAsError_LoopContinues(t *testing.T) {
	var observed [][]ChatMessage
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "nonexistent", Arguments: `{}`}}},
		{Role: "assistant", Content: "ok"},
	}
	// No handler registered for "nonexistent". Loop must not panic.
	result, err := runAgentLoop(context.Background(), scriptedTurns(scripted, &observed), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Rounds)
	require.Len(t, observed, 2)
	require.Len(t, observed[1], 2, "round 2 should see the original tool-call assistant message + the unknown-tool feedback")
	feedback := observed[1][1]
	assert.Equal(t, "tool", feedback.Role)
	assert.Equal(t, "c1", feedback.ToolCallID)
	assert.Contains(t, feedback.Content, "unknown tool")
	assert.Contains(t, feedback.Content, "nonexistent")
}

func TestRunAgentLoop_HandlerError_AbortsLoop(t *testing.T) {
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "boom", Arguments: `{}`}}},
	}
	bang := errors.New("disk on fire")
	tools := []Tool{
		{
			Name: "boom",
			Execute: func(_ context.Context, _ string) (string, error) {
				return "", bang
			},
		},
	}
	_, err := runAgentLoop(context.Background(), scriptedTurns(scripted, nil), nil, tools)
	require.Error(t, err)
	assert.True(t, errors.Is(err, bang), "handler error must propagate via errors.Is")
}

func TestRunAgentLoop_TurnError_Propagated(t *testing.T) {
	bang := errors.New("bifrost down")
	next := func(_ context.Context, _ []ChatMessage, _ []Tool) (ChatMessage, error) {
		return ChatMessage{}, bang
	}
	_, err := runAgentLoop(context.Background(), next, nil, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, bang))
}

func TestRunAgentLoop_MaxRoundsExceeded_ReturnsErrMaxRounds(t *testing.T) {
	// LLM keeps calling a registered tool forever. With max-rounds=1, the loop
	// must terminate after one round and return ErrMaxRounds.
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
	}
	tools := []Tool{
		{
			Name:    "echo",
			Execute: func(_ context.Context, _ string) (string, error) { return "", nil },
		},
	}
	result, err := runAgentLoop(context.Background(), scriptedTurns(scripted, nil), nil, tools, WithMaxRounds(1))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMaxRounds), "must wrap ErrMaxRounds for errors.Is")
	assert.Equal(t, 1, result.Rounds, "result must report rounds attempted")
}

// On round 2, the previously-marked message must be unflagged and the
// newly-appended tool result must carry the marker.
func TestRunAgentLoop_RotatesCacheBreakpointToLatestMessage(t *testing.T) {
	var observed [][]ChatMessage
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
		{Role: "assistant", Content: "ok"},
	}
	tools := []Tool{{
		Name: "echo", Description: "x",
		Execute: func(_ context.Context, _ string) (string, error) { return "result", nil },
	}}
	initial := []ChatMessage{{Role: "user", Content: "go", CacheBreakpoint: true}}

	_, err := runAgentLoop(context.Background(), scriptedTurns(scripted, &observed), initial, tools)
	require.NoError(t, err)
	require.Len(t, observed, 2)

	// Round 1: only the seeded user message has the flag.
	require.Len(t, observed[0], 1)
	require.True(t, observed[0][0].CacheBreakpoint)

	// Round 2: messages = [seeded user, assistant tool_use, tool result].
	// The seeded user message must KEEP its flag (durable breakpoint #2);
	// the latest message (tool result) must carry the rotating breakpoint;
	// the assistant message in between must NOT have it.
	require.Len(t, observed[1], 3)
	require.True(t, observed[1][0].CacheBreakpoint, "investigator prompt keeps its breakpoint")
	require.False(t, observed[1][1].CacheBreakpoint, "intermediate assistant message must not be flagged")
	require.True(t, observed[1][2].CacheBreakpoint, "latest tool result must carry rotating breakpoint")
}

// When there is NO seeded breakpoint, the rotating marker still applies
// to the latest appended message starting from round 2.
func TestRunAgentLoop_RotatingBreakpoint_NoSeeded(t *testing.T) {
	var observed [][]ChatMessage
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
		{Role: "assistant", Content: "ok"},
	}
	tools := []Tool{{
		Name: "echo", Description: "x",
		Execute: func(_ context.Context, _ string) (string, error) { return "result", nil },
	}}
	initial := []ChatMessage{{Role: "user", Content: "go"}}

	_, err := runAgentLoop(context.Background(), scriptedTurns(scripted, &observed), initial, tools)
	require.NoError(t, err)
	require.Len(t, observed[1], 3)
	require.False(t, observed[1][0].CacheBreakpoint)
	require.False(t, observed[1][1].CacheBreakpoint)
	require.True(t, observed[1][2].CacheBreakpoint)
}

// WithTurnCallback fires once per successful next() call so callers can count
// real LLM round-trips. The happy path: 2 successful turns -> 2 invocations.
func TestRunAgentLoop_TurnCallback_FiresOnEverySuccessfulTurn(t *testing.T) {
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
		{Role: "assistant", Content: "ok"},
	}
	tools := []Tool{{
		Name:    "echo",
		Execute: func(_ context.Context, _ string) (string, error) { return "", nil },
	}}
	var calls int
	_, err := runAgentLoop(context.Background(), scriptedTurns(scripted, nil), nil, tools, WithTurnCallback(func() { calls++ }))
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "callback must fire once per successful turn")
}

// A failing turn must NOT invoke the callback for itself; only successful
// turns count.
func TestRunAgentLoop_TurnCallback_NotFiredOnFailingTurn(t *testing.T) {
	bang := errors.New("bifrost down")
	turn := 0
	next := func(_ context.Context, _ []ChatMessage, _ []Tool) (ChatMessage, error) {
		turn++
		if turn == 2 {
			return ChatMessage{}, bang
		}
		return ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}}, nil
	}
	tools := []Tool{{
		Name:    "echo",
		Execute: func(_ context.Context, _ string) (string, error) { return "", nil },
	}}
	var calls int
	_, err := runAgentLoop(context.Background(), next, nil, tools, WithTurnCallback(func() { calls++ }))
	require.Error(t, err)
	assert.Equal(t, 1, calls, "only the first (successful) turn should fire the callback")
}

// When ErrMaxRounds terminates the loop, the callback must have fired once
// per attempted-and-completed turn.
func TestRunAgentLoop_TurnCallback_FiresOnMaxRoundsPath(t *testing.T) {
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
	}
	tools := []Tool{{
		Name:    "echo",
		Execute: func(_ context.Context, _ string) (string, error) { return "", nil },
	}}
	var calls int
	_, err := runAgentLoop(context.Background(), scriptedTurns(scripted, nil), nil, tools, WithMaxRounds(3), WithTurnCallback(func() { calls++ }))
	require.ErrorIs(t, err, ErrMaxRounds)
	assert.Equal(t, 3, calls, "callback fires per attempted turn even when max-rounds is hit")
}

func TestRunAgentLoop_MaxRoundsAllowsCompletion(t *testing.T) {
	// max-rounds=2 with a single tool-call followed by text must succeed in 2 rounds.
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
		{Role: "assistant", Content: "ok"},
	}
	tools := []Tool{
		{Name: "echo", Execute: func(_ context.Context, _ string) (string, error) { return "", nil }},
	}
	result, err := runAgentLoop(context.Background(), scriptedTurns(scripted, nil), nil, tools, WithMaxRounds(2))
	require.NoError(t, err)
	assert.Equal(t, 2, result.Rounds)
	assert.Equal(t, "ok", result.FinalMessage.Content)
}

// TestRunAgentLoop_PreTurnHookErrorTerminates pins the contract that a
// non-nil error from the pre-turn hook ends the loop with that error. The
// budgeted client uses this to translate "next turn would exceed input
// budget" into ErrTokenBudgetExceeded; partial state captured by tool
// handlers in earlier rounds survives unchanged.
func TestRunAgentLoop_PreTurnHookErrorTerminates(t *testing.T) {
	nextCalls := 0
	next := func(_ context.Context, _ []ChatMessage, _ []Tool) (ChatMessage, error) {
		nextCalls++
		return ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "1", Name: "noop", Arguments: "{}"}}}, nil
	}
	noop := Tool{Name: "noop", Execute: func(_ context.Context, _ string) (string, error) { return "ok", nil }}

	hookCalls := 0
	hook := func(_ []ChatMessage, _ []Tool) error {
		hookCalls++
		if hookCalls == 3 {
			return ErrTokenBudgetExceeded{Where: "test", Counted: 999, Budget: 100}
		}
		return nil
	}

	res, err := runAgentLoop(context.Background(), next,
		[]ChatMessage{{Role: "user", Content: "go"}}, []Tool{noop},
		WithMaxRounds(10), WithPreTurnHook(hook))

	if !errors.Is(err, ErrTokenBudgetExceeded{}) {
		t.Fatalf("expected ErrTokenBudgetExceeded, got %v", err)
	}
	if nextCalls != 2 {
		t.Fatalf("expected next to fire 2 times before hook refused, got %d", nextCalls)
	}
	if res.Rounds != 2 {
		t.Fatalf("expected res.Rounds=2 (rounds completed before refusal), got %d", res.Rounds)
	}
}

// TestRunAgentLoop_ClipsLargeToolResults pins the per-tool-result hard
// cap. A single tool result that would alone burn most of the budget is
// truncated with a "[truncated:" marker before being appended to the
// message history. Without this, one giant file read could push the
// next turn over budget on its own.
func TestRunAgentLoop_ClipsLargeToolResults(t *testing.T) {
	// Use representative English text rather than repeated single chars —
	// cl100k_base hits a slow path on degenerate single-char repeats and
	// the test would spend most of its time tokenizing.
	huge := strings.Repeat("the quick brown fox jumps over the lazy dog ", 5000)
	noop := Tool{Name: "big", Execute: func(_ context.Context, _ string) (string, error) { return huge, nil }}

	var capturedToolMsg ChatMessage
	round := 0
	next := func(_ context.Context, msgs []ChatMessage, _ []Tool) (ChatMessage, error) {
		round++
		if round == 1 {
			return ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "1", Name: "big", Arguments: "{}"}}}, nil
		}
		// Round 2: capture the tool message that landed in the history.
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "tool" {
				capturedToolMsg = msgs[i]
				break
			}
		}
		return ChatMessage{Role: "assistant", Content: "done"}, nil
	}

	_, err := runAgentLoop(context.Background(), next,
		[]ChatMessage{{Role: "user", Content: "go"}}, []Tool{noop},
		WithMaxRounds(5), WithMaxToolResultTokens(1000))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capturedToolMsg.Content, "[truncated") {
		t.Fatalf("tool message not clipped — no '[truncated' marker found")
	}
	// Generous upper bound: 1000 tokens × ~4 chars + marker overhead.
	if len(capturedToolMsg.Content) > 20000 {
		t.Fatalf("tool message not clipped: len=%d", len(capturedToolMsg.Content))
	}
}

// TestRunAgentLoop_ZeroMaxToolResultTokensSkipsClip pins the contract that
// 0 disables clipping (used when the model has no budget — ollama/lmstudio).
func TestRunAgentLoop_ZeroMaxToolResultTokensSkipsClip(t *testing.T) {
	huge := strings.Repeat("the quick brown fox jumps over the lazy dog ", 1000)
	noop := Tool{Name: "big", Execute: func(_ context.Context, _ string) (string, error) { return huge, nil }}

	var capturedToolMsg ChatMessage
	round := 0
	next := func(_ context.Context, msgs []ChatMessage, _ []Tool) (ChatMessage, error) {
		round++
		if round == 1 {
			return ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "1", Name: "big", Arguments: "{}"}}}, nil
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "tool" {
				capturedToolMsg = msgs[i]
				break
			}
		}
		return ChatMessage{Role: "assistant", Content: "done"}, nil
	}

	_, err := runAgentLoop(context.Background(), next,
		[]ChatMessage{{Role: "user", Content: "go"}}, []Tool{noop},
		WithMaxRounds(5), WithMaxToolResultTokens(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(capturedToolMsg.Content) != len(huge) {
		t.Fatalf("expected unmodified tool result with cap=0, got len=%d (want %d)", len(capturedToolMsg.Content), len(huge))
	}
	if strings.Contains(capturedToolMsg.Content, "[truncated") {
		t.Fatalf("did not expect truncation marker when cap=0")
	}
}
