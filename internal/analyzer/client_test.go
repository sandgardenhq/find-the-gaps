package analyzer_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// stubToolClient is a minimal implementation used to verify the ToolLLMClient
// interface is satisfiable.
type stubToolClient struct{}

func (s *stubToolClient) Complete(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *stubToolClient) CompleteJSON(_ context.Context, _ string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}

func (s *stubToolClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool, _ ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	return analyzer.AgentResult{}, nil
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

// TestLLMClient_CompleteJSON_ReturnsCannedResponse exercises the fake client's
// schema-keyed canned responses used across analyzer tests.
func TestLLMClient_CompleteJSON_ReturnsCannedResponse(t *testing.T) {
	var client analyzer.LLMClient = &fakeClient{
		jsonResponses: map[string]json.RawMessage{
			"foo": json.RawMessage(`{"x":1}`),
		},
	}
	schema := analyzer.JSONSchema{
		Name: "foo",
		Doc:  json.RawMessage(`{"type":"object"}`),
	}
	raw, err := client.CompleteJSON(context.Background(), "ping", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != `{"x":1}` {
		t.Errorf("got %q, want %q", string(raw), `{"x":1}`)
	}
}

func TestFakeClient_CompleteJSON_MissingSchema_ReturnsError(t *testing.T) {
	f := &fakeClient{jsonResponses: map[string]json.RawMessage{}}
	_, err := f.CompleteJSON(context.Background(), "ping", analyzer.JSONSchema{Name: "missing", Doc: json.RawMessage(`{"type":"object"}`)})
	if err == nil {
		t.Fatal("expected error when no canned response matches schema name")
	}
}

func TestFakeClient_CompleteJSON_ForcedError(t *testing.T) {
	f := &fakeClient{forcedErr: errBoom}
	_, err := f.CompleteJSON(context.Background(), "ping", analyzer.JSONSchema{Name: "x", Doc: json.RawMessage(`{"type":"object"}`)})
	if err == nil {
		t.Fatal("expected forced error")
	}
}
