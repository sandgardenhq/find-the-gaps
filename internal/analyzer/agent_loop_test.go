package analyzer

import (
	"context"
	"errors"
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
