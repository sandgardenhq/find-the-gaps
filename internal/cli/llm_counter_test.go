package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// fakeLLMClient implements analyzer.LLMClient (no tool support).
type fakeLLMClient struct{}

func (fakeLLMClient) Complete(_ context.Context, _ string) (string, error) { return "", nil }
func (fakeLLMClient) CompleteJSON(_ context.Context, _ string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}
func (fakeLLMClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}
func (fakeLLMClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}

// fakeToolLLMClient implements analyzer.ToolLLMClient (full tool support).
// CompleteWithTools simulates an N-turn agent loop by extracting the turn
// callback from the supplied AgentOptions and invoking it once per simulated
// turn — same effect as runAgentLoop firing the callback per round-trip,
// without coupling this test to runAgentLoop internals.
type fakeToolLLMClient struct {
	turns int
}

func (fakeToolLLMClient) Complete(_ context.Context, _ string) (string, error) { return "", nil }
func (fakeToolLLMClient) CompleteJSON(_ context.Context, _ string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}
func (fakeToolLLMClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}
func (fakeToolLLMClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}
func (f fakeToolLLMClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool, opts ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	turnCB := analyzer.OnTurnFromOptionsForTesting(opts...)
	n := max(f.turns, 1)
	for range n {
		if turnCB != nil {
			turnCB()
		}
	}
	return analyzer.AgentResult{Rounds: n}, nil
}

func TestWrapWithCounter_IncrementsOnComplete(t *testing.T) {
	var counter atomic.Int64
	c := wrapWithCounter(fakeLLMClient{}, &counter)

	if _, err := c.Complete(context.Background(), "p"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := counter.Load(); got != 1 {
		t.Fatalf("counter after one Complete = %d, want 1", got)
	}
}

func TestWrapWithCounter_IncrementsOnCompleteJSON(t *testing.T) {
	var counter atomic.Int64
	c := wrapWithCounter(fakeLLMClient{}, &counter)

	if _, err := c.CompleteJSON(context.Background(), "p", analyzer.JSONSchema{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := counter.Load(); got != 1 {
		t.Fatalf("counter after one CompleteJSON = %d, want 1", got)
	}
}

// CompleteJSONMultimodal is a real LLM round-trip and must increment the
// counter just like CompleteJSON. Pins the multimodal accounting so the
// vision relevance pass shows up in per-tier call summaries.
func TestWrapWithCounter_IncrementsOnCompleteJSONMultimodal(t *testing.T) {
	var counter atomic.Int64
	c := wrapWithCounter(fakeLLMClient{}, &counter)

	if _, err := c.CompleteJSONMultimodal(context.Background(), nil, analyzer.JSONSchema{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := counter.Load(); got != 1 {
		t.Fatalf("counter after one CompleteJSONMultimodal = %d, want 1", got)
	}
}

// CompleteWithTools is a multi-turn agent loop. The wrapper must count one
// per actual LLM round-trip (turn), not one per outer invocation.
func TestWrapWithCounter_CompleteWithTools_CountsPerTurn(t *testing.T) {
	var counter atomic.Int64
	wrapped := wrapWithCounter(fakeToolLLMClient{turns: 5}, &counter)
	tc, ok := wrapped.(analyzer.ToolLLMClient)
	if !ok {
		t.Fatal("wrapping a ToolLLMClient must yield a ToolLLMClient")
	}
	if _, err := tc.CompleteWithTools(context.Background(), nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := counter.Load(); got != 5 {
		t.Fatalf("counter after a 5-turn agent call = %d, want 5", got)
	}
}

// A single-turn agent call (text reply on the first turn) must count as 1,
// not 0 — the LLM still made one round-trip.
func TestWrapWithCounter_CompleteWithTools_SingleTurn(t *testing.T) {
	var counter atomic.Int64
	wrapped := wrapWithCounter(fakeToolLLMClient{turns: 1}, &counter)
	tc := wrapped.(analyzer.ToolLLMClient)
	if _, err := tc.CompleteWithTools(context.Background(), nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := counter.Load(); got != 1 {
		t.Fatalf("counter after a single-turn agent call = %d, want 1", got)
	}
}

func TestWrapWithCounter_PreservesNonToolClient(t *testing.T) {
	var counter atomic.Int64
	wrapped := wrapWithCounter(fakeLLMClient{}, &counter)
	if _, ok := wrapped.(analyzer.ToolLLMClient); ok {
		t.Fatal("wrapping a non-tool client must NOT satisfy ToolLLMClient")
	}
}

func TestLLMTiering_CallCounts_TracksPerTier(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	tg, err := newLLMTiering("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := tg.CallCounts(); got.Small != 0 || got.Typical != 0 || got.Large != 0 {
		t.Fatalf("fresh tiering should have zero counts, got %+v", got)
	}

	// Bump each tier's counter directly via the wrapper. We don't call the
	// real Bifrost-backed Complete here because that would hit the network;
	// instead we exercise the increment by reaching into the tiering's
	// per-tier atomic counter the same way wrapWithCounter does.
	tg.smallCalls.Add(3)
	tg.typicalCalls.Add(2)
	tg.largeCalls.Add(1)

	got := tg.CallCounts()
	if got.Small != 3 || got.Typical != 2 || got.Large != 1 {
		t.Fatalf("CallCounts = %+v, want {Small:3 Typical:2 Large:1}", got)
	}
}

func TestLLMTiering_SmallClient_IsCounted(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	tg, err := newLLMTiering("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The client returned by Small() must be a *countingClient (or
	// *countingToolClient) so that real calls flow through the counter.
	switch tg.Small().(type) {
	case *countingClient, *countingToolClient:
		// OK
	default:
		t.Fatalf("Small() must return a counting wrapper, got %T", tg.Small())
	}
	switch tg.Typical().(type) {
	case *countingClient, *countingToolClient:
		// OK
	default:
		t.Fatalf("Typical() must return a counting wrapper, got %T", tg.Typical())
	}
	switch tg.Large().(type) {
	case *countingClient, *countingToolClient:
		// OK
	default:
		t.Fatalf("Large() must return a counting wrapper, got %T", tg.Large())
	}
}

func TestLogLLMCallCounts_DebugLevel_EmitsSummary(t *testing.T) {
	t.Cleanup(func() {
		log.SetOutput(nil)
		log.SetLevel(log.InfoLevel)
	})
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	tg, err := newLLMTiering("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tg.smallCalls.Add(7)
	tg.typicalCalls.Add(3)
	tg.largeCalls.Add(2)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.DebugLevel)

	logLLMCallCounts(tg)

	out := buf.String()
	if !strings.Contains(out, "LLM call counts") {
		t.Fatalf("expected summary line in output, got: %q", out)
	}
	for _, want := range []string{"small=7", "typical=3", "large=2", "total=12"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; full output: %q", want, out)
		}
	}
}

func TestLogLLMCallCounts_InfoLevel_Silent(t *testing.T) {
	t.Cleanup(func() {
		log.SetOutput(nil)
		log.SetLevel(log.InfoLevel)
	})
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	tg, err := newLLMTiering("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tg.smallCalls.Add(5)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.InfoLevel)

	logLLMCallCounts(tg)

	if strings.Contains(buf.String(), "LLM call counts") {
		t.Fatalf("summary must not appear at info level; got: %q", buf.String())
	}
}

// TestAnalyze_verbose_logsCallCountsOnNonSuccessPath verifies the LLM call
// summary is emitted once the tiering is built, regardless of which return
// path the run takes. With mdfetch scrubbed from PATH the crawl yields zero
// pages, which trips an early `return nil` at line ~199 of analyze.go that
// previously bypassed the summary. The summary line must still appear.
func TestAnalyze_verbose_logsCallCountsOnNonSuccessPath(t *testing.T) {
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.InfoLevel)
	})
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("PATH", t.TempDir())

	repo := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	_ = run(&stdout, &stderr, []string{
		"--verbose", "analyze",
		"--repo", repo,
		"--cache-dir", cacheBase,
		"--docs-url", "http://127.0.0.1:1/does-not-exist",
	})
	if !strings.Contains(stderr.String(), "LLM call counts") {
		t.Fatalf("expected 'LLM call counts' summary in stderr on non-success path; got: %q", stderr.String())
	}
}

func TestWrapWithCounter_ConcurrentIncrements(t *testing.T) {
	var counter atomic.Int64
	c := wrapWithCounter(fakeLLMClient{}, &counter)

	const goroutines = 50
	const callsPerGoroutine = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range callsPerGoroutine {
				_, _ = c.Complete(context.Background(), "p")
			}
		}()
	}
	wg.Wait()

	want := int64(goroutines * callsPerGoroutine)
	if got := counter.Load(); got != want {
		t.Fatalf("counter after concurrent calls = %d, want %d", got, want)
	}
}
